package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkDoctorWorkspace：mkWorkspace 基础上补注册 actor（doctor 查 me/座位身份注册态）。
func mkDoctorWorkspace(t *testing.T, actorID string) string {
	t.Helper()
	ws := mkWorkspace(t, actorID)
	if err := os.MkdirAll(filepath.Join(ws, ".ousheng", "actors"), 0o755); err != nil {
		t.Fatal(err)
	}
	af := "schema_version: 1\nactor:\n    id: " + actorID + "\n    type: agent\n    display_name: " + actorID + "\n    responsible_human: pm\n"
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "actors", actorID+".yaml"), []byte(af), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// doctor 健康座位：全 PASS 零退出（延后清单 2026-09-20 判据实锤转正，档案 §12）。
func TestDoctorHealthySeat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws)

	so, se, code := runIn(t, repo, "doctor")
	if code != 0 {
		t.Fatalf("健康座位应零退出：\nstdout:%s\nstderr:%s", so, se)
	}
	for _, want := range []string{"座位身份 actor=data", "插件与内嵌资产逐字节一致", "AGENTS.md 协议段 = 当前版本", "全局放行", "me=data 已注册", "doctor: 全部通过"} {
		if !strings.Contains(so, want) {
			t.Fatalf("doctor 输出缺 %q:\n%s", want, so)
		}
	}
	if strings.Contains(so, "[FAIL") {
		t.Fatalf("健康座位不应有 FAIL:\n%s", so)
	}
}

// doctor 抓四类漂移：插件漂移 / 幽灵 actor / 全局放行缺失 / repo 映射失效。
// 每类：tamper → doctor 命中 FAIL + 退出 1 → adapter install（或对应命令）修复。
func TestDoctorDetectsDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := mkDoctorWorkspace(t, "data")
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws)

	expectFail := func(want string) {
		t.Helper()
		so, _, code := runIn(t, repo, "doctor")
		if code != 1 {
			t.Fatalf("漂移应退出 1：\n%s", so)
		}
		if !strings.Contains(so, want) {
			t.Fatalf("doctor 应命中 %q:\n%s", want, so)
		}
	}

	// 1. 插件漂移（install 恒同步语义 → 逐字节比对必抓）
	if err := os.WriteFile(filepath.Join(repo, ".opencode", "plugins", "ousheng-sampler.ts"), []byte("// drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectFail("插件与内嵌资产不一致")
	mustRun(t, repo, "adapter", "install", "--workspace", ws) // 修复

	// 2. 幽灵 actor（ousheng.json 指向未注册身份）
	if err := os.WriteFile(filepath.Join(repo, ".opencode", "ousheng.json"), []byte(`{"workspace":"`+ws+`","actor":"ghost"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	expectFail("actor=ghost 未注册")
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--actor", "data")

	// 3. 全局放行缺失（权限弹窗事故的机器可查形态）
	gpath := filepath.Join(home, ".config", "opencode", "opencode.json")
	if err := os.WriteFile(gpath, []byte(`{"provider":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	expectFail("全局放行缺")
	mustRun(t, repo, "adapter", "install", "--workspace", ws)

	// 4. repo 映射失效（repo set 忘配/路径漂移的机器可查形态）
	mustRun(t, ws, "repo", "set", "data", "/nonexistent-repo")
	expectFail("repo 映射 data → /nonexistent-repo 失效")
}

// 机器级 only：裸目录不炸、零退出、明示非座位。
func TestDoctorMachineOnly(t *testing.T) {
	dir := t.TempDir()
	so, _, code := runIn(t, dir, "doctor")
	if code != 0 {
		t.Fatalf("裸目录应零退出:\n%s", so)
	}
	if !strings.Contains(so, "非 adapter 座位") || !strings.Contains(so, "ousheng 0.3") {
		t.Fatalf("机器级输出缺关键行:\n%s", so)
	}
	if strings.Contains(so, "[FAIL") {
		t.Fatalf("裸目录不应 FAIL:\n%s", so)
	}
}
