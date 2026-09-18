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
