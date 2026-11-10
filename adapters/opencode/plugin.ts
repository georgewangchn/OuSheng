import { Plugin } from "@opencode/plugin"

// OuSheng v0.3 查看时机协议（低频事件驱动，非每 loop 轮询）：
//   时机① session 启动 → ousheng sync（pull + 索引 + 概览 + 我的上下文）
//   时机② 遇到 bug/问题 → 手动 ousheng context me / query_work_items（MCP 工具）
//   时机③ session 空闲/结束 → ousheng converge 收尾提醒
//
// opencode v2.0 plugin API（2026-10-19 升级适配）：
//   v1 `export const X: Plugin = async ({directory, client}) => {return {event: ...}}`
//   v2 `export default Plugin.define({id, setup(ctx)})` + `ctx.event.subscribe({signal})`
//   v1 的 Hooks.event 回调在 v2 不被调用——必须用事件流订阅。
//
// 本机配置解析链（2026-09-19 四机标准化裁决）：
//   .opencode/ousheng.json（adapter install 生成，机器本地）→ OUSHENG_DIR env → "."
// 失败必须大声报错（error 级 + 修复指引）——静默 null 曾让多台机的启动 sync
// 空转无人察觉（2026-09-19 车队事故：三台机三种即兴解法各自漂移）。

const OUSHENG_BIN = process.env.OUSHENG_BIN || "ousheng"

const BOARD_DIR = process.env.OUSHENG_BOARD_DIR || ".ousheng"
const BOARD_BIN = process.env.OUSHENG_BOARD_BIN || "board"

type MachineCfg = { workspace: string; actor: string; system: string }

// ousheng.json 每次直读（评审 §36）：cfgCache 永不失效——adapter install 重跑
// 后同进程读旧值（system 字段补写不生效）。事件低频（created/idle），直读无感。
async function readCfg(directory: string): Promise<MachineCfg> {
  // 回退链的兜底：env → "."
  const cfg: MachineCfg = {
    workspace: process.env.OUSHENG_DIR || ".",
    actor: process.env.OUSHENG_ACTOR || "",
    system: "",
  }
  try {
    const raw = await Bun.file(`${directory}/.opencode/ousheng.json`).text()
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed.workspace === "string" && parsed.workspace) {
      return {
        workspace: parsed.workspace,
        actor: typeof parsed.actor === "string" ? parsed.actor : "",
        system: typeof parsed.system === "string" ? parsed.system : "",
      }
    }
  } catch {
    // 无 ousheng.json / 解析失败 → env 回退（cfg 已初始化）
  }
  return cfg
}

type RunResult = { ok: boolean; out: string; err: string }

async function run(cmd: string[], cwd: string): Promise<RunResult> {
  try {
    const p = Bun.spawn({ cmd, cwd, stdout: "pipe", stderr: "pipe" })
    const [stdout, stderr] = await Promise.all([
      new Response(p.stdout).text(),
      new Response(p.stderr).text(),
    ])
    const exitCode = await p.exited
    return { ok: exitCode === 0, out: stdout.trim(), err: stderr.trim() }
  } catch (e) {
    return { ok: false, out: "", err: String(e) }
  }
}

type LogCtx = { app: { log(input: { body: { service: string; level: string; message: string } }): Promise<unknown> } }

async function log(client: LogCtx, level: "info" | "error", message: string) {
  await client.app
    .log({ body: { service: "ousheng-sampler", level, message } })
    .catch(() => {})
}

