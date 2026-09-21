package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ousheng/adapters/opencode"
)

// cmdAdapter：`ousheng adapter install [--dir <代码仓>] [--workspace <工作区>]`。
//
// 适配器资产内嵌于 CLI（adapters/opencode 的 go:embed），本命令幂等写出；
// 升级二进制后每个代码仓重跑一次即完成同步——散在代码仓的副本不会自动更新
// （2026-09-16 rdc-05 事故教训：joins 协议曾只说"挂 plugin"，路径漂移导致
// opencode 启动失败 / 插件静默失效）。
//
// v2（2026-09-19 四机标准化裁决）：五件套——三件资产之外新增
//   4. .opencode/ousheng.json：机器本地配置 {workspace, actor}（恒同步 + gitignore，
//      plugin 的单一事实源，取代散落各机的手写 AGENTS.md / env 即兴解法）；
//   5. AGENTS.md 协议段：机器中立纪律文本，标记段幂等替换，段外内容不触碰。
// 工作区解析链：--workspace > 同仓检测（--dir 自带 .ousheng）> 既有 ousheng.json
// > $OUSHENG_DIR。adapter 的 --dir 语义是代码仓（默认 "."），与工作区命令的
// --dir（默认 $OUSHENG_DIR）不同，故不走 newFS。
func cmdAdapter(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "install" {
		fmt.Fprintln(stderr, "usage: ousheng adapter <install> [--dir <code-repo>] [--workspace <ousheng-workspace>]")
		return 2
	}
	fs := &flagSetWithDir{FlagSet: newFlagSet("adapter install")}
	fs.dir = fs.String("dir", ".", "code repo dir (default: .)")
	wsFlag := fs.String("workspace", "", "ousheng workspace dir (default: colocated .ousheng > .opencode/ousheng.json > $OUSHENG_DIR)")
	actorFlag := fs.String("actor", "", "AI 窗口身份写入 ousheng.json（默认 me）——PM 机 AI 会话 ≠ me 时用")
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	oc := filepath.Join(dir, ".opencode")
	plugins := filepath.Join(oc, "plugins")

	// 工作区解析（身份/路径只能来自本机输入：flag / 本机文件 / 本机 env）
	ws := resolveAdapterWorkspace(*wsFlag, dir)
	if ws == "" {
		fmt.Fprintln(stderr, "无法确定 ousheng 工作区：--workspace 未给、--dir 不是工作区、.opencode/ousheng.json 不存在、$OUSHENG_DIR 未设。")
		fmt.Fprintln(stderr, "用法：ousheng adapter install --dir <代码仓> --workspace <工作区路径>")
		return 1
	}
	if _, err := os.Stat(filepath.Join(ws, ".ousheng", "project.yaml")); err != nil {
		fmt.Fprintf(stderr, "%s 不是 ousheng 工作区（缺 .ousheng/project.yaml）\n", ws)
		return 1
	}
	// actor：--actor 覆盖 > me（PM 机形态：AI 会话用独立 agent 身份，me 留给 human 终端亲操）
	actor := strings.TrimSpace(*actorFlag)
	if actor == "" {
		actor = readWorkspaceActor(ws)
	}

	if err := os.MkdirAll(plugins, 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	// 1. plugin：我们的资产，恒覆盖到版本一致（这是"升级同步"的载体）。
	st, err := writeAsset(filepath.Join(plugins, "ousheng-sampler.ts"), opencode.PluginTS, true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "plugin        .opencode/plugins/ousheng-sampler.ts  %s\n", st)

	// 2. package.json：仅在缺失时创建，不覆盖用户已有依赖。
	st, err = writeAsset(filepath.Join(oc, "package.json"), opencode.PackageJSON, false)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "package.json  .opencode/package.json  %s\n", st)

	// 3. opencode.json（MCP）：缺失则写内嵌配置；已存在则只修 instructions 的
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

	// 4. ousheng.json：机器本地配置（plugin 的单一事实源），恒同步。
	mc, err := json.MarshalIndent(map[string]string{"workspace": ws, "actor": actor}, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	st, err = writeAsset(filepath.Join(oc, "ousheng.json"), append(mc, '\n'), true)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "ousheng.json  .opencode/ousheng.json  %s\n", st)

	// 座位资产不入库（2026-09-21 裁决，档案 §13）：资产由二进制内嵌分发（adapter
	// install 即写出），入库副本必漂移（同「协议外置分发 = 漂移死法」）；且同事
	// clone 代码仓后开 opencode，插件因无 ousheng.json 每会话大声报错。gitignore
	// 覆盖全部座位资产，只留 skills/ 等仓库自有内容。
	st = "unchanged"
	for _, line := range []string{"ousheng.json", "opencode.json", "package.json", "package-lock.json", "bun.lock", "node_modules/", "plugins/"} {
		if ls := ensureLine(filepath.Join(oc, ".gitignore"), line); ls != "unchanged" && st == "unchanged" {
			st = ls
		}
	}
	fmt.Fprintf(stdout, "gitignore     .opencode/.gitignore  %s\n", st)

	// 5. AGENTS.md：机器中立协议段，标记段幂等替换，段外内容不触碰。
	st, err = upsertAgentsSection(filepath.Join(dir, "AGENTS.md"), opencode.AgentsMD)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "AGENTS.md     AGENTS.md  %s\n", st)

	// 6. 全局放行：工作区在代码仓之外，opencode 默认 external_directory=ask
	//（TUI 的 "always" 只活当前会话），不配置则每个新会话访问工作区都弹权限
	// 询问（2026-09-20 车队反馈：224/225 每次启动都问 /data/kanban）。工作区
	// 路径是机器本地信息，入库的五件套放不下——落全局配置，软失败不阻塞。
	pst, perr := upsertGlobalPermission(ws)
	if perr != nil {
		fmt.Fprintf(stderr, "全局放行跳过：%v\n", perr)
	} else {
		fmt.Fprintf(stdout, "permission    ~/.config/opencode/opencode.json  %s\n", pst)
	}

	// 历史遗留：旧指南把它放在 .opencode/plugin.ts——opencode 不加载该位置。
	if _, err := os.Stat(filepath.Join(oc, "plugin.ts")); err == nil {
		fmt.Fprintln(stdout, "提示：发现 .opencode/plugin.ts（opencode 不加载此位置，插件静默失效），可删除")
	}
	fmt.Fprintln(stdout, "完成。opencode 重启后生效；MCP 需 ousheng-mcp 在 PATH。")
	return 0
}

