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

		// 座位资产不得入库（2026-09-21 裁决，档案 §13）：二进制内嵌分发为单一来源，
		// 入库副本必漂移；gitignore 不回溯已跟踪文件，故单列检查。
		if out, err := gitOut(dir, "ls-files", "--", ".opencode/opencode.json", ".opencode/ousheng.json",
			".opencode/package.json", ".opencode/package-lock.json", ".opencode/bun.lock", ".opencode/plugins"); err == nil {
			if tracked := strings.TrimSpace(out); tracked != "" {
				check("FAIL", "座位资产被入库（"+strings.ReplaceAll(tracked, "\n", " ")+"）→ git rm --cached <路径>（档案 §13）")
			} else {
				check("PASS", "座位资产未入库（gitignore 生效）")
			}
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
			// 审计桶（2026-09-21，档案 §14）：不可归属写的机器可查形态。历史条目存在
			// 不 FAIL（审计记录不可删），但新写不应再产生（身份解析 + 写路径硬门已修）。
			if files, _ := filepath.Glob(filepath.Join(ws, ".ousheng", "activity", "*", "unknown.jsonl")); len(files) > 0 {
				lines := 0
				for _, f := range files {
					if b, err := os.ReadFile(f); err == nil {
						if s := strings.TrimSpace(string(b)); s != "" {
							lines += strings.Count(s, "\n") + 1
						}
					}
				}
				check("WARN", fmt.Sprintf("审计桶 unknown.jsonl 存在（%d 文件 / %d 条）——历史不可归属写；新写已被拒，查身份解析链", len(files), lines))
			} else {
				check("PASS", "无 unknown 审计桶（所有 activity 可归属）")
			}
			// 水位（无 fetch，按本地上游引用给建议）：未推/落后/分叉都是协作断点
			// （BUG-001 事故：工单烂在本地全舰队不可见，档案 §10）。
			if out, err := gitOut(ws, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
				if f := strings.Fields(out); len(f) == 2 {
					behind, ahead := atoiSafe(f[0]), atoiSafe(f[1])
					switch {
					case ahead > 0 && behind > 0:
						check("WARN", fmt.Sprintf("工作区与上游分叉（未推 %d / 落后 %d）→ 人工 git pull --rebase 后再 sync", ahead, behind))
					case ahead > 0:
						check("WARN", fmt.Sprintf("工作区有 %d 个未推提交 → ousheng sync 推送收口（未推提交全舰队不可见）", ahead))
					case behind > 0:
						check("WARN", fmt.Sprintf("工作区落后上游 %d 个提交 → ousheng sync 拉齐", behind))
					default:
						check("PASS", "工作区水位与上游一致")
					}
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

// gitOut：在 dir 跑 git 子命令并返回 stdout（非 git 仓/无 upstream 等由调用方忽略）。
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
