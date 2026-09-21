package machine

import (
	"os"
	"path/filepath"
	"testing"
)

// ResolveActor 优先级：显式 > 座位配置（AI 窗口身份）> 工作区 me > 拒绝。
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
	// me 兜底
	write(filepath.Join(ws, ".ousheng", "me"), "backend-agent\n")
	if a, err := ResolveActor("", seat, ws); err != nil || a != "backend-agent" {
		t.Fatalf("me 兜底失败: %q %v", a, err)
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
