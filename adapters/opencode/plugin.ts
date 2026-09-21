import type { Plugin } from "@opencode-ai/plugin"

// OuSheng v0.3 查看时机协议（低频事件驱动，非每 loop 轮询）：
//   时机① session 启动 → ousheng sync（pull + 索引 + 概览 + 我的上下文）
//   时机② 遇到 bug/问题 → 手动 ousheng context me / query_work_items（MCP 工具）
//   时机③ session 空闲/结束 → ousheng converge 收尾提醒
//
// 本机配置解析链（2026-09-19 四机标准化裁决）：
//   .opencode/ousheng.json（adapter install 生成，机器本地）→ OUSHENG_DIR env → "."
// 失败必须大声报错（error 级 + 修复指引）——静默 null 曾让多台机的启动 sync
// 空转无人察觉（2026-09-19 车队事故：三台机三种即兴解法各自漂移）。

const OUSHENG_BIN = process.env.OUSHENG_BIN || "ousheng"

const BOARD_DIR = process.env.OUSHENG_BOARD_DIR || ".ousheng"
const BOARD_BIN = process.env.OUSHENG_BOARD_BIN || "board"

type MachineCfg = { workspace: string; actor: string }

const cfgCache = new Map<string, MachineCfg>()

async function readCfg(directory: string): Promise<MachineCfg> {
  const cached = cfgCache.get(directory)
  if (cached) return cached
  // 回退链的兜底：env → "."
  let cfg: MachineCfg = {
    workspace: process.env.OUSHENG_DIR || ".",
    actor: process.env.OUSHENG_ACTOR || "",
  }
  try {
    const raw = await Bun.file(`${directory}/.opencode/ousheng.json`).text()
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed.workspace === "string" && parsed.workspace) {
      cfg = {
        workspace: parsed.workspace,
        actor: typeof parsed.actor === "string" ? parsed.actor : "",
      }
    }
  } catch {
    // 无 ousheng.json / 解析失败 → env 回退（cfg 已初始化）
  }
  cfgCache.set(directory, cfg)
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

async function log(
  client: Parameters<Parameters<Plugin>[0]>[0]["client"],
  level: "info" | "error",
  message: string,
) {
  await client.app
    .log({ body: { service: "ousheng-sampler", level, message } })
    .catch(() => {})
}

export const OuShengSampler: Plugin = async ({ directory, client }) => {
  return {
    event: async ({ event }) => {
      // 时机①：session 启动 —— 拉取 + 刷新 + 我的上下文
      if (event.type === "session.created") {
        try {
          const cfg = await readCfg(directory)
          const args = cfg.actor ? ["sync", "--actor", cfg.actor] : ["sync"]
          let r = await run([OUSHENG_BIN, ...args, "--dir", cfg.workspace], directory)
          let result = r.ok && r.out ? r.out : null
          // v0.3 优先；找不到 workspace 时 sync 失败 → v1 回退
          if (!result) {
            const b = await run([BOARD_BIN, "read", "--dir", BOARD_DIR], directory)
            result = b.ok && b.out ? b.out : null
          }
          if (result) {
            await log(
              client,
              "info",
              `[OuSheng] 本机 actor=${cfg.actor || "?"} workspace=${cfg.workspace}\n[OuSheng] Engineering context at session start:\n${result}`,
            )
          } else {
            await log(
              client,
              "error",
              `[OuSheng] session 启动 sync 失败（workspace=${cfg.workspace}）：${r.err || r.out || "未知错误"}\n[OuSheng] 修复：在代码仓运行 ousheng adapter install --workspace <工作区路径>（或设 OUSHENG_DIR）`,
            )
          }
        } catch (err) {
          await log(client, "error", `[OuSheng] session.created sampling failed: ${String(err)}`)
        }
      }

      // 时机③：session 空闲 —— 收敛检查 + 收尾提醒
      if (event.type === "session.idle") {
        try {
          const cfg = await readCfg(directory)
          let r = await run([OUSHENG_BIN, "converge", "--dir", cfg.workspace], directory)
          let result = r.ok && r.out ? r.out : null
          let reminder: string
          if (result) {
            const status = result.split("\n")[0]?.trim() || "UNKNOWN"
            reminder =
              status === "CONVERGED"
                ? "All work items done. Clean state."
                : status === "BLOCKED"
                  ? "Workspace is BLOCKED — explicit blocked item, dangling dependency, cycle, or missing evidence/ack. Human intervention needed."
                  : "Workspace is IN_PROGRESS — if you changed work items this session, run ousheng work update / progress report / evidence add before ending, then ousheng context me."
          } else {
            const b = await run([BOARD_BIN, "converge", "--dir", BOARD_DIR], directory)
            result = b.ok && b.out ? b.out : null
            const status = result?.match(/status:\s*(\w+)/)?.[1] || "UNKNOWN"
            reminder =
              status === "CONVERGED"
                ? "All contracts verified. No pending writes needed."
                : status === "STUCK"
                  ? "Board is STUCK — dependency cycle or broken dependency. Human intervention needed."
                  : "Board is IN_PROGRESS — if you changed contracts this session, remember to write_board before ending."
          }
          if (result) {
            await log(
              client,
              "info",
              `[OuSheng] Convergence at session end:\n${result}\n${reminder}`,
            )
          } else {
            await log(
              client,
              "error",
              `[OuSheng] converge 失败（workspace=${cfg.workspace}）：${r.err || "未知错误"}。修复：ousheng adapter install --workspace <工作区路径>`,
            )
          }
        } catch (err) {
          await log(client, "error", `[OuSheng] session.idle sampling failed: ${String(err)}`)
        }
      }

      // 时机③（续）：未推提交自动收口（2026-09-21 用户反馈：经常不上绳，本地有 commit 未推）。
      // 只在有未推/落后时才动网（本地 rev-list 检查，零网络）——不无条件每 idle sync
      // （idle 每次响应都触发，会变成每响应一次网络往返）。失败判定只认 sync 末行
      // 机器标记「sync收口: ok」——分叉/网络/push 失败都以退出码 0 + prose 溜过
      // （2026-09-21 review 事故），缺标记 = 未收口，fail-closed 大声报错交人。
      if (event.type === "session.idle") {
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
                await log(
                  client,
                  "info",
                  `[OuSheng] 自动同步（未推 ${ahead} / 落后 ${behind}）完成：\n${r.out}`,
                )
              } else {
                await log(
                  client,
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
    },
  }
}