// upsertGlobalPermission：把 `permission.external_directory[<workspace>/**]="allow"`
// 合并进 ~/.config/opencode/opencode.json。合并语义只增不删（key=工作区路径模式，
// 自标识）；既有键全保留；首次改动前备份 .bak-ousheng；解析失败（JSONC 等）或
// permission/external_directory 非对象结构时拒绝改写、给人话指引——不碰用户配置。
func upsertGlobalPermission(ws string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cfgDir := filepath.Join(home, ".config", "opencode")
	path := filepath.Join(cfgDir, "opencode.json")
	pattern := ws + "/**"

	cfg := map[string]any{}
	existed := false
	if raw, err := os.ReadFile(path); err == nil {
		existed = true
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return "", fmt.Errorf("%s 解析失败（%v）——请手动在 permission.external_directory 加 %q: allow", path, err, pattern)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	perm, ok := cfg["permission"].(map[string]any)
	if existed && cfg["permission"] != nil && !ok {
		return "", fmt.Errorf("permission 非对象结构，不覆盖——请手动加 external_directory %q: allow", pattern)
	}
	if perm == nil {
		perm = map[string]any{}
		cfg["permission"] = perm
	}
	ext, ok := perm["external_directory"].(map[string]any)
	if perm["external_directory"] != nil && !ok {
		return "", fmt.Errorf("permission.external_directory 非对象结构，不覆盖——请手动加 %q: allow", pattern)
	}
	if ext == nil {
		ext = map[string]any{}
		perm["external_directory"] = ext
	}
	if ext[pattern] == "allow" {
		return "unchanged", nil
	}
	if existed {
		if _, err := os.Stat(path + ".bak-ousheng"); err != nil {
			if raw, _ := os.ReadFile(path); raw != nil {
				_ = os.WriteFile(path+".bak-ousheng", raw, 0o644)
			}
		}
	}
	ext[pattern] = "allow"
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return "", err
	}
	if existed {
		return "updated", nil
	}
	return "created", nil
}

// resolveAdapterWorkspace：flag > 同仓检测 > 既有机器配置 > env。
func resolveAdapterWorkspace(flagVal, dir string) string {
	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	if flagVal != "" {
		return abs(flagVal)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ousheng", "project.yaml")); err == nil {
		return abs(dir)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, ".opencode", "ousheng.json")); err == nil {
		var c map[string]string
		if json.Unmarshal(raw, &c) == nil && c["workspace"] != "" {
			return c["workspace"]
		}
	}
	if e := os.Getenv("OUSHENG_DIR"); e != "" {
		return abs(e)
	}
	return ""
}

// readWorkspaceActor：从工作区 me 文件读本机身份；缺失返回空（sync 可无 actor）。
func readWorkspaceActor(ws string) string {
	raw, err := os.ReadFile(filepath.Join(ws, ".ousheng", "me"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// ensureLine：确保文件含指定行（不存在则建，缺行则补）。
func ensureLine(path, line string) string {
	old, err := os.ReadFile(path)
	if err == nil {
		for _, l := range strings.Split(string(old), "\n") {
			if strings.TrimSpace(l) == line {
				return "unchanged"
			}
		}
		if err := os.WriteFile(path, append(append(old, '\n'), []byte(line+"\n")...), 0o644); err == nil {
			return "updated"
		}
		return "exists（写入失败）"
	}
	if !os.IsNotExist(err) {
		return "exists（读取失败）"
	}
	if os.WriteFile(path, []byte(line+"\n"), 0o644) == nil {
		return "created"
	}
	return "exists（写入失败）"
}

const (
	agentsBegin = "<!-- ousheng:begin -->"
	agentsEnd   = "<!-- ousheng:end -->"
)

// upsertAgentsSection：AGENTS.md 的受管协议段。无文件则创建；有标记段则
// 强制回到内嵌版本（升级同步载体）；无标记段则追加。段外内容一律不触碰。
func upsertAgentsSection(path string, section []byte) (string, error) {
	block := agentsBegin + "\n" + strings.TrimRight(string(section), "\n") + "\n" + agentsEnd
	old, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "created", os.WriteFile(path, []byte(block+"\n"), 0o644)
	}
	if err != nil {
		return "", err
	}
	s := string(old)
	i, j := strings.Index(s, agentsBegin), strings.Index(s, agentsEnd)
	if i < 0 || j < 0 || j < i {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		return "appended", os.WriteFile(path, []byte(s+"\n"+block+"\n"), 0o644)
	}
	end := j + len(agentsEnd)
	if s[i:end] == block {
		return "unchanged", nil
	}
	return "updated", os.WriteFile(path, []byte(s[:i]+block+s[end:]), 0o644)
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
