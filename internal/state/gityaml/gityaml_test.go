package gityaml

// TestListReadsBestEffort（sim50 复测实锤，2026-11-16）：单文件损坏不瘫读面——
// 分叉撞号把冲突标记写进卡面（§43 T-192 同型）→ 一张坏卡曾让 todo/status/
// report/plan/web board 全炸。判据（§37/§40 记档「再发一次即做」）实锤触发：
// work/actors/designs 三读面统一跳坏 + 警告；写路径（GetWorkItem 单卡）仍拒。

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestListActivityBestEffort（2026-11-24 实栈复核实锤）：审计读面同罪——板仓
// `.ousheng/activity/2026-11/pm.jsonl` 带两段未解冲突标记入库（11-22 17:31
// merge 提交），LoadActivity 整体报错，而 status/report/plan 调用点吞错 →
// 全舰队「交付 0 单｜无座位活动｜每张 doing 从未有证据」静默归零 42 小时。
// work/actors/designs 三读面 11-16 已 best-effort，activity 当时漏网：
// 坏行跳过 + 响亮警告（点名文件:行 + 冲突标记提示），好行照常投影。
func TestListActivityBestEffort(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git 不可用: %v %s", err, out)
	}
	repo := Open(dir)
	p := filepath.Join(dir, ".ousheng", "activity", "2026-11", "pm.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"ts":"2026-11-22T16:41:29Z","actor":"pm","action":"status_changed","work_item":"T-001","detail":"doing -> done"}` + "\n" +
		"<<<<<<< HEAD\n" +
		`{"ts":"2026-11-22T17:28:04Z","actor":"pm","action":"evidence_added","work_item":"T-002","detail":"manual_check x"}` + "\n" +
		"=======\n" +
		"{不是 json\n" +
		">>>>>>> uat/main\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// gityaml 读面警告直写 os.Stderr（与 work/actors/designs 三读面同款）——接管捕获。
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	acts, lerr := repo.ListActivity()
	os.Stderr = oldErr
	w.Close()
	var warned bytes.Buffer
	_, _ = io.Copy(&warned, r)
	if lerr != nil || len(acts) != 2 {
		t.Fatalf("坏行应跳过、好行照常投影（实栈：一行坏行曾瘫死全审计读面）: %v %d", lerr, len(acts))
	}
	if acts[0].WorkItem != "T-001" || acts[1].WorkItem != "T-002" {
		t.Fatalf("好行时序应保留: %+v", acts)
	}
	warn := warned.String()
	// 文件名 + 首坏行号 + 冲突标记提示 + 聚合计数（一行坏行一条警告会刷屏）。
	for _, want := range []string{"activity/2026-11/pm.jsonl", "跳过 4 条坏行", "第2行", "冲突标记"} {
		if !strings.Contains(warn, want) {
			t.Fatalf("审计坏行警告缺 %q（读面降级必须可见不静默）:\n%s", want, warn)
		}
	}
	if strings.Count(warn, "[warn]") != 1 {
		t.Fatalf("同一文件的坏行应聚合为一条警告:\n%s", warn)
	}
	// 同进程重复读不重复警告（plan --wait 每 20s 重建——刷屏即噪音）。
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	_, _ = repo.ListActivity()
	os.Stderr = oldErr
	w2.Close()
	var again bytes.Buffer
	_, _ = io.Copy(&again, r2)
	if strings.Contains(again.String(), "[warn]") {
		t.Fatalf("同进程同文件重复读不应重复警告（--wait 轮询刷屏）:\n%s", again.String())
	}
}

func TestListReadsBestEffort(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git 不可用: %v %s", err, out)
	}
	repo := Open(dir)
	ws := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws(".ousheng/work/T-001.yaml", "schema_version: 2\nid: T-001\ntype: task\ntitle: 好\nsystem: s\nstatus: backlog\nrevision: 1\n")
	ws(".ousheng/work/T-002.yaml", "schema_version: 2\n<<<<<<< HEAD\ntitle: 冲突标记\n=======\n")
	ws(".ousheng/actors/pm.yaml", "schema_version: 1\nactor:\n    id: pm\n    type: human\n    display_name: pm\n")
	ws(".ousheng/actors/broken.yaml", "schema_version: 1\nactor: [没闭合\n")
	ws(".ousheng/designs/good/design.md", "---\nstatus: draft\nowner: pm\n---\n正文\n")
	ws(".ousheng/designs/bad/design.md", "---\nstatus: [没闭合\n")

	items, err := repo.ListWorkItems()
	if err != nil || len(items) != 1 || items[0].ID != "T-001" {
		t.Fatalf("坏卡应跳过不瘫读面: %v %d", err, len(items))
	}
	afs, err := repo.ListActors()
	if err != nil || len(afs) != 1 || afs[0].Actor.ID != "pm" {
		t.Fatalf("坏注册记录应跳过: %v %d", err, len(afs))
	}
	ds, err := repo.ListDesigns()
	if err != nil || len(ds) != 1 || ds[0].Topic != "good" {
		t.Fatalf("坏 design 应跳过: %v %d", err, len(ds))
	}
	// 写路径仍 fail-loud：坏卡单读必须报错（修复入口不静默）。
	if _, err := repo.GetWorkItem("T-002"); err == nil {
		t.Fatal("GetWorkItem 对坏卡必须报错（写路径 fail-loud 不降级）")
	}
}
