package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/adapters/opencode"
)

func TestAdapterInstall(t *testing.T) {
	dir := t.TempDir()
	out := mustRun(t, dir, "adapter", "install")
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

	// 幂等：第二次全部 unchanged
	out2 := mustRun(t, dir, "adapter", "install")
	if !strings.Contains(out2, "unchanged") || strings.Contains(out2, "created") {
		t.Fatalf("install 不幂等:\n%s", out2)
	}
}

func TestAdapterInstallFixesConfigAndFlagsLegacy(t *testing.T) {
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

	out := mustRun(t, dir, "adapter", "install")
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
