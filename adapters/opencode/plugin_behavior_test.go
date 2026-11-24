package opencode

// plugin_behavior_test.go —— plugin 的行为锁 + 缺陷回放（真跑 bun；不可用则跳过）。
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

// runPluginHarness：在 bun 里真跑一份插件源码，回放事件流
// session.created(sid) → session.idle，返回假 ousheng 收到的全部调用参数。
func runPluginHarness(t *testing.T, src []byte) string {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun 不可用，跳过 plugin 行为锁")
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
let settled = 0
while (Date.now() < deadline) {
  try { seen = fs.readFileSync(%q, "utf8") } catch {}
  if (seen.includes("attach")) break
  // idle 处理里 attach 排在 converge 回退调用之后——见到回退调用再留 1.5s 余量，
  // 仍无 attach 即可判定「不会有了」（缺陷回放不必等满 deadline）。
  if (seen.includes("converge --dir .ousheng")) {
    if (!settled) settled = Date.now()
    else if (Date.now() - settled > 1500) break
  }
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
	if err != nil {
		t.Fatalf("harness 运行失败: %v\n%s", err, out)
	}
	return string(out)
}

func TestPluginAutoAttachOnIdle(t *testing.T) {
	got := runPluginHarness(t, PluginTS)
	if !strings.Contains(got, "handover attach") || !strings.Contains(got, "--session sid-ABC123") {
		t.Fatalf("session.idle 必须用 session.created 捕获的 sid 补记 attach（A 形态代际指针——实栈从未生效）:\n%s", got)
	}
}

// TestLockReplayPluginScopeDefect（档案 §58 缺陷回放）：一把锁是不是锁，只能用
// 反例证明——把历史缺陷施加回源码副本，看它红不红。
//
// 缺陷形态（b512376）：`let seatSessionId` 声明在 `for await` 循环体内。
// 三断言：①回放成立——缺陷行为复现（idle 无 attach）；②旧字符串锁
// （TestPluginSessionAttach 的四项 strings.Contains 判据）在缺陷源码上仍全绿
// ——它检测不了这个缺陷，所以它不是锁；③行为锁的判据在缺陷源码上失败
// ——行为锁对该缺陷可检测（detectable），锁的有效性由此立住。
// 锚点对不上 = 资产已变形 → 响亮失败，逼着重做回放（不许静默跳过）。
func TestLockReplayPluginScopeDefect(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun 不可用，跳过缺陷回放")
	}
	src := string(PluginTS)
	hoisted := "    let seatSessionId = \"\"\n"
	loopHead := "      for await (const event of ctx.event.subscribe({ signal: controller.signal })) {\n"
	if strings.Count(src, hoisted) != 1 || strings.Count(src, loopHead) != 1 {
		t.Fatalf("回放锚点失效（资产已变形）——按新形态重做缺陷回放，不许跳过：hoisted=%d loop=%d",
			strings.Count(src, hoisted), strings.Count(src, loopHead))
	}
	sabotaged := strings.Replace(src, hoisted, "", 1)
	sabotaged = strings.Replace(sabotaged, loopHead, loopHead+"        let seatSessionId = \"\"\n", 1)

	// ② 旧字符串锁的判据：在缺陷源码上全绿——证明「字符串在位 ≠ 行为成立」。
	for _, want := range []string{"seatSessionId", "handover", "attach", "cfg.system"} {
		if !strings.Contains(sabotaged, want) {
			t.Fatalf("回放前提不成立：缺陷源码本应仍含 %q", want)
		}
	}
	// ①③ 行为：缺陷源码上 idle 不得出现 attach——这正是行为锁会红的原因。
	got := runPluginHarness(t, []byte(sabotaged))
	if strings.Contains(got, "handover attach") {
		t.Fatalf("回放未复现缺陷（缺陷源码上 attach 竟然发生了）——回放失效，需重做:\n%s", got)
	}
	if !strings.Contains(got, "converge") {
		t.Fatalf("回放前提不成立：idle 事件应已到达（有 converge 调用）:\n%s", got)
	}
}
