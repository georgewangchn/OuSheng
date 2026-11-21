package main

// cruise ignite 锁（档案 §50-B 点火唯一入口）：四道前置门 + systemd 拉起 +
// presence 过夜提醒。测试经 env 旋钮（OUSHENG_SYSTEMCTL / OUSHENG_SYSTEMD_DIR /
// OUSHENG_ETC_DEFAULT）注入假 systemctl 与临时路径——真 systemd 不进 CI。

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ousheng/adapters/opencode"
	"os/exec"
)

// igniteFixture：板（cruiseFixture：data 座位已注册）+ repos.yaml 映射 +
// 座位仓（canonical 驱动器 + ousheng.json）+ systemd 面（单元/参数文件）。
func igniteFixture(t *testing.T) (ws, repo, unitDir, etcDir string) {
	t.Helper()
	ws = cruiseFixture(t)
	repo = t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "repos.yaml"),
		[]byte("lake: "+repo+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".opencode", "ousheng.json"),
		[]byte(`{"workspace": "`+ws+`", "actor": "data", "system": "lake"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "cruise-loop.sh"), opencode.CruiseLoopSh, 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(unitDir, "ousheng-cruise@.service"), []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	etcDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "ousheng-cruise-data"),
		[]byte("CRUISE_REPO="+repo+"\nOUSHENG_CRUISE_ARGS=\""+ws+" data lake "+repo+" 20 600 22:00-07:00\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, repo, unitDir, etcDir
}

// fakeSystemctl：记录调用参数的假 systemctl。
func fakeSystemctl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	bin := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OUSHENG_SYSTEMCTL", bin)
	return log
}

// TestCruiseIgniteNonLocalSeat：跨机座位拒（点火 = 本机执行位操作）。
func TestCruiseIgniteNonLocalSeat(t *testing.T) {
	ws, repo, unitDir, etcDir := igniteFixture(t)
	_ = repo
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)
	// machine 登记：直接写 actor 文件（team add 对已存在座位幂等 skip 不更新
	// 字段——§36 Y6 语义，测试不经该路径改字段）。
	af := filepath.Join(ws, ".ousheng", "actors", "data.yaml")
	if b, err := os.ReadFile(af); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(af, []byte(strings.Replace(string(b),
		"type: agent", "type: agent\n    machine: otherbox(10.9.9.9)", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data"); !strings.Contains(out, "≠ 本机") {
		t.Fatalf("non-local seat must be rejected:\n%s", out)
	}
}

// TestCruiseIgniteDriverGate：驱动器缺失/分叉均拒 + adapter install 指引
// （§37「驱动器=协议资产」从纪律升为门）。
func TestCruiseIgniteDriverGate(t *testing.T) {
	ws, repo, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)

	drv := filepath.Join(repo, "scripts", "cruise-loop.sh")
	if err := os.WriteFile(drv, []byte("#!/bin/sh\n# hand-forked\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data")
	if !strings.Contains(out, "分叉") || !strings.Contains(out, "adapter install --cruise") {
		t.Fatalf("drift must be rejected with guidance:\n%s", out)
	}
	if err := os.Remove(drv); err != nil {
		t.Fatal(err)
	}
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data"); !strings.Contains(out, "驱动器缺失") {
		t.Fatalf("missing driver must be rejected:\n%s", out)
	}
}

// TestCruiseIgniteSystemdHappy：单元+参数齐备 → systemctl start 拉起，幂等路径
// 在真机由座位 flock 兜底（§46 BUG-179）。
func TestCruiseIgniteSystemdHappy(t *testing.T) {
	ws, _, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	log := fakeSystemctl(t)

	out := mustRun(t, ws, "cruise", "ignite", "--seat", "data")
	if !strings.Contains(out, "点火") || !strings.Contains(out, "journalctl") {
		t.Fatalf("ignite must report start + log path:\n%s", out)
	}
	called, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(called), "start ousheng-cruise@data") {
		t.Fatalf("systemctl start must be invoked, got %v %q", err, called)
	}
	// --stop 面。
	out = mustRun(t, ws, "cruise", "ignite", "--seat", "data", "--stop")
	if !strings.Contains(out, "停火") {
		t.Fatalf("stop must report:\n%s", out)
	}
	called, _ = os.ReadFile(log)
	if !strings.Contains(string(called), "stop ousheng-cruise@data") {
		t.Fatalf("systemctl stop must be invoked:\n%s", called)
	}
}

// TestCruiseIgniteNoUnit：无监督形态（缺单元/参数文件）不点火——§34 复盘前
// 「驱动器单进程无监督」死法不开后门。
func TestCruiseIgniteNoUnit(t *testing.T) {
	ws, _, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)
	// unitDir 故意不注入（默认 /etc/systemd/system 在测试机无单元）——为确定性
	// 直接指向空目录。
	t.Setenv("OUSHENG_SYSTEMD_DIR", t.TempDir())
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data"); !strings.Contains(out, "systemd 单元缺失") {
		t.Fatalf("missing unit must fail-loud:\n%s", out)
	}
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", t.TempDir())
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data"); !strings.Contains(out, "参数文件缺失") {
		t.Fatalf("missing env file must fail-loud:\n%s", out)
	}
}

// TestCruiseIgnitePresenceReminder：present 挂超 12h → 点火输出携带提醒
// （11-13→16 实锤：present 挂三天 = agent 空等烧 token）。提醒非门——不拦。
func TestCruiseIgnitePresenceReminder(t *testing.T) {
	ws, _, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)
	old := time.Now().Add(-72 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "presence.yaml"),
		[]byte("schema_version: 1\nactor: pm\nstate: present\nsince: "+old+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, ws, "cruise", "ignite", "--seat", "data")
	if !strings.Contains(out, "present 已挂") || !strings.Contains(out, "away") {
		t.Fatalf("stale present must carry reminder:\n%s", out)
	}
}

// TestWebCruiseIgnite：web 面点火 = CLI 等价（§24 直调派发器）；拒绝路径透传。
func TestWebCruiseIgnite(t *testing.T) {
	ws, repo, unitDir, etcDir := igniteFixture(t)
	_ = repo
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	log := fakeSystemctl(t)
	// 板已有 me=data（cruiseFixture）——guard 只要求 me 非空，点火身份走座位门。

	// web 侧点火（data 的 machine 空 = 本机归属过门）。
	if resp := webPost(t, ws, "/cruise/ignite", mapToValues(map[string]string{"seat": "data", "action": "start"}), ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("web ignite must succeed, got %d", resp.StatusCode)
	}
	called, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(called), "start ousheng-cruise@data") {
		t.Fatalf("web ignite must invoke systemctl, got %v %q", err, called)
	}
	// CLI 等价：同参数同结果（TestWebOpsMatchCLI 形态）。
	if out := mustRun(t, ws, "cruise", "ignite", "--seat", "data"); !strings.Contains(out, "点火") {
		t.Fatalf("CLI ignite must succeed:\n%s", out)
	}
}

func mapToValues(m map[string]string) url.Values {
	v := url.Values{}
	for k, val := range m {
		v.Set(k, val)
	}
	return v
}

// TestCruiseIgniteStopBypassesHealthGates（10 轮后实锤 🔴）：--stop 不被健康门
// 拦——拓扑分叉时停火必须可用（fail-safe 方向：点火要健康，停止永远允许；
// 09:2x 事故：四座带旧脚本在跑、stop 被拓扑门拒 = 想停停不下来）。
func TestCruiseIgniteStopBypassesHealthGates(t *testing.T) {
	ws, repo, unitDir, etcDir := igniteFixture(t)
	_ = repo
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	fakeSystemctl(t)
	// 制造拓扑 hard：og 上有本地缺新的提交（需 n≥2 才入比较——再加 og2 同推基线）。
	og := filepath.Join(t.TempDir(), "og")
	og2 := filepath.Join(t.TempDir(), "og2")
	exec.Command("git", "init", "-q", "--bare", og).Run()
	exec.Command("git", "init", "-q", "--bare", og2).Run()
	exec.Command("git", "-C", ws, "remote", "add", "og", og).Run()
	exec.Command("git", "-C", ws, "remote", "add", "og2", og2).Run()
	exec.Command("git", "-C", ws, "push", "-q", "og", "main").Run()
	exec.Command("git", "-C", ws, "push", "-q", "og2", "main").Run()
	ws2 := filepath.Join(t.TempDir(), "ws2")
	exec.Command("git", "clone", "-q", og, ws2).Run()
	exec.Command("git", "-C", ws2, "config", "user.email", "t@t").Run()
	exec.Command("git", "-C", ws2, "config", "user.name", "t").Run()
	mustRun(t, ws2, "todo", "远端新单", "--system", "lake", "--actor", "data", "--dir", ws2)
	exec.Command("git", "-C", ws2, "push", "-q", "origin", "main").Run()
	exec.Command("git", "-C", ws, "fetch", "-q", "og").Run()
	// start 应被拓扑门拒。
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); !strings.Contains(out, "分叉") {
		t.Fatalf("hard 分叉应拒点火:\n%s", out)
	}
	// stop 必须放行（健康门豁免）。
	if out := mustRun(t, ws, "cruise", "ignite", "--seat", "data", "--stop", "--dir", ws); !strings.Contains(out, "停火") {
		t.Fatalf("stop 不应被健康门拦:\n%s", out)
	}
}
