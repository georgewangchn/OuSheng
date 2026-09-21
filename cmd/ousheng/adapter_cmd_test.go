package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/adapters/opencode"
)

// mkWorkspace 造一个最小 ousheng 工作区（project.yaml + 可选 me）。
func mkWorkspace(t *testing.T, actor string) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".ousheng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "project.yaml"), []byte("schema_version: 1\nid: t\nname: t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if actor != "" {
		if err := os.WriteFile(filepath.Join(ws, ".ousheng", "me"), []byte(actor+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

// readMachineCfg 读 install 写出的机器本地配置。
func readMachineCfg(t *testing.T, repo string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo, ".opencode", "ousheng.json"))
	if err != nil {
		t.Fatalf("ousheng.json 未写出: %v", err)
	}
	var cfg map[string]string
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("ousheng.json 非法: %v", err)
	}
	return cfg
}

func TestAdapterInstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // install 写全局配置，隔离 HOME 防污染真实机
	ws := mkWorkspace(t, "224")
	dir := t.TempDir()
	out := mustRun(t, dir, "adapter", "install", "--workspace", ws)
	for _, want := range []string{"ousheng-sampler.ts", "created", "opencode.json", "created"} {
		if !strings.Contains(out, want) {
			t.Fatalf("install 输出缺 %q:\n%s", want, out)
		}
	}
	// plugin 与内嵌资产逐字节一致（升级同步的载体）
	got, err := os.ReadFile(filepath.Join(dir, ".opencode", "plugins", "ousheng-sampler.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(opencode.PluginTS) {
		t.Fatal("plugin 内容与内嵌资产不一致")
	}
	// opencode.json 必须是合法配置
	raw, err := os.ReadFile(filepath.Join(dir, ".opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("opencode.json 非法: %v", err)
	}
	if _, ok := cfg["instructions"].([]any); ok {
		t.Fatal("内嵌配置不应包含 instructions（纪律走 AGENTS.md）")
	}

	// v2：机器本地配置 + gitignore + AGENTS.md 协议段
	mc := readMachineCfg(t, dir)
	if mc["workspace"] != ws {
		t.Fatalf("ousheng.json workspace = %q, want %q", mc["workspace"], ws)
	}
	if mc["actor"] != "224" {
		t.Fatalf("ousheng.json actor = %q, want 224（从 me 解析）", mc["actor"])
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".opencode", ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "ousheng.json") {
		t.Fatalf(".opencode/.gitignore 应含 ousheng.json（机器配置不入库）: %v %q", err, gi)
	}
	// 2026-09-21 裁决（档案 §13）：全部座位资产走 gitignore，只留 skills/ 等仓库内容
	for _, line := range []string{"ousheng.json", "opencode.json", "package.json", "plugins/"} {
		if !strings.Contains(string(gi), line) {
			t.Fatalf(".opencode/.gitignore 应含 %q（座位资产不入库，二进制内嵌分发为单一来源）:\n%s", line, gi)
		}
	}
	ag, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md 未写出: %v", err)
	}
	s := string(ag)
	if !strings.Contains(s, "ousheng:begin") || !strings.Contains(s, "ousheng:end") {
		t.Fatalf("AGENTS.md 缺标记段:\n%s", s)
	}
	if strings.Contains(s, ws) {
		t.Fatal("AGENTS.md 含机器路径——协议段必须机器中立（机器参数只活在 ousheng.json）")
	}

	// 幂等：第二次全部 unchanged
	out2 := mustRun(t, dir, "adapter", "install")
	if !strings.Contains(out2, "unchanged") || strings.Contains(out2, "created") {
		t.Fatalf("install 不幂等:\n%s", out2)
	}
	// 升级重跑：既有 ousheng.json 就是 workspace 来源（无需再传 --workspace）
	cfg2 := readMachineCfg(t, dir)
	if cfg2["workspace"] != ws {
		t.Fatalf("重跑后 workspace 漂移: %q", cfg2["workspace"])
	}
}

