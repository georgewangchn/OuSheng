import type { Plugin } from "@opencode-ai/plugin"

// OuSheng v0.3 查看时机协议（低频事件驱动，非每 loop 轮询）：
//   时机① session 启动 → ousheng sync（pull + 索引 + 概览 + 我的上下文）
//   时机② 遇到 bug/问题 → 手动 ousheng context me / query_work_items（MCP 工具）
//   时机③ session 空闲/结束 → ousheng converge 收尾提醒
const WS_DIR = process.env.OUSHENG_DIR || "."
const OUSHENG_BIN = process.env.OUSHENG_BIN || "ousheng"
const OUSHENG_ACTOR = process.env.OUSHENG_ACTOR || ""

const BOARD_DIR = process.env.OUSHENG_BOARD_DIR || ".ousheng"
const BOARD_BIN = process.env.OUSHENG_BOARD_BIN || "board"

async function run(
  cmd: string[],
  cwd: string,
): Promise<string | null> {
  try {
    const { exitCode, stdout } = await Bun.spawn({
      cmd,
      cwd,
      stdout: "pipe",
      stderr: "pipe",
    }).capture()
    if (exitCode !== 0) {
      return null
    }
    return stdout.toString().trim() || null
  } catch {
    return null
  }
}

async function ousheng(args: string[], directory: string): Promise<string | null> {
  return run([OUSHENG_BIN, ...args, "--dir", WS_DIR], directory)
}

async function board(args: string[], directory: string): Promise<string | null> {
  return run([BOARD_BIN, ...args, "--dir", BOARD_DIR], directory)
}

export const OuShengSampler: Plugin = async ({ directory, client }) => {
  return {
    event: async ({ event }) => {
      // 时机①：session 启动 —— 拉取 + 刷新 + 我的上下文
      if (event.type === "session.created") {
        try {
          const args = OUSHENG_ACTOR
            ? ["sync", "--actor", OUSHENG_ACTOR]
            : ["sync"]
          // v0.3 优先；找不到 workspace 时 sync 失败返回 null → v1 回退
          let result = await ousheng(args, directory)
          if (!result) {
            result = await board(["read"], directory)
          }
          if (result) {
            await client.app.log({
              body: {
                service: "ousheng-sampler",
                level: "info",
                message: `[OuSheng] Engineering context at session start:\n${result}`,
              },
            })
          }
        } catch (err) {
          await client.app
            .log({
              body: {
                service: "ousheng-sampler",
                level: "error",
                message: `[OuSheng] session.created sampling failed: ${String(err)}`,
              },
            })
            .catch(() => {})
        }
      }

      // 时机③：session 空闲 —— 收敛检查 + 收尾提醒
      if (event.type === "session.idle") {
        try {
          let result = await ousheng(["converge"], directory)
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
            result = await board(["converge"], directory)
            const status = result?.match(/status:\s*(\w+)/)?.[1] || "UNKNOWN"
            reminder =
              status === "CONVERGED"
                ? "All contracts verified. No pending writes needed."
                : status === "STUCK"
                  ? "Board is STUCK — dependency cycle or broken dependency. Human intervention needed."
                  : "Board is IN_PROGRESS — if you changed contracts this session, remember to write_board before ending."
          }
          if (result) {
            await client.app.log({
              body: {
                service: "ousheng-sampler",
                level: "info",
                message: `[OuSheng] Convergence at session end:\n${result}\n${reminder}`,
              },
            })
          }
        } catch (err) {
          await client.app
            .log({
              body: {
                service: "ousheng-sampler",
                level: "error",
                message: `[OuSheng] session.idle sampling failed: ${String(err)}`,
              },
            })
            .catch(() => {})
        }
      }
    },
  }
}
