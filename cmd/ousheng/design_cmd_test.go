package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- 共识层 CLI（v0.4）---

func writeDesignFile(t *testing.T, dir, topic, fm, body string) {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "designs", topic, "design.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\n"+fm+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDesignDecideMustBeHuman(t *testing.T) {
	// human 门（C2 血统，T10 §10.2）：agent 自 decide = 自拍共识，必须拒。
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent", "--responsible-human", "george")
	writeDesignFile(t, dir, "csv-plan", "status: draft\nowner: dev-agent\n", "# 方案\n")

	if _, se, code := runIn(t, dir, "design", "decide", "csv-plan", "--actor", "dev-agent"); code == 0 || !strings.Contains(se, "must be human") {
		t.Fatalf("agent decide must be rejected, got code=%d err=%s", code, se)
	}
	out := mustRun(t, dir, "design", "decide", "csv-plan", "--actor", "george")
	if !strings.Contains(out, "decided by george") {
		t.Fatalf("human decide must pass: %s", out)
	}
	// 已 agreed 不能重复 decide
	if _, _, code := runIn(t, dir, "design", "decide", "csv-plan", "--actor", "george"); code == 0 {
		t.Fatal("re-decide agreed design must fail")
	}
}

func TestDesignSupersede(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent", "--responsible-human", "george")
	writeDesignFile(t, dir, "old-plan", "status: draft\nowner: george\n", "v1\n")
	writeDesignFile(t, dir, "new-plan", "status: draft\nowner: george\n", "v2\n")
	mustRun(t, dir, "design", "decide", "old-plan", "--actor", "george")
	mustRun(t, dir, "design", "decide", "new-plan", "--actor", "george")

	if _, _, code := runIn(t, dir, "design", "supersede", "old-plan", "--by", "old-plan", "--actor", "george"); code == 0 {
		t.Fatal("self-supersede must fail")
	}
	if _, _, code := runIn(t, dir, "design", "supersede", "old-plan", "--by", "ghost", "--actor", "george"); code == 0 {
		t.Fatal("supersede by nonexistent must fail")
	}
	if _, se, code := runIn(t, dir, "design", "supersede", "old-plan", "--by", "new-plan", "--actor", "dev-agent"); code == 0 || !strings.Contains(se, "must be human") {
		t.Fatalf("agent supersede must be rejected, got code=%d", code)
	}
	out := mustRun(t, dir, "design", "supersede", "old-plan", "--by", "new-plan", "--actor", "george")
	if !strings.Contains(out, "superseded by new-plan") {
		t.Fatalf("supersede must pass: %s", out)
	}
}

func TestDesignListFilters(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent", "--responsible-human", "george")
	writeDesignFile(t, dir, "csv-plan", "status: draft\nowner: george\nsystems: [api]\n", "a\n")
	writeDesignFile(t, dir, "auth-plan", "status: draft\nowner: george\nsystems: [api]\n", "b\n")
	writeDesignFile(t, dir, "done-plan", "status: draft\nowner: george\n", "c\n")
	mustRun(t, dir, "design", "decide", "done-plan", "--actor", "george")

	list := mustRun(t, dir, "design", "list", "--status", "agreed")
	if !strings.Contains(list, "done-plan") || strings.Contains(list, "csv-plan") {
		t.Fatalf("--status filter wrong:\n%s", list)
	}
	// waiting-for：csv/auth 未发言 → 待 dev-agent；done agreed 不算
	wf := mustRun(t, dir, "design", "list", "--waiting-for", "dev-agent")
	if !strings.Contains(wf, "csv-plan") || !strings.Contains(wf, "auth-plan") || strings.Contains(wf, "done-plan") {
		t.Fatalf("--waiting-for filter wrong:\n%s", wf)
	}
}

// TestDesignSignalPaths 共识层信号四路判决（v0.4 §4.4）：
// 接入 = work show（detail）+ context me（WorkBrief.design / knowledge / pending_reviews）；
// 出局 = work list / view kanban（扫视层不放深链接——明文判决，见 §4.4 表）；
// 注入面铁律 = context me 只含指针，design 正文自由文本绝不出现（T10 §10.1）。
func TestDesignSignalPaths(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "cms-api")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent", "--responsible-human", "george", "--role", "dev", "--system", "cms-api")
	mustRun(t, dir, "todo", "结果导出", "--system", "cms-api") // T-001 assignee george
	archDir := filepath.Join(dir, ".ousheng", "architecture")
	os.MkdirAll(archDir, 0o755)
	os.WriteFile(filepath.Join(archDir, "cms-api.md"), []byte("全局面貌 INJECTION-MARKER-ARCH\n"), 0o644)
	writeDesignFile(t, dir, "csv-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\nrelated_items: [T-001]\n",
		"# CSV 方案\n\n正文里埋 INJECTION-MARKER-DESIGN 不该进任何注入面。\n")

	// 待发言：dev-agent（scope 含 cms-api）尚未发言 → pending
	mcDev := mustRun(t, dir, "context", "me", "--actor", "dev-agent")
	if !strings.Contains(mcDev, `"pending_reviews"`) || !strings.Contains(mcDev, "csv-plan") {
		t.Fatalf("dev-agent must see pending review:\n%s", mcDev)
	}

	// dev-agent 轮次发言后 → pending 消失
	roundDir := filepath.Join(dir, ".ousheng", "designs", "csv-plan")
	os.WriteFile(filepath.Join(roundDir, "round-1.md"), []byte("## dev-agent — 2026-09-15\n赞成。\n"), 0o644)
	mcDev2 := mustRun(t, dir, "context", "me", "--actor", "dev-agent")
	if strings.Contains(mcDev2, `"pending_reviews"`) {
		t.Fatalf("spoken actor must not be pending:\n%s", mcDev2)
	}

	// 路 1：work show（detail 层）
	show := mustRun(t, dir, "work", "show", "T-001")
	if !strings.Contains(show, "csv-plan") || !strings.Contains(show, "draft") {
		t.Fatalf("work show must carry design provenance:\n%s", show)
	}

	// 路 2：context me（WorkBrief.design 出处 + knowledge 文件名清单）
	mc := mustRun(t, dir, "context", "me", "--actor", "george")
	if !strings.Contains(mc, `"design": "csv-plan (draft)"`) {
		t.Fatalf("context me must carry design pointer:\n%s", mc)
	}
	if !strings.Contains(mc, `"knowledge"`) || !strings.Contains(mc, "cms-api") {
		t.Fatalf("context me must carry knowledge file list:\n%s", mc)
	}

	// 注入面铁律：design 正文与 architecture 正文的自由文本绝不进 context me
	for _, mark := range []string{"INJECTION-MARKER-DESIGN", "INJECTION-MARKER-ARCH"} {
		if strings.Contains(mc, mark) || strings.Contains(mcDev, mark) || strings.Contains(mcDev2, mark) {
			t.Fatalf("injection-surface violated: %s leaked into context me", mark)
		}
	}

	// 出局判决（§4.4）：work list / view kanban 不含方案深链接
	for _, out := range []string{
		mustRun(t, dir, "work", "list", "--open"),
		mustRun(t, dir, "view", "kanban"),
	} {
		if strings.Contains(out, "csv-plan") {
			t.Fatalf("list/kanban must stay pointer-free (扫视层不放深链接):\n%s", out)
		}
	}

	// 生命周期贯通：decide 后出处变 agreed
	mustRun(t, dir, "design", "decide", "csv-plan", "--actor", "george")
	mc2 := mustRun(t, dir, "context", "me", "--actor", "george")
	if !strings.Contains(mc2, `"design": "csv-plan (agreed)"`) {
		t.Fatalf("decided design must update provenance:\n%s", mc2)
	}

	// converge：architecture 覆盖齐全（cms-api.md 在），无共识层警告
	conv := mustRun(t, dir, "converge")
	if strings.Contains(conv, "no architecture doc") || strings.Contains(conv, "running ahead") {
		t.Fatalf("unexpected design warnings:\n%s", conv)
	}
}
