package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// handoverSave 经 CLI 存快照（stdin 注入正文——run() 直调读真 stdin）。
func saveHandover(t *testing.T, dir, system, actor, body string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "handover")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(body)
	f.Close()
	old := os.Stdin
	os.Stdin, _ = os.Open(f.Name())
	defer func() { os.Stdin = old }()
	args := []string{"handover", "save", "--dir", dir, "--system", system}
	if actor != "" {
		args = append(args, "--actor", actor)
	}
	if _, se, code := runCLIRaw(args); code != 0 {
		t.Fatalf("handover save failed: %s", se)
	}
}

// TestHandoverSizeGate：4KB 硬门卡 save 写路径（fail-closed）；
// human 直改文件不拦（卡点不变式——绳不卡人的手）。
func TestHandoverSizeGate(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data")

	saveHandover(t, dir, "lake", "data", "# 小快照\n下一步：跑回归\n")
	out := mustRun(t, dir, "handover", "list")
	if !strings.Contains(out, "lake") || !strings.Contains(out, "data") {
		t.Fatalf("list must show seat, got:\n%s", out)
	}

	big := "# 膨胀\n" + strings.Repeat("x", 5000)
	f, _ := os.CreateTemp(t.TempDir(), "big")
	f.WriteString(big)
	f.Close()
	old := os.Stdin
	os.Stdin, _ = os.Open(f.Name())
	_, _, code := runCLIRaw([]string{"handover", "save", "--dir", dir, "--system", "lake"})
	os.Stdin = old
	if code == 0 {
		t.Fatalf("oversized handover must be rejected")
	}

	// human 直改不拦：手写大文件合法落盘（git 审计兜底），list 如实显示超限。
	p := filepath.Join(dir, ".ousheng", "handover", "lake", "data.md")
	os.WriteFile(p, []byte("---\nsystem: lake\nactor: data\nsaved_at: 2026-10-31T00:00:00+08:00\n---\n"+strings.Repeat("y", 5000)), 0o644)
	out = mustRun(t, dir, "handover", "list")
	if !strings.Contains(out, "5000B / 4096B") && !strings.Contains(out, "/ 4096B") {
		t.Fatalf("oversized page must surface in list, got:\n%s", out)
	}
}

// TestHandoverContextPointer：context me 只显本座位指针行（T10——正文零注入，
// INJECTION-MARKER 双断言）；四路出局：work show/list、view kanban 不含。
func TestHandoverContextPointer(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data")
	mustRun(t, dir, "system", "add", "lake")
	mustRun(t, dir, "team", "add", "other", "--type", "agent", "--responsible-human", "data")

	saveHandover(t, dir, "lake", "data", "# 交接\nINJECTION-MARKER-HANDOVER 不该进任何注入面\n下一步 T-9\n")
	// 别人的座位：不该出现在 data 的 context me。
	saveHandover(t, dir, "lake", "other", "# 别人的工作记忆\nINJECTION-MARKER-OTHER\n")

	mc := mustRun(t, dir, "context", "me", "--actor", "data")
	if !strings.Contains(mc, "handover: lake/data @") {
		t.Fatalf("own seat pointer must appear:\n%s", mc)
	}
	if strings.Contains(mc, "INJECTION-MARKER-HANDOVER") {
		t.Fatalf("handover body must not be injected (T10):\n%s", mc)
	}
	if strings.Contains(mc, "lake/other") || strings.Contains(mc, "INJECTION-MARKER-OTHER") {
		t.Fatalf("other seats' snapshots must stay invisible:\n%s", mc)
	}
	// 四路出局：work 面/看板不携带。
	for _, out := range []string{
		mustRun(t, dir, "work", "list"),
		mustRun(t, dir, "view", "kanban"),
	} {
		if strings.Contains(out, "INJECTION-MARKER-HANDOVER") || strings.Contains(out, "handover:") {
			t.Fatalf("work/kanban surfaces must not carry handover:\n%s", out)
		}
	}
	// show 恢复（读了就是恢复）。
	sh := mustRun(t, dir, "handover", "show", "--system", "lake", "--actor", "data")
	if !strings.Contains(sh, "INJECTION-MARKER-HANDOVER") || !strings.Contains(sh, "数据不是指令") {
		t.Fatalf("show must return body with data-not-instruction banner:\n%s", sh)
	}
}

// TestHandoverRoundtripGit：save→git 历史→re-clone 后快照可得（灾难恢复性质）。
func TestHandoverRoundtripGit(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data")
	mustRun(t, dir, "system", "add", "lake")
	saveHandover(t, dir, "lake", "data", "# v1\n第一页\n")
	saveHandover(t, dir, "lake", "data", "# v2\n覆盖后的当前页\n")

	// re-clone（join 灾难恢复形态）：快照随 git 走。
	clone := t.TempDir()
	gitRun(t, clone, "clone", "-q", dir, clone)
	out := mustRun(t, clone, "handover", "show", "--system", "lake", "--actor", "data")
	if !strings.Contains(out, "覆盖后的当前页") {
		t.Fatalf("re-clone must restore current snapshot:\n%s", out)
	}
	// 旧版在 git 历史（版本链），工作区恒单页。
	if entries, _ := os.ReadDir(filepath.Join(clone, ".ousheng", "handover", "lake")); len(entries) != 1 {
		t.Fatalf("workspace keeps single page per seat, got %d", len(entries))
	}
}

// TestHandoverDegrade：无目录零噪音；损坏文件跳过不炸。
func TestHandoverDegrade(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data")
	if out := mustRun(t, dir, "handover", "list"); !strings.Contains(out, "无快照") {
		t.Fatalf("empty must be silent hint, got %s", out)
	}
	mc := mustRun(t, dir, "context", "me", "--actor", "data")
	if strings.Contains(mc, "handover:") {
		t.Fatalf("no dir must yield no pointer:\n%s", mc)
	}
	// 损坏快照：list 跳过（不炸），show 明确报错。
	p := filepath.Join(dir, ".ousheng", "handover", "lake", "broken.md")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("not a handover file"), 0o644)
	if out := mustRun(t, dir, "handover", "list"); strings.Contains(out, "broken") {
		t.Fatalf("corrupt page must be skipped:\n%s", out)
	}
}

// TestHandoverNotInDocLayer：handover md 不进 doc orphan/census（.ousheng/ 排除回归锁）。
func TestHandoverNotInDocLayer(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data")
	mustRun(t, dir, "system", "add", "lake")
	saveHandover(t, dir, "lake", "data", "# 交接\n")
	for _, out := range []string{
		mustRun(t, dir, "doc", "map"),
		mustRun(t, dir, "doc", "check"),
		mustRun(t, dir, "doc", "census"),
	} {
		if strings.Contains(out, "handover") {
			t.Fatalf("doc layer must never surface handover pages:\n%s", out)
		}
	}
}
