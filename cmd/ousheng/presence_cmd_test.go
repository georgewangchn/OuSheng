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

// TestAdapterSystemField：--system / repos.yaml 反查 → ousheng.json 含 system 字段
// （plugin 代际 attach 的前置——盲区修复锁）。
func TestAdapterSystemField(t *testing.T) {
	repo, ws := seatFor(t)
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	b, _ := os.ReadFile(filepath.Join(repo, ".opencode", "ousheng.json"))
	if strings.Contains(string(b), `"system": "data"`) || strings.Contains(string(b), `"system": "lake"`) {
		t.Fatalf("未映射系统时 system 应为空（不猜）:\n%s", b)
	}
	// repos.yaml 反查：ws 仓的 repos.yaml 里 data→repo 路径 → install 后 system=data。
	mustRun(t, ws, "repo", "set", "data", repo)
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	b, _ = os.ReadFile(filepath.Join(repo, ".opencode", "ousheng.json"))
	if !strings.Contains(string(b), `"system": "data"`) {
		t.Fatalf("repos.yaml 反查应填 system=data:\n%s", b)
	}
	// 显式 --system 覆盖。
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--system", "lake")
	b, _ = os.ReadFile(filepath.Join(repo, ".opencode", "ousheng.json"))
	if !strings.Contains(string(b), `"system": "lake"`) {
		t.Fatalf("--system 显式覆盖:\n%s", b)
	}
}

// TestPluginSessionAttach：plugin 源码含 seatSessionId 捕获 + idle attach 调用
// （盲区修复的回归锁——plugin 是 ts 不跑 go test，锁源码护栏）。
func TestPluginSessionAttach(t *testing.T) {
	b, err := os.ReadFile("plugin.ts")
	if err != nil {
		t.Skip("不在 adapter 目录")
	}
	s := string(b)
	for _, want := range []string{
		"seatSessionId",          // session.created 捕获
		"handover.*attach",       // idle 时自动 attach
		"cfg.system",             // 从 ousheng.json 读 system
		"system.*string.*system", // MachineCfg 含 system 字段
	} {
		if !strings.Contains(s, want) && !strings.Contains(s, strings.ReplaceAll(want, ".*", "")) {
			t.Fatalf("plugin.ts 缺代际 attach 护栏 %q", want)
		}
	}
}
