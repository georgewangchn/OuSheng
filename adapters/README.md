# OuSheng Adapters

Tool-specific adapters implementing the **查看时机协议（consultation-timing protocol）**：低频、事件驱动、按人的思路决定何时看板 —— 不做每 loop 轮询。

## 查看时机协议（v0.3）

| 时机 | 动作 | 触发 |
|---|---|---|
| ① 每日 / session 启动 | `ousheng sync [--actor ID]` = git pull + 索引刷新 + 项目概览 + 我的上下文 | adapter 自动（session start hook） |
| ② 出现 bug / 问题 | `ousheng context me` / `query_work_items` / `get_system_context` | Agent 遇阻时手动（MCP 工具） |
| ③ 当前任务结束 | `ousheng work update` / `progress report` / `evidence add`，然后 `converge` + `context me` 再看板 | adapter 提醒（session idle/stop）+ Agent 主动 |

原则：**看板频率不高；每次查看都有明确理由。**

## 环境变量

v0.3（优先）：
- `OUSHENG_DIR` — workspace 根目录（含 `.ousheng/`，默认 `.`）
- `OUSHENG_BIN` — `ousheng` 二进制路径（默认 `ousheng`，须在 PATH）
- `OUSHENG_ACTOR` — 当前 agent 的 actor id（设置后 session start 额外注入 `get_my_context`）

v1 兼容（无 `.ousheng/` 工作区时回退）：
- `OUSHENG_BOARD_DIR` — v1 board 目录（默认 `.ousheng`）
- `OUSHENG_BOARD_BIN` — `board` 二进制（默认 `board`）

## opencode

1. 构建：`go build -o ousheng ./cmd/ousheng && go build -o ousheng-mcp ./cmd/mcp`
2. 二进制入 PATH
3. 拷贝 `adapters/opencode/plugin.ts` → `.opencode/plugins/ousheng-sampler.ts`
4. 拷贝/合并 `adapters/opencode/opencode.json`、`package.json`
5. `cd .opencode && bun install`

行为：
- `session.created` → `ousheng sync`（时机①）；v1 回退 `board read`
- `session.idle` → `ousheng converge` + 收尾提醒（时机③）；v1 回退 `board converge`
- MCP 工具：v1 `read_board`/`write_board`/`converge` + v0.3 `get_my_context`/`query_work_items`/`report_progress`/`add_evidence` 等 15 个

## Claude Code

1. 构建：`go build -o ousheng ./cmd/ousheng`
2. `ousheng` 入 PATH
3. 拷贝 `adapters/claude/session-start.sh`、`session-stop.sh`
4. 合并 `adapters/claude/settings.json` → `.claude/settings.json`
5. `chmod +x adapters/claude/session-*.sh`

行为：
- `SessionStart` → `ousheng sync`（时机①；设 `OUSHENG_ACTOR` 注入我的上下文）
- `Stop` → `ousheng converge` + 收尾提醒（时机③）

## Soft tools（无 hook 的工具 / 人）

无需 adapter。把 `docs/context-protocol.md` + `docs/board-protocol.md` 放进 AGENTS.md / 系统提示 / 工作约定，按三时机协议手动执行。

## 硬度分级

| 工具 | 硬度 | 机制 |
|---|---|---|
| Claude Code | 硬 | SessionStart/Stop hooks 自动注入 |
| opencode | 半硬 | Plugin 事件 + MCP 工具 |
| 其他 | 软 | 协议文档作为纪律 |

所有层级共享同一硬门：写入经 Core 校验（schema、CAS、双状态机、C1 evidence / C2 human_ack）。adapter 只控制"何时读"，不控制"写什么"。
