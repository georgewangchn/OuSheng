package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ousheng/adapters/opencode"
)

// cmdAdapter：`ousheng adapter install [--dir <代码仓>]`。
//
// 适配器资产内嵌于 CLI（adapters/opencode 的 go:embed），本命令幂等写出：
// 升级二进制后每个代码仓重跑一次即完成同步——散在代码仓的副本不会自动更新
// （2026-09-16 rdc-05 事故教训：joins 协议曾只说"挂 plugin"，路径漂移导致
// opencode 启动失败 / 插件静默失效）。
func cmdAdapter(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "install" {
		fmt.Fprintln(stderr, "usage: ousheng adapter <install> [--dir <code-repo>]")
		return 2
	}
	fs := newFS("adapter install")
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	oc := filepath.Join(dir, ".opencode")
	plugins := filepath.Join(oc, "plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// plugin：我们的资产，恒覆盖到版本一致（这是"升级同步"的载体）。
	st, err := writeAsset(filepath.Join(plugins, "ousheng-sampler.ts"), opencode.PluginTS, true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "plugin        .opencode/plugins/ousheng-sampler.ts  %s\n", st)

	// package.json：仅在缺失时创建，不覆盖用户已有依赖。
	st, err = writeAsset(filepath.Join(oc, "package.json"), opencode.PackageJSON, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "package.json  .opencode/package.json  %s\n", st)

	// opencode.json（MCP）：缺失则写内嵌配置；已存在则只修 instructions 的
	// 非法对象数组（其余键原样保留），不覆盖用户自定义。
	cfgPath := filepath.Join(oc, "opencode.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		st, err = writeAsset(cfgPath, opencode.ConfigJSON, false)
	} else {
		st, err = fixInstructions(cfgPath, dir)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "opencode.json .opencode/opencode.json  %s\n", st)

	// 历史遗留：旧指南把它放在 .opencode/plugin.ts——opencode 不加载该位置。
	if _, err := os.Stat(filepath.Join(oc, "plugin.ts")); err == nil {
		fmt.Fprintln(stdout, "提示：发现 .opencode/plugin.ts（opencode 不加载此位置，插件静默失效），可删除")
	}
	fmt.Fprintln(stdout, "完成。opencode 重启后生效；MCP 需 ousheng-mcp 在 PATH。")
	return 0
}

// writeAsset 写资产；force=false 时已存在则保留。返回 created/updated/unchanged/exists（保留）。
func writeAsset(path string, data []byte, force bool) (string, error) {
	old, err := os.ReadFile(path)
	if err == nil {
		if !force {
			return "exists（保留）", nil
		}
		if string(old) == string(data) {
			return "unchanged", nil
		}
		return "updated", os.WriteFile(path, data, 0o644)
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return "created", os.WriteFile(path, data, 0o644)
}

// fixInstructions 修正已存在配置里 instructions 的非法对象数组（opencode schema
// 只接受路径/glob 字符串）；悬空路径移除，其余键原样保留。
func fixInstructions(cfgPath, proj string) (string, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", err
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return "（JSON 非法，未改动）", nil
	}
	ins, ok := d["instructions"].([]any)
	if !ok {
		return "unchanged", nil
	}
	needFix := false
	for _, v := range ins {
		if _, ok := v.(string); !ok {
			needFix = true
			break
		}
	}
	if !needFix {
		return "unchanged", nil
	}
	var keep []string
	for _, v := range ins {
		var p string
		switch t := v.(type) {
		case string:
			p = t
		case map[string]any:
			p, _ = t["path"].(string)
		}
		if p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			if _, err := os.Stat(p); err == nil {
				keep = append(keep, p)
			}
			continue
		}
		for _, base := range []string{proj, filepath.Join(proj, ".opencode")} {
			if _, err := os.Stat(filepath.Join(base, p)); err == nil {
				keep = append(keep, p)
				break
			}
		}
	}
	if len(keep) > 0 {
		d["instructions"] = keep
	} else {
		delete(d, "instructions")
	}
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	return "instructions 已修正", os.WriteFile(cfgPath, append(out, '\n'), 0o644)
}
