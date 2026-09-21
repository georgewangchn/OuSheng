// 2026-09-16 事故（rdc-05）：适配器配置的 instructions 写成对象数组
// {path, description}，而 opencode schema 只接受路径/glob 字符串数组——
// 文件拷进 .opencode/ 后 opencode 启动即失败（Configuration is invalid:
// Expected string）。同时使用指南把插件拷到 .opencode/plugin.ts，而
// opencode 只从 .opencode/plugins/ 加载，插件静默失效。两个 bug 都是
// "拷进别人机器才炸" 的分发物，故就地加锁。

package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	// 未推提交提醒（2026-09-21，档案 §10/§15）：收尾 sync 的结构化兜底——不自动推
	// （idle 频繁触发，网络动作会变成每 loop 轮询），只大声提醒。
	if !strings.Contains(s, "rev-list") || !strings.Contains(s, "未推提交") {
		t.Fatal("plugin 空闲时必须提醒未推提交（未推提交全舰队不可见，BUG-001 事故）")
	}
}
