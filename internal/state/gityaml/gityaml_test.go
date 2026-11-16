package gityaml

// TestListReadsBestEffort（sim50 复测实锤，2026-11-16）：单文件损坏不瘫读面——
// 分叉撞号把冲突标记写进卡面（§43 T-192 同型）→ 一张坏卡曾让 todo/status/
// report/plan/web board 全炸。判据（§37/§40 记档「再发一次即做」）实锤触发：
// work/actors/designs 三读面统一跳坏 + 警告；写路径（GetWorkItem 单卡）仍拒。

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
