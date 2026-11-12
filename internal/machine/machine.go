// Package machine 收拢「机器本地配置」的读写：repos.yaml（system→代码仓映射）、
// .ousheng/me（本机默认身份）、.opencode/ousheng.json（座位身份）。
//
// 2026-09-21 审计裁决（档案 §14）：CLI 与 MCP 是同一套写路径的两个入口，身份解析
// 与证据仓定位必须同语义——空 actor 曾经由 MCP 静默写入 unknown 桶（审计空洞），
// git_commit 证据曾对工作区仓而非系统代码仓验证（语义漂移）。两套实现 = 漂移死法，
// 故落一处实现、两入口共用。
package machine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Me 读工作区默认身份（.ousheng/me）；无则空串。
func Me(workspace string) string {
	b, err := os.ReadFile(filepath.Join(workspace, ".ousheng", "me"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SeatActor 读座位身份（<seatDir>/.opencode/ousheng.json 的 actor）；无则空串。
// 座位配置是 AI 窗口身份的单一事实源（plugin 同读此文件）。
func SeatActor(seatDir string) string {
	raw, err := os.ReadFile(filepath.Join(seatDir, ".opencode", "ousheng.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		Actor string `json:"actor"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Actor)
}

// ResolveActor：显式 > 座位配置（AI 窗口身份）> 工作区 me（**仅同目录**）> 拒绝。
// 座位优先于 me：me 可能是 human（PM 机终端身份），而调用方多为 AI 窗口，
// 其身份在座位配置里——顺序保证 AI 的写不会被归到人名下。
//
// me 兜底仅 CWD==workspace 时生效（2026-11-12，BUG-136 实栈事故，档案 §37）：
// 板仓 me 是单槽位机器本地文件，多座位共享板仓时任一座位 `ousheng me` 覆写
// 即污染全机——CWD 无座位配置且 --dir 指向别处时静默取板仓 me = 身份错配，
// 必须 fail-loud（不确定即拒绝）。PM 单机 cd 板仓的同目录形态不受影响。
func ResolveActor(explicit, seatDir, workspace string) (string, error) {
	if a := strings.TrimSpace(explicit); a != "" {
		return a, nil
	}
	if a := SeatActor(seatDir); a != "" {
		return a, nil
	}
	if sameDir(seatDir, workspace) {
		if a := Me(workspace); a != "" {
			return a, nil
		}
	}
	return "", fmt.Errorf("无法确定 acting actor：传 --actor，或在本机座位仓跑（.opencode/ousheng.json），或 cd 进工作区用 me（--dir 指向外部工作区时板仓 me 不兜底——多座位共享板仓的静默错配已收紧，BUG-136）")
}

// sameDir 判两路径同目录（EvalSymlinks 归一，失败回退 Clean——/tmp 与
// /private/tmp 类符号链接差异不构成身份判定依据）。
func sameDir(a, b string) bool {
	norm := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Clean(r)
		}
		return filepath.Clean(p)
	}
	return norm(a) == norm(b)
}

// --- repos.yaml（本机 system→代码仓映射，gitignored）---

// ReposPath 返回本机映射文件路径（.ousheng/repos.yaml）。
func ReposPath(workspace string) string { return filepath.Join(workspace, ".ousheng", "repos.yaml") }

// LoadRepoMap 读映射；缺失/坏行静默跳过（本机配置，非事实源）。
func LoadRepoMap(workspace string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(ReposPath(workspace))
	if err != nil {
		return out
	}
	// 极简行格式："<system-id>: <path>"
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, ":"); i > 0 {
			out[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	return out
}

// SaveRepoMap 稳定排序写出映射。
func SaveRepoMap(workspace string, m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(ReposPath(workspace)), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, m[k])
	}
	return os.WriteFile(ReposPath(workspace), []byte(b.String()), 0o644)
}

// RepoDirForSystem 解析 system 的本机代码仓路径：repos.yaml 映射 > 工作区自身（monorepo）。
func RepoDirForSystem(workspace, system string) string {
	if p, ok := LoadRepoMap(workspace)[system]; ok && p != "" {
		return p
	}
	return workspace
}
