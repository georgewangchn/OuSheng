# OuSheng Adapters

Tool-specific adapters for semi-hard sampling. Each adapter wires OuSheng's `board` CLI into a tool's lifecycle events so that `read_board` fires at session start and `converge` fires at session end — without relying on agent self-discipline.

## Board location convention

The board is a separate git repo at `.ousheng/` in the project root:

```bash
board init .ousheng
```

Override via environment:
- `OUSHENG_BOARD_DIR` — board directory (default `.ousheng`)
- `OUSHENG_BOARD_BIN` — path to `board` binary (default `board`, must be in PATH)

## opencode

1. Build the MCP server: `go build -o ousheng-mcp ./cmd/mcp`
2. Build the CLI: `go build -o board ./cmd/board`
3. Put both binaries in PATH
4. Copy `adapters/opencode/plugin.ts` to `.opencode/plugins/ousheng-sampler.ts`
5. Copy `adapters/opencode/opencode.json` to your project root (merge with existing if any)
6. Copy `adapters/opencode/package.json` to `.opencode/package.json` (merge if exists)
7. Run `cd .opencode && bun install` (opencode auto-installs on next session)

**What it does**:
- `session.created` event → runs `board read --dir .ousheng`, logs result to opencode
- `session.idle` event → runs `board converge --dir .ousheng`, logs convergence status + reminder
- MCP server registered as `ousheng` tool provider — agents can call `read_board`, `write_board`, `converge` as MCP tools

## Claude Code

1. Build the CLI: `go build -o board ./cmd/board`
2. Put `board` in PATH
3. Copy `adapters/claude/session-start.sh` and `adapters/claude/session-stop.sh` to your project
4. Merge `adapters/claude/settings.json` into `.claude/settings.json` (or project-level settings)
5. Make scripts executable: `chmod +x adapters/claude/session-*.sh`

**What it does**:
- `SessionStart` hook → runs `board read --dir .ousheng`, stdout injected into session context
- `Stop` hook → runs `board converge --dir .ousheng`, shows convergence + reminder

## Soft tools (pi / codex / human / no-hook tools)

No adapter needed. Put `docs/board-protocol.md` into your AGENTS.md / system prompt / work agreement. The agent reads it and follows the sampling discipline manually.

## Hardness levels

| Tool | Hardness | Mechanism |
|---|---|---|
| Claude Code | Hard | SessionStart/Stop hooks auto-inject board state |
| opencode | Semi-hard | Plugin event hook + MCP tools |
| Others | Soft | board-protocol.md as skill/instruction |

All levels share the same hard gate: `board write` validates schema, CAS, state machine, breaking-change limiter. The adapter only controls when `read` and `converge` fire — the agent still decides what to `write`.
