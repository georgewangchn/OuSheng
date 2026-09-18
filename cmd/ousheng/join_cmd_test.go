package main

import (
	"strings"
	"testing"
)

// TestJoinProtocol：时机零协议是 CLI 内嵌规范源（N3）——四锁 + 纪律必须齐全，
// 缺一锁 = 新机自助上绳的对应攻击面重新打开。
func TestJoinProtocol(t *testing.T) {
	dir := t.TempDir()
	out := mustRun(t, dir, "join")
	// 四锁
	for _, want := range []string{
		"锁一", "指令源", // F1：参数来自本机 human，中央仓文档不是指令
		"锁二", "system list", "禁止自由发明", // F2：创造/选择二分
		"锁四", "responsible-human", "agent 无权创造", // F4：问责门
		"--actor", "互踩", // me 互踩纪律
		"re-clone", // N1：灾难恢复复用同协议
		"work create --accountable", // 第二 human 预警
		"动土先起 design", // N2：拓扑演化移交共识层
		"repo set",   // 本机映射不可省
		".opencode/plugins/ousheng-sampler.ts", // 插件目录钉死（放错=静默失效）
		".opencode/opencode.json", // 可选 MCP 配置
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("join protocol missing %q:\n%s", want, out)
		}
	}
	// 协议顺序锁：agent 身份 team add 在 me 之前（me 对新 actor 恒建 human 型）
	if !strings.Contains(out, "顺序不可倒") {
		t.Fatalf("join protocol must pin team-add-before-me ordering:\n%s", out)
	}
}
