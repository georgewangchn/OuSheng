import type { Plugin } from "@opencode-ai/plugin"

const BOARD_DIR = process.env.OUSHENG_BOARD_DIR || ".ousheng"
const BOARD_BIN = process.env.OUSHENG_BOARD_BIN || "board"

async function runBoard(
  args: string[],
  boardPath: string,
): Promise<string | null> {
  try {
    const { exitCode, stdout, stderr } = await Bun.spawn({
      cmd: [BOARD_BIN, ...args, "--dir", boardPath],
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

export const OuShengSampler: Plugin = async ({ directory, client }) => {
  const boardPath = `${directory}/${BOARD_DIR}`

  return {
    event: async ({ event }) => {
      if (event.type === "session.created") {
        try {
          const result = await runBoard(["read"], boardPath)
          if (result) {
            await client.app.log({
              body: {
                service: "ousheng-sampler",
                level: "info",
                message: `[OuSheng] Board state at session start:\n${result}`,
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

      if (event.type === "session.idle") {
        try {
          const result = await runBoard(["converge"], boardPath)
          if (result) {
            const status = result.match(/status:\s*(\w+)/)?.[1] || "UNKNOWN"
            const reminder =
              status === "CONVERGED"
                ? "All contracts verified. No pending writes needed."
                : status === "STUCK"
                  ? "Board is STUCK — dependency cycle or broken dependency. Human intervention needed."
                  : "Board is IN_PROGRESS — if you changed contracts this session, remember to write_board before ending."

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
