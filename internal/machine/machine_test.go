package machine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ResolveActor 优先级：显式 > 座位配置（AI 窗口身份）> 工作区 me（仅同目录）> 拒绝。
func TestResolveActorPriority(t *testing.T) {
	ws := t.TempDir()
	seat := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".ousheng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seat, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 三来源皆无 → 拒
	if _, err := ResolveActor("", seat, ws); err == nil {
		t.Fatal("三来源皆无必须拒")
	}
	// me 兜底（仅同目录：CWD==workspace，PM 单机 cd 板仓形态）
	write(filepath.Join(ws, ".ousheng", "me"), "backend-agent\n")
	if a, err := ResolveActor("", ws, ws); err != nil || a != "backend-agent" {
		t.Fatalf("同目录 me 兜底失败: %q %v", a, err)
	}
	// 座位优先于 me（AI 窗口身份优先，防 AI 的写归到人名下）
	write(filepath.Join(seat, ".opencode", "ousheng.json"), `{"workspace":"x","actor":"test-agent"}`)
	if a, err := ResolveActor("", seat, ws); err != nil || a != "test-agent" {
		t.Fatalf("座位应优先于 me: %q %v", a, err)
	}
	// 显式最高
	if a, err := ResolveActor("explicit", seat, ws); err != nil || a != "explicit" {
		t.Fatalf("显式应最高: %q %v", a, err)
	}
}

// me 兜底仅同目录（2026-11-12，BUG-136 实栈事故，档案 §37）：板仓 me 是单槽位
// 机器本地文件，多座位共享板仓时任一座位覆写即污染全机——CWD 无座位配置且
// --dir 指向别处时，me 兜底必须 fail-loud（不确定即拒绝），不得静默错配。
func TestResolveActorMeFallbackOnlyColocated(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".ousheng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".ousheng", "me"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	caller := t.TempDir() // 无座位配置的 CWD（/tmp、家目录、其他仓）

	_, err := ResolveActor("", caller, ws)
	if err == nil {
		t.Fatal("CWD≠workspace 且无座位配置：me 兜底必须拒（BUG-136 静默错配形态）")
	}
	if !strings.Contains(err.Error(), "--actor") {
		t.Fatalf("报错须指引 --actor: %v", err)
	}
	// 同目录形态不受影响
	if a, err := ResolveActor("", ws, ws); err != nil || a != "data" {
		t.Fatalf("同目录 me 兜底应保留: %q %v", a, err)
	}
}

// RepoDirForSystem：映射优先，无映射回退工作区（monorepo）——CLI 与 MCP 同一语义。
func TestRepoDirForSystem(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".ousheng"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveRepoMap(ws, map[string]string{"data": "/srv/data"}); err != nil {
		t.Fatal(err)
	}
	if got := RepoDirForSystem(ws, "data"); got != "/srv/data" {
		t.Fatalf("映射未生效: %q", got)
	}
	if got := RepoDirForSystem(ws, "ui"); got != ws {
		t.Fatalf("无映射应回退工作区: %q", got)
	}
}
