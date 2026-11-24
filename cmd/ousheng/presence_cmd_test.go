package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ousheng/internal/model"
)

// TestPresenceDeclarationExpires（档案 §56）：present 声明有有效期——过期即按
// away 行事（唯一判定点 effectivePresence：pm status / plan·status 头行同源）。
// 判据：纯纪律「夜离场必 away」+ ignite 提醒两道都没拦住，present 挂三天两度
// 复发（11-13→16、11-21→24）= 全队「问完等」空等烧 token。
func TestPresenceDeclarationExpires(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "pm", "--name", "PM")
	mustRun(t, dir, "system", "add", "lake")
	mustRun(t, dir, "team", "add", "dev1", "--type", "agent", "--responsible-human", "pm", "--system", "lake")

	// 新鲜声明（11h）：仍是 present。
	oldNow := model.Now
	model.Now = func() string { return time.Now().Add(-11 * time.Hour).Format(time.RFC3339) }
	mustRun(t, dir, "pm", "present")
	model.Now = oldNow
	out := mustRun(t, dir, "pm", "status")
	if !strings.Contains(out, "问完等") || strings.Contains(out, "有效期") {
		t.Fatalf("11h 内的 present 声明应生效:\n%s", out)
	}
	// 过期声明（13h）：声明原文仍在（pm 的手不被卡），生效态回到悲观默认 away。
	model.Now = func() string { return time.Now().Add(-13 * time.Hour).Format(time.RFC3339) }
	mustRun(t, dir, "pm", "present")
	model.Now = oldNow
	out = mustRun(t, dir, "pm", "status")
	for _, want := range []string{"PM present", "13 小时", "按 away 行事", "挂起转下一活"} {
		if !strings.Contains(out, want) {
			t.Fatalf("过期 present 应按 away 行事且显式说明（缺 %q）:\n%s", want, out)
		}
	}
	// plan/status 头行同源：agent 与 pm 看到的是同一个生效态。
	out = mustRun(t, dir, "cruise", "plan", "--actor", "dev1")
	if !strings.Contains(out, "声明过期按 away 行事") || !strings.Contains(out, "不空等") || strings.Contains(out, "问完等") {
		t.Fatalf("plan 头行应随生效态走 away:\n%s", out)
	}
	out = mustRun(t, dir, "cruise", "status")
	if !strings.Contains(out, "声明过期按 away 行事") {
		t.Fatalf("status 头行应显式过期:\n%s", out)
	}
	// 重新声明即恢复（pm 在场的一步动作）。
	mustRun(t, dir, "pm", "present")
	out = mustRun(t, dir, "pm", "status")
	if !strings.Contains(out, "问完等") || strings.Contains(out, "声明已") {
		t.Fatalf("重新声明应恢复 present:\n%s", out)
	}
	// 声明缺有效时间戳（手改坏了 since）：无法证明新鲜 = 不是 present（C1 不确定即拒，
	// 不能因为解析不了时间戳就永久 present——那是 fail-open 的过期绕过）。
	path := filepath.Join(dir, ".ousheng", "presence.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nactor: pm\nstate: present\nsince: 不是时间\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = mustRun(t, dir, "pm", "status")
	for _, want := range []string{"PM present", "缺有效时间戳", "按 away 行事"} {
		if !strings.Contains(out, want) {
			t.Fatalf("坏时间戳的 present 应按 away（缺 %q）:\n%s", want, out)
		}
	}
	if out := mustRun(t, dir, "cruise", "plan", "--actor", "dev1"); strings.Contains(out, "问完等") {
		t.Fatalf("plan 头行同源按 away:\n%s", out)
	}
}

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
