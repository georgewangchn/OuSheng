package main

// cmdDoctor：`ousheng doctor [--dir <代码仓>]`。部署标准自检（2026-09-20 裁决：
// 延后清单判据「多人反复踩配置坑」实锤转正——本会话三起：每会话权限弹窗、
// 旧 actor 残留、插件/协议段漂移无人察觉）。
//
// 三层检查：机器级（二进制/MCP）/ 座位级（五件套+全局放行）/ 工作区级
// （me/repo 映射有效性）。铁律：
//   - 检查不修复：修复单一入口 = `adapter install`（幂等）或对应 registry 命令；
//   - FAIL 必带修复命令（→ 后缀），任一 FAIL 退出码 1（可脚本化）；
//   - WARN 不影响退出码（MCP 缺失只降通道不降功能）；
//   - 用户全局配置解析失败降级 WARN 不是 FAIL（不碰用户配置原则，档案 §11）；
//   - plugin 逐字节比对（install 恒同步语义），opencode.json/package.json 只查
//     存在（install 保留语义）——检查口径对齐写入口径。

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ousheng/adapters/opencode"
	"ousheng/internal/state/gityaml"
)

func cmdDoctor(args []string, stdout, stderr io.Writer) int {
	fs := &flagSetWithDir{FlagSet: newFlagSet("doctor")}
	fs.dir = fs.String("dir", ".", "code repo dir (default: .)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	fails := 0
	check := func(status, msg string) {
		fmt.Fprintf(stdout, "[%-4s] %s\n", status, msg)
		if status == "FAIL" {
			fails++
		}
	}

	// --- 机器级 ---
	check("PASS", versionString())
	if _, err := exec.LookPath("ousheng-mcp"); err == nil {
		check("PASS", "ousheng-mcp 在 PATH")
	} else {
		check("WARN", "ousheng-mcp 不在 PATH——MCP 工具通道不可用（CLI 通道不受影响）")
	}

	// --- 座位级 + 工作区定位 ---
	cfgPath := filepath.Join(dir, ".opencode", "ousheng.json")
	ws, actor := "", ""
	hasSeatCfg := false
	if raw, err := os.ReadFile(cfgPath); err == nil {
		hasSeatCfg = true
		var mc struct {
			Workspace string `json:"workspace"`
			Actor     string `json:"actor"`
		}
		if err := json.Unmarshal(raw, &mc); err != nil {
			check("FAIL", fmt.Sprintf("ousheng.json 非法（%v）→ ousheng adapter install", err))
		} else {
			ws, actor = mc.Workspace, mc.Actor
		}
	} else if adapterAssetsPresent(dir) {
		check("FAIL", "检测到 adapter 资产但无 .opencode/ousheng.json（旧形态残留）→ ousheng adapter install --workspace <工作区>")
	} else {
		check("INFO", "当前目录非 adapter 座位（无 .opencode/ousheng.json）")
	}
	if ws == "" {
		ws = resolveAdapterWorkspace("", dir)
	}

	// 座位级：工作区/身份/plugin/协议段/全局放行
	if ws != "" {
		if _, err := os.Stat(filepath.Join(ws, ".ousheng", "project.yaml")); err != nil {
			check("FAIL", fmt.Sprintf("工作区 %s 无效（缺 .ousheng/project.yaml）→ ousheng adapter install --workspace <工作区>", ws))
		} else if hasSeatCfg && actor != "" {
			if _, err := gityaml.Open(ws).GetActor(actor); err != nil {
				check("FAIL", fmt.Sprintf("ousheng.json actor=%s 未注册 → ousheng adapter install --actor <身份>（或 team add）", actor))
			} else {
				check("PASS", fmt.Sprintf("座位身份 actor=%s（workspace=%s）已注册", actor, ws))
			}
		}
		switch globalPermissionStatus(ws) {
		case "allow":
			check("PASS", "全局放行 "+ws+"/** 已配置")
		case "unparsable":
			check("WARN", "全局配置解析失败（JSONC 等）——按档案 §11 手动确认 external_directory")
		default:
			check("FAIL", "全局放行缺 "+ws+"/** → ousheng adapter install")
		}
	}
	if hasSeatCfg {
		pluginPath := filepath.Join(dir, ".opencode", "plugins", "ousheng-sampler.ts")
		if raw, err := os.ReadFile(pluginPath); err != nil {
			check("FAIL", "插件缺失（.opencode/plugins/ousheng-sampler.ts）→ ousheng adapter install")
		} else if string(raw) != string(opencode.PluginTS) {
			check("FAIL", "插件与内嵌资产不一致（版本漂移）→ ousheng adapter install")
		} else {
			check("PASS", "插件与内嵌资产逐字节一致")
		}
		for _, a := range []string{filepath.Join(dir, ".opencode", "opencode.json"), filepath.Join(dir, ".opencode", "package.json")} {
			if _, err := os.Stat(a); err != nil {
				check("FAIL", "adapter 资产缺失 "+filepath.Base(a)+" → ousheng adapter install")
			}
		}
		block := agentsBegin + "\n" + strings.TrimRight(string(opencode.AgentsMD), "\n") + "\n" + agentsEnd
		if raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")); err != nil {
			check("FAIL", "AGENTS.md 未装 → ousheng adapter install")
		} else if !strings.Contains(string(raw), agentsBegin) {
			check("FAIL", "AGENTS.md 缺标记段 → ousheng adapter install")
		} else if !strings.Contains(string(raw), block) {
			check("FAIL", "AGENTS.md 协议段过期（升级后未重跑）→ ousheng adapter install")
		} else {
			check("PASS", "AGENTS.md 协议段 = 当前版本")
		}
	}

	// --- 工作区级：me / repo 映射 ---
	if ws != "" {
		if _, err := os.Stat(filepath.Join(ws, ".ousheng", "project.yaml")); err == nil {
			if raw, err := os.ReadFile(mePath(ws)); err == nil {
				me := strings.TrimSpace(string(raw))
				if me == "" {
					check("FAIL", "me 为空 → ousheng me <id>")
				} else if _, err := gityaml.Open(ws).GetActor(me); err != nil {
					check("FAIL", fmt.Sprintf("me=%s 未注册 → ousheng me <id>", me))
				} else {
					check("PASS", "me="+me+" 已注册")
				}
			} else {
				check("FAIL", "工作区未设默认身份 → ousheng me <id>")
			}
			repos := loadRepoMap(ws)
			for sys, path := range repos {
				if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
					check("FAIL", fmt.Sprintf("repo 映射 %s → %s 失效（非 git 仓或不存在）→ ousheng repo set %s <路径>", sys, path, sys))
				} else {
					check("PASS", "repo 映射 "+sys+" → "+path)
				}
			}
		}
	}

	if fails > 0 {
		fmt.Fprintf(stdout, "doctor: %d 项 FAIL（修复后重跑；adapter 类一键 ousheng adapter install）\n", fails)
		return 1
	}
	fmt.Fprintln(stdout, "doctor: 全部通过")
	return 0
}

// adapterAssetsPresent：无 ousheng.json 但有 plugin/协议段等资产 = 旧形态残留。
func adapterAssetsPresent(dir string) bool {
	for _, p := range []string{
		filepath.Join(dir, ".opencode", "plugins", "ousheng-sampler.ts"),
		filepath.Join(dir, ".opencode", "opencode.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// globalPermissionStatus：只读检查（doctor 不写任何配置）。
// allow=已放行；unparsable=用户全局配置解析失败；missing=无放行规则。
func globalPermissionStatus(ws string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "missing"
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		return "missing"
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "unparsable"
	}
	perm, ok := cfg["permission"].(map[string]any)
	if cfg["permission"] != nil && !ok {
		return "unparsable"
	}
	ext, ok := perm["external_directory"].(map[string]any)
	if perm["external_directory"] != nil && !ok {
		return "unparsable"
	}
	if ext[ws+"/**"] == "allow" {
		return "allow"
	}
	return "missing"
}