export default Plugin.define({
  id: "ousheng-sampler",
  async setup(ctx) {
    const directory: string = ctx.location.directory

    // 订阅事件流（v2：ctx.event.subscribe 替代 v1 Hooks.event 回调）
    const controller = new AbortController()
    void (async () => {
      for await (const event of ctx.event.subscribe({ signal: controller.signal })) {
        // 时机①：session 启动 —— 拉取 + 刷新 + 我的上下文 + 捕获 sid（代际制）
        let seatSessionId = ""
        if (event.type === "session.created") {
          const props = (event as any).properties || {}
          seatSessionId = props.sessionID || props.id || ""
          try {
            const cfg = await readCfg(directory)
            const args = cfg.actor ? ["sync", "--actor", cfg.actor] : ["sync"]
            const r = await run([OUSHENG_BIN, ...args, "--dir", cfg.workspace], directory)
            if (r.ok && r.out) {
              await log(ctx, "info", `[OuSheng] session 启动 sync:\n${r.out}`)
            } else {
              await log(
                ctx,
                "error",
                `[OuSheng] session 启动 sync 失败（workspace=${cfg.workspace}）：${r.err || r.out || "未知错误"}\n[OuSheng] 修复：在代码仓运行 ousheng adapter install --workspace <工作区路径>（或设 OUSHENG_DIR）`,
              )
            }
          } catch (err) {
            await log(ctx, "error", `[OuSheng] session.created sampling failed: ${String(err)}`)
          }
        }

        // 时机③：session 空闲 —— 收敛检查 + 收尾提醒
        if (event.type === "session.idle") {
          try {
            const cfg = await readCfg(directory)
            // converge 的 exit code：0=CONVERGED/IN_PROGRESS，1=BLOCKED（合法状态非错误）。
            // 不能用 run().ok 判——BLOCKED 时 ok=false 但 out 是正常收敛报告，误报"未知错误"。
            let result: string | null = null
            try {
              const p = Bun.spawn({ cmd: [OUSHENG_BIN, "converge", "--dir", cfg.workspace], cwd: directory, stdout: "pipe", stderr: "pipe" })
              const [stdout] = await Promise.all([new Response(p.stdout).text()])
              await p.exited
              result = stdout.trim() || null
            } catch {
              // converge 二进制缺失/spawn 失败 → 走 board fallback
            }
            let reminder: string
            if (result) {
              const status = result.split("\n")[0]?.trim() || "UNKNOWN"
              reminder =
                status === "CONVERGED"
                  ? "All work items done. Clean state."
                  : status === "BLOCKED"
                    ? "Workspace is BLOCKED — explicit blocked item, dangling dependency, cycle, or missing evidence/ack. Human intervention needed."
                    : "Workspace is IN_PROGRESS — if you changed work items this session, run ousheng work update / progress report / evidence add before ending, then ousheng context me."
              // 上下文交接（时机③并列提醒，便利层——权威在 AGENTS.md 协议段）：
              // 长任务 session 膨胀，收尾存一页导航态快照，下一班 handover show 续航。
              reminder +=
                "\nContext handover: if this session carried working state, pipe a ≤4KB nav summary (in-progress / key facts / next steps) to `ousheng handover save --system <system>` — the next session starts light via `handover show`."
              await log(ctx, "info", `[OuSheng] Convergence at session end:\n${result}\n${reminder}`)
            } else {
              const b = await run([BOARD_BIN, "converge", "--dir", BOARD_DIR], directory)
              const bresult = b.ok && b.out ? b.out : null
              if (bresult) {
                const status = bresult.match(/status:\s*(\w+)/)?.[1] || "UNKNOWN"
                reminder =
                  status === "CONVERGED"
                    ? "All contracts verified. No pending writes needed."
                    : status === "STUCK"
                      ? "Board is STUCK — dependency cycle or broken dependency. Human intervention needed."
                      : "Board is IN_PROGRESS — if you changed contracts this session, remember to write_board before ending."
                await log(ctx, "info", `[OuSheng] Convergence at session end:\n${bresult}\n${reminder}`)
              } else {
                await log(
                  ctx,
                  "error",
                  `[OuSheng] converge 失败（workspace=${cfg.workspace}）：${b.err || "未知错误"}。修复：ousheng adapter install --workspace <工作区路径>`,
                )
              }
            }
          } catch (err) {
            await log(ctx, "error", `[OuSheng] session.idle sampling failed: ${String(err)}`)
          }

          // 时机③（续）：代际指针补记（档案 §34 盲区修复：A 形态哨兵/web 发令
          // 场景下 agent 不知道自己的 sid——plugin 从 session.created 事件捕获，
          // idle 时自动 attach。agent 只管 save（内容），指针由 plugin/runner 管（基础设施）。
          if (seatSessionId) {
            try {
              const cfg = await readCfg(directory)
              if (cfg.system && cfg.actor) {
                await run([OUSHENG_BIN, "handover", "attach", "--dir", cfg.workspace,
                  "--system", cfg.system, "--actor", cfg.actor,
                  "--session", seatSessionId], directory)
              }
            } catch {
              // attach 非致命——指针缺位下轮 plan 无 attach 行，退化为新代（可用）
            }
          }

          // 时机③（续）：未推提交自动收口
          try {
            const cfg = await readCfg(directory)
            const u = await run(
              ["git", "-C", cfg.workspace, "rev-list", "--left-right", "--count", "@{u}...HEAD"],
              directory,
            )
            if (u.ok) {
              const f = u.out.trim().split(/\s+/)
              const behind = parseInt(f[0] ?? "0", 10) || 0
              const ahead = parseInt(f[1] ?? "0", 10) || 0
              if (ahead > 0 || behind > 0) {
                const r = await run([OUSHENG_BIN, "sync", "--dir", cfg.workspace], directory)
                if (r.ok && r.out.includes("sync收口: ok")) {
                  await log(ctx, "info", `[OuSheng] 自动同步（未推 ${ahead} / 落后 ${behind}）完成：\n${r.out}`)
                } else {
                  await log(
                    ctx,
                    "error",
                    `[OuSheng] 自动同步未收口——需人工处理（分叉/网络/权限），修复后跑 ousheng sync：\n${r.out || r.err}`,
                  )
                }
              }
            }
          } catch {
            // 自动同步失败无语义影响（doctor 水位检查同项可查）
          }
        }
      }
    })()

    return () => controller.abort()
  },
})