func TestAdapterInstallColocated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 226 形态：工作区检出本身就是安装目标 → 自动识别，无需 --workspace
	repo := mkWorkspace(t, "226")
	mustRun(t, repo, "adapter", "install")
	cfg := readMachineCfg(t, repo)
	if cfg["workspace"] != repo {
		t.Fatalf("同仓检测失败: workspace=%q want=%q", cfg["workspace"], repo)
	}
	if cfg["actor"] != "226" {
		t.Fatalf("actor = %q, want 226", cfg["actor"])
	}
}

func TestAdapterInstallEnvFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkWorkspace(t, "225")
	t.Setenv("OUSHENG_DIR", ws)
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install")
	cfg := readMachineCfg(t, repo)
	if cfg["workspace"] != ws {
		t.Fatalf("env 回退失败: workspace=%q want=%q", cfg["workspace"], ws)
	}
}

func TestAdapterInstallFailsWithoutWorkspace(t *testing.T) {
	repo := t.TempDir()
	so, se, code := runIn(t, repo, "adapter", "install")
	if code == 0 {
		t.Fatalf("无工作区来源应报错退出:\n%s", so)
	}
	if !strings.Contains(se, "--workspace") {
		t.Fatalf("报错应指引 --workspace:\n%s", se)
	}
	// --workspace 指向非工作区 → 拒绝
	so, se, code = runIn(t, repo, "adapter", "install", "--workspace", repo)
	if code == 0 || !strings.Contains(se, "不是 ousheng 工作区") {
		t.Fatalf("非工作区应拒绝:\n%s\n%s", so, se)
	}
}

func TestAdapterInstallAgentsSection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkWorkspace(t, "224")
	repo := t.TempDir()
	// 已有用户内容、无标记段 → 追加且保留用户内容
	user := "# 本仓说明\n\n自定义内容，install 不得触碰。\n"
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, repo, "adapter", "install", "--workspace", ws)
	if !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("输出应含 AGENTS.md 状态:\n%s", out)
	}
	ag, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	s := string(ag)
	if !strings.Contains(s, "自定义内容") || !strings.Contains(s, "ousheng:begin") {
		t.Fatalf("用户内容必须保留、标记段必须追加:\n%s", s)
	}
	// 标记段内塞入过期内容 → 重跑强制回到内嵌版本（升级同步的载体）
	stale := "# 本仓说明\n\n自定义内容，install 不得触碰。\n\n<!-- ousheng:begin -->\n过期协议文本\n<!-- ousheng:end -->\n"
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	ag, _ = os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	s = string(ag)
	if strings.Contains(s, "过期协议文本") {
		t.Fatalf("标记段内的过期内容应被替换:\n%s", s)
	}
	if !strings.Contains(s, "自定义内容") || !strings.Contains(s, string(opencode.AgentsMD)) {
		t.Fatalf("段外内容保留 + 段内回到内嵌版本:\n%s", s)
	}
}

// --actor：PM 机形态——AI 会话身份（agent 型）≠ me（human 型），install 落前者。
// 2026-09-20 越位事故：226 的 AI 会话借 human 身份操作，类型门全失明。
func TestAdapterInstallActorOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ws := mkWorkspace(t, "226") // me = 226（human）
	repo := t.TempDir()
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--actor", "226-dev")
	if cfg := readMachineCfg(t, repo); cfg["actor"] != "226-dev" {
		t.Fatalf("--actor 应覆盖 me：got %q", cfg["actor"])
	}
	// 缺省 = me（执行机常态：agent 自己就是 me）
	repo2 := t.TempDir()
	mustRun(t, repo2, "adapter", "install", "--workspace", ws)
	if cfg := readMachineCfg(t, repo2); cfg["actor"] != "226" {
		t.Fatalf("缺省应取 me：got %q", cfg["actor"])
	}
	// 幂等：同参重跑 unchanged；换 actor → ousheng.json updated
	out := mustRun(t, repo, "adapter", "install", "--workspace", ws, "--actor", "226-dev")
	if !strings.Contains(out, "unchanged") {
		t.Fatalf("同参重跑应幂等:\n%s", out)
	}
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--actor", "226-dev2")
	if cfg := readMachineCfg(t, repo); cfg["actor"] != "226-dev2" {
		t.Fatalf("换 actor 应更新：got %q", cfg["actor"])
	}
}

