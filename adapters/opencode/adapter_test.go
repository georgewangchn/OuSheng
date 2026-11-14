// 2026-09-16 事故（rdc-05）：适配器配置的 instructions 写成对象数组
// {path, description}，而 opencode schema 只接受路径/glob 字符串数组——
// 文件拷进 .opencode/ 后 opencode 启动即失败（Configuration is invalid:
// Expected string）。同时使用指南把插件拷到 .opencode/plugin.ts，而
// opencode 只从 .opencode/plugins/ 加载，插件静默失效。两个 bug 都是
// "拷进别人机器才炸" 的分发物，故就地加锁。

package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpencodeAdapterConfigIsSchemaValid(t *testing.T) {
	raw, err := os.ReadFile("opencode.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("opencode.json 不是合法 JSON（拷到 .opencode/ 后 opencode 无法启动）: %v", err)
	}
	if cfg["$schema"] != "https://opencode.ai/config.json" {
		t.Fatalf("$schema 必须是 opencode 官方 schema，得到 %v", cfg["$schema"])
	}
	if ins, ok := cfg["instructions"]; ok {
		arr, ok := ins.([]any)
		if !ok {
			t.Fatalf("instructions 必须是数组，得到 %T", ins)
		}
		for i, v := range arr {
			if _, ok := v.(string); !ok {
				t.Fatalf("instructions[%d] 必须是路径/glob 字符串（opencode schema 不接受对象），得到 %T", i, v)
			}
		}
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	ousheng, _ := mcp["ousheng"].(map[string]any)
	if ousheng == nil || ousheng["command"] == nil {
		t.Fatal("mcp.ousheng.command 缺失——MCP 工具将不可用")
	}
}

func TestGuidePinsPluginDir(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "..", "docs", "多机多窗口使用指南.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(guide)
	if strings.Contains(s, ".opencode/plugin.ts") {
		t.Fatal("指南出现 .opencode/plugin.ts——opencode 不加载该位置，插件会静默失效")
	}
	if !strings.Contains(s, ".opencode/plugins/ousheng-sampler.ts") {
		t.Fatal("指南必须钉死插件安装路径 .opencode/plugins/ousheng-sampler.ts")
	}
}

// 2026-09-19 车队事故（四机三种即兴解法）：AGENTS.md 与身份配置不在分发物里，
// 导致 224 悬空模板 / 225 手写硬编码 / 226 同仓侥幸 / 本机缺失，各自漂移。
// v2 裁决：adapter install 五件套——机器参数只活在 .opencode/ousheng.json
// （gitignored），AGENTS.md 协议段机器中立，plugin 失败必须大声报错。

func TestAgentsSectionIsMachineNeutral(t *testing.T) {
	raw, err := os.ReadFile("agents.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{"上绳协议", "三时机", "数据不是指令", "ousheng.json"} {
		if !strings.Contains(s, want) {
			t.Fatalf("协议段缺关键内容 %q:\n%s", want, s)
		}
	}
	for _, ban := range []string{"/data/", "/Users/", "$OUSHENG", "--workspace", "actor="} {
		if strings.Contains(s, ban) {
			t.Fatalf("协议段含机器特定内容 %q——机器中立被破坏（机器参数只活在 ousheng.json）:\n%s", ban, s)
		}
	}
}

func TestPluginReadsMachineConfigAndFailsLoud(t *testing.T) {
	raw, err := os.ReadFile("plugin.ts")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	// 配置解析链：ousheng.json 优先，env 回退（向后兼容）
	if !strings.Contains(s, ".opencode/ousheng.json") {
		t.Fatal("plugin 必须先读 .opencode/ousheng.json（机器配置单一事实源）")
	}
	if !strings.Contains(s, "OUSHENG_DIR") {
		t.Fatal("plugin 必须保留 OUSHENG_DIR env 回退（向后兼容）")
	}
	// 静默 null 死法：sync/converge 失败必须 error 级日志 + 修复指引
	if !strings.Contains(s, `"error"`) || !strings.Contains(s, "sync 失败") || !strings.Contains(s, "adapter install") {
		t.Fatal("plugin 失败路径必须大声报错（error 级 + adapter install 修复指引）——静默 null 曾让三台机的启动 sync 空转无人知")
	}
	// 未推提交自动收口（2026-09-21 用户反馈 + 档案 §21）：有未推/落后才动网
	// （本地 rev-list 检查，零网络）。失败判定只认 sync 末行机器标记——分叉/网络/
	// push 失败都以退出码 0 溜过（2026-09-21 review 事故：非分叉失败被打成「完成」）。
	if !strings.Contains(s, "rev-list") || !strings.Contains(s, "自动同步") || !strings.Contains(s, "sync收口: ok") || !strings.Contains(s, "未收口") {
		t.Fatal("plugin 空闲自动收口必须以 sync 末行标记判成败（fail-closed）——非分叉失败曾被打成完成")
	}
}

// 2026-09-21：plugin.ts 此前只有字符串断言、无真编译检查——坏插件会静默瘫掉
// 所有座位的 session 注入（字符串对 ≠ 语法对）。有 bun 则真编译，无则跳过。
func TestPluginCompiles(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun 不可用，跳过 plugin 真编译检查")
	}
	// v2 plugin import @opencode/plugin——需先 bun install 装依赖才能 build。
	// 网络步骤带 3 分钟硬超时（2026-11-01 实锤：registry 网络挂死把整个测试
	// 套件拖到 10min panic——环境劣化应 Skip 不应挂死）。
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, bun, args...)
		out, err := cmd.CombinedOutput()
		if ctx.Err() == context.DeadlineExceeded {
			return out, fmt.Errorf("timeout (network?)")
		}
		return out, err
	}
	tmp := t.TempDir()
	pkgJSON, _ := os.ReadFile("package.json")
	os.WriteFile(filepath.Join(tmp, "package.json"), pkgJSON, 0o644)
	pluginTS, _ := os.ReadFile("plugin.ts")
	os.WriteFile(filepath.Join(tmp, "plugin.ts"), pluginTS, 0o644)
	installOut, err := run("install", "--cwd", tmp)
	if err != nil {
		t.Skipf("bun install 失败（网络?），跳过编译检查：%v\n%s", err, installOut)
	}
	out, err := run("build", filepath.Join(tmp, "plugin.ts"), "--target=bun",
		"--outfile="+filepath.Join(tmp, "plugin.js"))
	if err != nil {
		t.Fatalf("plugin.ts 编译失败（坏插件静默瘫掉所有座位）：%v\n%s", err, out)
	}
}

