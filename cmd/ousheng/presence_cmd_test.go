package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPresenceCmd：present/away 声明 + 非人拒绝 + 缺省悲观默认。
func TestPresenceCmd(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "pm", "--name", "PM")

	// 未声明 = away（悲观默认，P1）。
	out := mustRun(t, dir, "pm", "status")
	if !strings.Contains(out, "away") || !strings.Contains(out, "悲观默认") {
		t.Fatalf("undeclared must default away:\n%s", out)
	}
	// present 声明。
	out = mustRun(t, dir, "pm", "present")
	if !strings.Contains(out, "PM present") {
		t.Fatalf("present declare:\n%s", out)
	}
	// cruise plan 头行随在场度变化。
	mustRun(t, dir, "system", "add", "lake")
	mustRun(t, dir, "team", "add", "dev1", "--type", "agent", "--responsible-human", "pm")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "dev1")
	if !strings.Contains(out, "PM present") || !strings.Contains(out, "问完等") {
		t.Fatalf("plan header must carry presence fork:\n%s", out)
	}
	mustRun(t, dir, "pm", "away")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "dev1")
	if !strings.Contains(out, "PM away") || !strings.Contains(out, "不空等") {
		t.Fatalf("plan header away fork:\n%s", out)
	}
	// agent 座位不得代人在场（me=agent 的机器）。
	agent := t.TempDir()
	mustRun(t, agent, "init")
	mustRun(t, agent, "me", "pm", "--name", "PM")
	mustRun(t, agent, "system", "add", "lake")
	mustRun(t, agent, "team", "add", "dev1", "--type", "agent", "--responsible-human", "pm", "--system", "lake")
	mustRun(t, agent, "me", "dev1")
	if _, se, code := runCLIRaw([]string{"pm", "present", "--dir", agent}); code == 0 || !strings.Contains(se, "human") {
		t.Fatalf("agent must not declare presence, code=%d se=%s", code, se)
	}
	_ = os.Stdout
	_ = filepath.Join
	_ = strings.Contains
}
