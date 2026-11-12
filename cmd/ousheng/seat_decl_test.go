package main

// §41 座位声明锁：新 session 冷启动三问（我在哪台机/板仓在哪/系统契约卡在哪）
// context me 必答；缺卡降级为空不报错（诚实降级）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/internal/testfix"
)

func TestContextSeatDeclaration(t *testing.T) {
	dir := testfix.Setup(t)
	mustRun(t, dir, "me", "pm")
	mustRun(t, dir, "system", "add", "s")
	mustRun(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "s", "--machine", "aliyun-a")
	// 板上放 architecture 卡 + 登记 contract_doc（系统信息卡双源）
	if err := writeArchitecture(t, dir, "s", "# s 架构\n"); err != nil {
		t.Fatal(err)
	}

	out := mustRun(t, dir, "context", "me", "--actor", "data")
	for _, want := range []string{
		`"machine": "aliyun-a"`,          // 我在哪台机（§40 registry）
		`"board": `,                       // 板仓在哪（cmd 层绝对路径）
		"architecture/s.md",               // 系统卡指针（板上架构卡）
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("context me 缺座位声明 %q:\n%s", want, out)
		}
	}
}

func TestContextSeatCardsDegrade(t *testing.T) {
	dir := testfix.Setup(t) // fixture 无 architecture/无 contract_doc
	mustRun(t, dir, "me", "pm")
	mustRun(t, dir, "system", "add", "s")
	mustRun(t, dir, "team", "add", "x", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "s")

	out := mustRun(t, dir, "context", "me", "--actor", "x")
	if strings.Contains(out, "system_cards") && strings.Contains(out, "architecture") {
		t.Fatalf("无卡不应伪造指针（诚实降级）:\n%s", out)
	}
	if !strings.Contains(out, `"board": `) {
		t.Fatalf("board 指针恒有:\n%s", out)
	}
}

func writeArchitecture(t *testing.T, dir, sys, body string) error {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "architecture")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p, sys+".md"), []byte(body), 0o644)
}
