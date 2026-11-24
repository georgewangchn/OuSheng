package opencode

// plugin_behavior_test.go —— plugin 的行为锁（真跑 bun；不可用则跳过）。
//
// 背景（档案 §56 实锤）：TestPluginSessionAttach 只锁「源码含 seatSessionId /
// attach 字样」——字符串在位 ≠ 行为成立。b512376（11-09）把 `let seatSessionId`
// 声明在 `for await` 循环体内，每个事件迭代重置，session.idle 时恒为空串：
// A 形态（web 发令 / pm 一句话会话）的自动 attach 从未执行过一天。板面数据
// 吻合：A 形态日（11-17/22/23/24）有 handover save、零 attach。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginAutoAttachOnIdle(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun 不可用，跳过 plugin 行为锁")
	}
	src, err := os.ReadFile("plugin.ts")
	if err != nil {
		t.Skip("不在 adapter 目录")
	}
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "plugin.ts"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	// @opencode/plugin 桩：Plugin.define 原样返回配置（零网络——不 bun install）。
	pkg := filepath.Join(tmp, "node_modules", "@opencode", "plugin")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@opencode/plugin","version":"0.0.0","main":"index.js","type":"module"}`), 0o644)
	os.WriteFile(filepath.Join(pkg, "index.js"), []byte("export const Plugin = { define: (cfg) => cfg };\n"), 0o644)

	// 假 ousheng：记录 argv（attach 补记 = 「handover attach … --session <sid>」）。
	callLog := filepath.Join(tmp, "calls.log")
	fake := filepath.Join(tmp, "fake-ousheng")
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" >> \""+callLog+"\"\nexit 0\n"), 0o755)

	// 座位仓形态：.opencode/ousheng.json（机器本地配置）。
	ws := filepath.Join(tmp, "ws")
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, ".opencode", "ousheng.json"),
		[]byte(fmt.Sprintf(`{"workspace": %q, "actor": "data", "system": "lake"}`, ws)), 0o644)

	// 事件流：session.created（捕获 sid）→ session.idle（补记 attach）。两事件分属
	// 两次循环迭代——正是作用域 bug 的暴露面。
	harness := filepath.Join(tmp, "harness.ts")
	os.WriteFile(harness, []byte(fmt.Sprintf(`import plugin from "./plugin"
import fs from "fs"
const events = [
  { type: "session.created", properties: { sessionID: "sid-ABC123" } },
  { type: "session.idle" },
]
const ctx: any = {
  location: { directory: %q },
  event: { subscribe: () => (async function* () { for (const e of events) yield e })() },
  app: { log: async () => {} },
}
const mod: any = plugin
await (mod.setup ?? mod.default?.setup)(ctx)
const deadline = Date.now() + 8000
let seen = ""
while (Date.now() < deadline) {
  try { seen = fs.readFileSync(%q, "utf8") } catch {}
  if (seen.includes("attach")) break
  await new Promise((r) => setTimeout(r, 50))
}
process.stdout.write("SEEN:" + seen)
`, repo, callLog)), 0o644)

	cmd := exec.Command(bun, "run", harness)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"OUSHENG_BIN="+fake, "OUSHENG_BOARD_BIN="+fake,
		"BUN_INSTALL_CACHE_DIR="+filepath.Join(tmp, ".bun-cache"), // 不触全局缓存
	)
	out, err := cmd.CombinedOutput()
	got := string(out)
	if err != nil {
		t.Fatalf("harness 运行失败: %v\n%s", err, out)
	}
	if !strings.Contains(got, "handover attach") || !strings.Contains(got, "--session sid-ABC123") {
		t.Fatalf("session.idle 必须用 session.created 捕获的 sid 补记 attach（A 形态代际指针——实栈从未生效）:\n%s", got)
	}
}