func TestAdapterInstallFixesConfigAndFlagsLegacy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	oc := filepath.Join(dir, ".opencode")
	if err := os.MkdirAll(oc, 0o755); err != nil {
		t.Fatal(err)
	}
	// 坏配置：instructions 对象数组 + 悬空路径 + 用户自定义键
	broken := `{"$schema":"https://opencode.ai/config.json",
		"mcp":{"ousheng":{"type":"local","command":["ousheng-mcp"],"enabled":true}},
		"watcher":{"ignore":["node_modules/**"]},
		"instructions":[{"path":"docs/board-protocol.md","description":"x"}]}`
	if err := os.WriteFile(filepath.Join(oc, "opencode.json"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	// 历史遗留位置：opencode 不加载
	if err := os.WriteFile(filepath.Join(oc, "plugin.ts"), []byte("// old"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := mustRun(t, dir, "adapter", "install", "--workspace", mkWorkspace(t, "224"))
	if !strings.Contains(out, "instructions 已修正") {
		t.Fatalf("应修正非法 instructions:\n%s", out)
	}
	if !strings.Contains(out, ".opencode/plugin.ts") {
		t.Fatalf("应提示历史遗留 plugin.ts:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(oc, "opencode.json"))
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["instructions"]; ok {
		t.Fatalf("悬空 instructions 应被移除: %s", raw)
	}
	if cfg["watcher"] == nil {
		t.Fatal("用户自定义键必须保留")
	}
}

// 2026-09-20 车队反馈锁：224/225 每次 opencode 启动都弹工作区 external_directory
// 权限询问（opencode 默认 ask，TUI "always" 只活当前会话）。install 必须把
// `permission.external_directory[<workspace>/**]=allow` 合并进全局配置；
// 工作区路径是机器本地信息，入库的五件套放不下。
func TestAdapterInstallGrantsWorkspacePermission(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ws := mkWorkspace(t, "225")
	gcfgDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(gcfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gpath := filepath.Join(gcfgDir, "opencode.json")
	// 预置用户全局配置（provider 必须原样保留）
	if err := os.WriteFile(gpath, []byte(`{"provider":{"gate":{"npm":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	so, se, code := runIn(t, repo, "adapter", "install", "--workspace", ws)
	if code != 0 || strings.Contains(se, "全局放行跳过") {
		t.Fatalf("全局放行不应失败：code=%d\nstdout:%s\nstderr:%s", code, so, se)
	}
	raw, err := os.ReadFile(gpath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("合并后全局配置非法: %v\n%s", err, raw)
	}
	if cfg["provider"] == nil {
		t.Fatal("用户 provider 必须保留")
	}
	perm, _ := cfg["permission"].(map[string]any)
	ext, _ := perm["external_directory"].(map[string]any)
	if ext[ws+"/**"] != "allow" {
		t.Fatalf("external_directory 缺工作区放行: %s", raw)
	}
	// 首次改动应有备份
	if _, err := os.Stat(gpath + ".bak-ousheng"); err != nil {
		t.Fatal("首次改动应备份 .bak-ousheng")
	}
	// 幂等：重跑不再改动
	before, _ := os.ReadFile(gpath)
	mustRun(t, repo, "adapter", "install", "--workspace", ws)
	after, _ := os.ReadFile(gpath)
	if string(before) != string(after) {
		t.Fatalf("重跑应幂等:\nbefore:%s\nafter:%s", before, after)
	}

	// permission 非对象结构 → 拒绝改写、软失败不阻塞 install
	if err := os.WriteFile(gpath, []byte(`{"permission":"allow"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	so, se, code = runIn(t, repo, "adapter", "install", "--workspace", ws)
	if code != 0 {
		t.Fatalf("全局放行软失败不应阻塞 install:\n%s", se)
	}
	if !strings.Contains(se, "手动") {
		t.Fatalf("非对象结构应给人话指引:\n%s", se)
	}
}