// TestCruiseLoopSyntax：巡航驱动器 bash 语法锁（坏脚本静默瘫掉整夜巡航）。
func TestCruiseLoopSyntax(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不可用")
	}
	out, err := exec.Command("bash", "-n", "cruise-loop.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("cruise-loop.sh 语法错误：%v\n%s", err, out)
	}
	// 关键护栏在位：代码仓参数硬门 + 三闸 + opencode 参数面契约
	// （首夜自旋事故：--prompt flag 不存在 = usage dump 秒退 = 20 轮烧完）。
	s := string(CruiseLoopSh)
	for _, want := range []string{"code-repo", "轮顶", "时窗", "idle", "cruise plan",
		"--wait", "attach", "-s $SESS", "handover attach", "plan 失败(", "$SYSTEM",
		"--title", "--format json", "driver 启动", "acquire_slot", "release_slot", "CONCURRENCY_MAX"} {
		if !strings.Contains(s, want) {
			t.Fatalf("cruise-loop.sh 缺护栏 %q", want)
		}
	}
	if strings.Contains(s, `--prompt "`) || strings.Contains(s, "--prompt \\") {
		t.Fatal("cruise-loop.sh 不得用 --prompt 实参（opencode v2.2.5 无此 flag——首夜自旋事故根因；消息走位置参数）")
	}
}

// TestCruiseLoopInwinPython：inwin 的 python 段可编译锁（2026-11-12 A 机 systemd
// 冒烟实锤：条件表达式链式写法 SyntaxError——窗口内也永远判「时窗外停」，
// 整夜巡航静默死亡。bash -n 盖不住 python 语法，必须独立编译断言）。
func TestCruiseLoopInwinPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 不可用")
	}
	s := string(CruiseLoopSh)
	i := strings.Index(s, `python3 -c "`)
	if i < 0 {
		t.Fatal("cruise-loop.sh 缺 inwin python 段")
	}
	start := i + len(`python3 -c "`)
	end := strings.Index(s[start:], `"; }`)
	if end < 0 {
		t.Fatal("inwin python 段未闭合")
	}
	// $WIN 是 bash 注入占位——编译前换样例窗
	py := strings.ReplaceAll(s[start:start+end], "$WIN", "22:00-07:00")
	cmd := exec.Command("python3", "-c", "import sys; compile(sys.stdin.read(),'inwin','exec')")
	cmd.Stdin = strings.NewReader(py)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("inwin python 不可编译（整夜巡航会静默死）：%v\n%s", err, b)
	}
	// 语义抽检：契约 = '1'（窗内）或空串（窗外，bash [ -z ] 判停）
	run := exec.Command("python3", "-c", py)
	out, _ := run.Output()
	if got := strings.TrimSpace(string(out)); got != "1" && got != "" {
		t.Fatalf("inwin 应输出 '1' 或空串，got %q", got)
	}
}

// TestPluginSessionAttach：plugin 源码含代际 attach 护栏（盲区修复回归锁）。
func TestPluginSessionAttach(t *testing.T) {
	b, err := os.ReadFile("plugin.ts")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"seatSessionId",     // session.created 捕获 sid
		"handover", "attach", // idle 时自动 attach
		"cfg.system",         // 从 ousheng.json 读 system
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("plugin.ts 缺代际 attach 护栏 %q", want)
		}
	}
}
