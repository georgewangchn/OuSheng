package main

import (
	"os"
	"os/exec"
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
	for _, want := range []string{"座位身份 actor=data", "插件与内嵌资产", "逐字节一致", "AGENTS.md 协议段 = 当前版本", "全局放行", "me=data 已注册", "doctor: 全部通过"} {
		if !strings.Contains(so, want) {
			t.Fatalf("doctor 输出缺 %q:\n%s", want, so)
		}
	}
	if strings.Contains(so, "[FAIL") {
		t.Fatalf("健康座位不应有 FAIL:\n%s", so)
	}
}

// §37 收紧对偶判据：板仓无 me × 多 agent 座位 = 合法防御形态（BUG-136——me 是
// 单槽位污染源），doctor 降 WARN 不 FAIL；写纪律 = 显式 --actor。
func TestDoctorMultiSeatNoMeWarns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	// 第二 agent 座位（多座位共享板仓形态）
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "actors", "ui.yaml"),
		[]byte("schema_version: 1\nactor:\n    id: ui\n    type: agent\n    display_name: ui\n    responsible_human: pm\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(ws, ".ousheng", "me"))
	// 机器级双信号：repos.yaml 双映射（doctor 会验路径真伪——用真 git 目录）
	r1, r2 := t.TempDir(), t.TempDir()
	for _, d := range []string{r1, r2} {
		c := exec.Command("git", "init", "-q")
		c.Dir = d
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "repos.yaml"),
		[]byte("data: "+r1+"\nui: "+r2+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws)

	so, _, code := runIn(t, repo, "doctor")
	if code != 0 {
		t.Fatalf("多座位无 me 应 WARN 不 FAIL（合法防御形态）:\n%s", so)
	}
	if !strings.Contains(so, "多座位共享") || !strings.Contains(so, "显式 --actor") {
		t.Fatalf("应提示多座位防御形态与 --actor 纪律:\n%s", so)
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
	expectFail("插件与内嵌资产")
	expectFail("不一致")
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

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// 2026-09-21 审计裁决（档案 §15）三项新检查的锁。
func TestDoctorAuditChecks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	t.Run("座位资产入库", func(t *testing.T) {
		ws := mkDoctorWorkspace(t, "data")
		repo := t.TempDir()
		mustRun(t, repo, "adapter", "install", "--workspace", ws)
		gitRun(t, repo, "init", "-b", "main")
		gitRun(t, repo, "config", "user.email", "t@t")
		gitRun(t, repo, "config", "user.name", "t")
		gitRun(t, repo, "add", "-f", ".opencode/opencode.json")
		gitRun(t, repo, "commit", "-qm", "x")
		so, _, code := runIn(t, repo, "doctor")
		if code != 1 || !strings.Contains(so, "座位资产被入库") {
			t.Fatalf("应 FAIL 座位资产入库:\n%s", so)
		}
	})

	t.Run("unknown 审计桶", func(t *testing.T) {
		ws := mkDoctorWorkspace(t, "data")
		repo := t.TempDir()
		mustRun(t, repo, "adapter", "install", "--workspace", ws)
		d := filepath.Join(ws, ".ousheng", "activity", "2026-09")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "unknown.jsonl"), []byte("{\"actor\":\"\"}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		so, _, _ := runIn(t, repo, "doctor")
		if !strings.Contains(so, "unknown.jsonl 存在") {
			t.Fatalf("应 WARN unknown 桶:\n%s", so)
		}
	})

	t.Run("未推提交水位", func(t *testing.T) {
		ws := mkDoctorWorkspace(t, "data")
		gitRun(t, ws, "init", "-b", "main")
		gitRun(t, ws, "config", "user.email", "t@t")
		gitRun(t, ws, "config", "user.name", "t")
		gitRun(t, ws, "add", "-A")
		gitRun(t, ws, "commit", "-qm", "base")
		remote := filepath.Join(t.TempDir(), "r.git")
		gitRun(t, t.TempDir(), "init", "--bare", remote)
		gitRun(t, ws, "remote", "add", "origin", remote)
		gitRun(t, ws, "push", "-q", "-u", "origin", "main")
		if err := os.WriteFile(filepath.Join(ws, "new.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, ws, "add", "-A")
		gitRun(t, ws, "commit", "-qm", "unpushed")
		repo := t.TempDir()
		mustRun(t, repo, "adapter", "install", "--workspace", ws)
		so, _, _ := runIn(t, repo, "doctor")
		if !strings.Contains(so, "未推提交") {
			t.Fatalf("应 WARN 未推提交:\n%s", so)
		}
	})
}

// TestDoctorSystemHint（§36 Y7 锁）：ousheng.json 缺 system 字段 → WARN
// （plugin 代际 attach 静默断链的可查面）；补齐后 WARN 消失。
func TestDoctorSystemHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	so, _, code := runIn(t, repo, "doctor")
	if code != 0 {
		t.Fatalf("WARN 不扣退出码:\n%s", so)
	}
	if !strings.Contains(so, "无 system 字段") {
		t.Fatalf("缺 system 必须 WARN:\n%s", so)
	}
	if err := os.WriteFile(filepath.Join(repo, ".opencode", "ousheng.json"),
		[]byte(`{"workspace":"`+ws+`","actor":"data","system":"lake"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	so, _, _ = runIn(t, repo, "doctor")
	if strings.Contains(so, "无 system 字段") {
		t.Fatalf("补 system 后 WARN 应消失:\n%s", so)
	}
}

// TestDoctorCruiseDriverDrift（§50-C）：装过 --cruise 即逐字节比对 canonical——
// 分叉 FAIL（手改/fork 漂移，§37/§47 事故形态）+ 重装取回复原。未装不查
// （可选资产语义，TestDoctorHealthySeat 已盖）。
func TestDoctorCruiseDriverDrift(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--cruise")

	so, _, code := runIn(t, repo, "doctor")
	if code != 0 || !strings.Contains(so, "巡航驱动器 = canonical") {
		t.Fatalf("canonical driver must PASS:\ncode=%d\n%s", code, so)
	}
	drv := filepath.Join(repo, "scripts", "cruise-loop.sh")
	if err := os.WriteFile(drv, []byte("#!/bin/sh\n# hand-forked\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	so, _, code = runIn(t, repo, "doctor")
	if code == 0 || !strings.Contains(so, "巡航驱动器与 canonical 分叉") || !strings.Contains(so, "adapter install --cruise") {
		t.Fatalf("drifted driver must FAIL with guidance:\ncode=%d\n%s", code, so)
	}
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--cruise")
	if so, _, code = runIn(t, repo, "doctor"); code != 0 || !strings.Contains(so, "巡航驱动器 = canonical") {
		t.Fatalf("reinstall must restore:\ncode=%d\n%s", code, so)
	}
}

// TestDoctorBoardRemoteDrift（T-226 盲区④机械锁，档案 §54）：板仓多 remote
// 水位差 → doctor WARN；推平 → PASS；单 remote 不查（无从比较）。
func TestDoctorBoardRemoteDrift(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 不可用")
	}
	t.Setenv("HOME", t.TempDir())
	ws := mkDoctorWorkspace(t, "data")
	if out, err := exec.Command("git", "-C", ws, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Skipf("git init 失败: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", ws, "commit", "-q", "--allow-empty", "-m", "init").CombinedOutput(); err != nil {
		t.Skipf("空提交失败: %v %s", err, out)
	}
	g := func(dir string, args ...string) {
		c := append([]string{"-C", dir}, args...)
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	og1 := filepath.Join(t.TempDir(), "og1")
	og2 := filepath.Join(t.TempDir(), "og2")
	exec.Command("git", "init", "-q", "--bare", og1).Run()
	exec.Command("git", "init", "-q", "--bare", og2).Run()
	g(ws, "remote", "add", "og1", og1)
	g(ws, "remote", "add", "og2", og2)
	g(ws, "push", "-q", "og1", "main")
	g(ws, "push", "-q", "og2", "main")
	// 单测环境无座位仓——doctor 只验证板仓段：直接调 boardRemoteDrift。
	n, drifts := boardRemoteDrift(ws, true)
	if n != 2 || len(drifts) != 0 {
		t.Fatalf("两源同源应零差值: n=%d drifts=%v", n, drifts)
	}
	// 制造漂移：新提交只推 og1。
	mustRun(t, ws, "todo", "新单", "--system", "lake", "--actor", "data")
	g(ws, "push", "-q", "og1", "main")
	_, drifts = boardRemoteDrift(ws, true)
	if len(drifts) != 1 || !strings.Contains(drifts[0], "og2") {
		t.Fatalf("og2 落后应报差值: %v", drifts)
	}
	g(ws, "push", "-q", "og2", "main")
	if _, drifts = boardRemoteDrift(ws, true); len(drifts) != 0 {
		t.Fatalf("推平后应零差值: %v", drifts)
	}
}

// TestCruiseIgniteTopologyGate（档案 §54 第五道门）：板仓多 remote 水位差 → 拒点火。
func TestCruiseIgniteTopologyGate(t *testing.T) {
	ws, _, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)
	og1 := filepath.Join(t.TempDir(), "og1")
	og2 := filepath.Join(t.TempDir(), "og2")
	exec.Command("git", "init", "-q", "--bare", og1).Run()
	exec.Command("git", "init", "-q", "--bare", og2).Run()
	g := func(dir string, args ...string) {
		c := append([]string{"-C", dir}, args...)
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g(ws, "remote", "add", "og1", og1)
	g(ws, "remote", "add", "og2", og2)
	g(ws, "push", "-q", "og1", "main")
	g(ws, "push", "-q", "og2", "main")
	mustRun(t, ws, "todo", "漂移单", "--system", "lake", "--actor", "data")
	g(ws, "push", "-q", "og1", "main") // og2 落后
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); !strings.Contains(out, "拓扑分裂") {
		t.Fatalf("水位差应拒点火:\n%s", out)
	}
	g(ws, "push", "-q", "og2", "main")
	if out := mustRun(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); !strings.Contains(out, "点火") {
		t.Fatalf("推平后应放行:\n%s", out)
	}
}
