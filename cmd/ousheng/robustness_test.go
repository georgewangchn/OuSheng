package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMissingDirsToleratedAfterClone：空目录不进 git——队友 clone 中央仓后
// .ousheng/work/、.ousheng/actors/ 可能缺失。缺目录 = 空工作区，不是报错
// （2026-09-16 反馈：bug report 直接 open .ousheng/work/...: no such file）。
func TestMissingDirsToleratedAfterClone(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "alice", "--name", "Alice")
	mustRun(t, dir, "system", "add", "api")
	for _, d := range []string{"work", "actors"} {
		if err := os.RemoveAll(filepath.Join(dir, ".ousheng", d)); err != nil {
			t.Fatal(err)
		}
	}
	// 写路径须重建目录
	mustRun(t, dir, "bug", "report", "--id", "BUG-1", "--title", "缺目录复现",
		"--system", "api", "--assignee", "alice", "--accountable", "alice", "--actor", "alice")
	mustRun(t, dir, "team", "add", "bob", "--type", "human", "--name", "Bob")
	mustRun(t, dir, "converge")
}

// TestOUSHENGDirEnvHonored：适配器与指南用 OUSHENG_DIR 指向上下文仓；
// CLI 必须认它，否则 plugin 从代码仓 cwd 跑命令会操作错工作区
// （2026-09-16 反馈：此前只认 cwd/--dir）。
func TestOUSHENGDirEnvHonored(t *testing.T) {
	ws := t.TempDir()
	mustRun(t, ws, "init")
	mustRun(t, ws, "me", "alice", "--name", "Alice")
	mustRun(t, ws, "system", "add", "envsys")
	t.Setenv("OUSHENG_DIR", ws)

	var so, se bytes.Buffer
	if code := run([]string{"converge"}, &so, &se); code != 0 {
		t.Fatalf("converge via OUSHENG_DIR failed: %s", se.String())
	}
	if !strings.Contains(so.String(), "envsys") {
		t.Fatalf("converge 未作用于 OUSHENG_DIR 指向的工作区:\n%s", so.String())
	}
}
