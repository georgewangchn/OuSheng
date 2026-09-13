# AGENTS.md

Repo-specific guidance for OpenCode sessions. Read once at session start.

## What this repo is

OuSheng (㸸绳) — multi-person AI Coding collaboration tool. v0.3 已实现：**轻量工程上下文运行时（Engineering Context Runtime）**。

- v1：Card/Contract 协作绳（`cards/`、`cmd/board`）——继续可用，勿破坏。
- v0.3：工程语义层（`.ousheng/` workspace、`cmd/ousheng`）——Actor/System/Assignment/WorkItem/typed Evidence/Progress；Git+YAML 为 canonical，SQLite 仅派生索引（`internal/index/sqlite`）。

语言：所有文档正文为中文，保留中文内容不翻译。

## Directory map

- `docs/` — design docs (Chinese)
  - `多人AI协作机制_方案基石.md` — design cornerstone (principles, control-theory mapping)
  - `superpowers/specs/2026-08-09-OuSheng-完整设计方案.md` — full product design (data model, interfaces, protocol)
  - `superpowers/plans/2026-08-09-阶段1-core-lib-cli.md` — phase 1 Go implementation plan (TDD, task-by-task)
- `内容/` — WeChat public account articles (Chinese dir name, easy to mistype)
- `assets/logo/`, `assets/cover/` — brand PNGs, versioned `v1`..`v9`. **Never delete older versions** — iteration history is intentional.
- `.workbuddy/memory/` — date-stamped work logs (local, gitignored)

## Gitignore gotchas (high-signal)

- `AGENT.md` (singular) **is gitignored**. `AGENTS.md` (plural, this file) **is NOT** — write here.
- `CLAUDE.md` is gitignored → cannot carry persistent instructions; put them here instead.
- `.claude/`, `.workbuddy/`, `.sisyphus/`, `.omo/` are local tooling dirs, all gitignored. **Never commit** their contents.
- `.env` gitignored; `.env.example` allowed.
- `demo_dev/`, `比赛*/` gitignored.
- `.gitignore` is Python-flavored but the project will be **Go** — don't be confused by the Python sections.

## Git

- Remote: `git@github.com:georgewangchn/OuSheng.git` (SSH)
- Branch: `main` only. No PR/branch policy documented.
- Project was renamed `WTH` → `OuSheng`; GitHub repo also renamed. **Do not use the old name `WTH`** in new content.

## When implementing code (v0.3 layer)

设计依据：`docs/OuSheng_工程本体化改造方案_v0.3.md`（权威）+ `docs/engineering-model.md`。铁律：

- **模块路径** `ousheng`；新外部依赖白名单：`gopkg.in/yaml.v3`、`modernc.org/sqlite`（纯 Go，仅派生索引）、MCP SDK。仍禁 JSON Schema 库、go-git。
- **v0.3 布局**：`internal/{model,state,state/gityaml,index/{memory,sqlite},context,projection,converge,migrate,workspace}`、`cmd/{board,mcp,ousheng}`、`adapters/{git,claude,opencode}`。
- **双状态机**：WorkItem 7 态（backlog/ready/doing/blocked/testing/done/cancelled）与 Contract 5 态分离；`revision`（CAS）≠ `target_version`（产品版本），永不混用。
- **校验手写**（严格解码拒绝未知键、8192 字节门、C1 verified→evidence、C2 breaking→human_ack）。
- **activity 是审计非事实源**；progress 是 reported state（必须带 basis + actor + 时间戳）。
- **SQLite 硬约束 S5**：memory 与 sqlite 查询结果必须深度相等（`internal/index/sqlite/sqlite_test.go`）；改动 Index 接口须同步两实现 + 等价性测试。
- **SQLite 定位**（防"零读者部件"复发，2026-09-13 推演裁决）：产品查询路径默认 memory（零基础设施模式）；sqlite 是派生索引，树内消费者 = S5 等价测试（交叉验证 oracle），树外用途 = 只读 SQL 查询面（`ousheng index rebuild` 后查 `.ousheng/cache/index.db`）。接入查询路径的激活判据与升 authoritative store 相同（v0.3 §1）：真实使用证明 memory 每命令重建成为实际痛点，才接。
- **新字段四路验证**：WorkItem 新增字段必须同时接通四条消费路径——`work show`（detail 层）、`work list`（列表列）、`view kanban`（卡片行）、`context me`（WorkBrief 信道，AI 注入面）。只进存储不进信道 = 没上绳（2026-09-12 复盘教训：priority/due_on 曾漏接 context me，人拍板牛不见）。机械锁：`cmd/ousheng/onboarding_test.go` 的 `TestWorkPlanningFields` 四路断言齐全，新字段照此扩展。
- **嵌套 aggregate 同罪**：Contract、blocks 等嵌套结构与平面字段适用同一四路规则——信号必须接通四路，或明文判决出局并写明理由（2026-09-12 第二课：contract 进了存储但 list/kanban/WorkBrief 三路全盲，接口标准是基石唯一钉死之物，恰好漏得最狠）。机械锁：同文件 `TestContractSignalFourPaths`。WorkBrief 构造已收敛至 `internal/context` 的 `newWorkBrief` 单点，新信号只改一处。
- **验证命令**：`go test ./... && go vet ./...`（每次改动必跑）。
- **查看时机协议**：三时机（session 启动 sync / 遇问题查上下文 / 任务结束更新再看板），禁止引入每 loop 轮询。
- fixtures：`fixtures/lakehouse/` 是五场景数据，`internal/testfix.Setup(t)` 装载；改 fixture 须跑全量测试。
- **提交纪律**：`git add` 用显式路径，禁 `git add -A`（会把 `内容/` 未跟踪文章带进来）。Commit message 正常散文体 + trailer `Co-Authored-By: Claude <noreply@anthropic.com>`。

## When implementing code (phase 1, v1 layer — historical rules still binding)

The plan in `docs/superpowers/plans/2026-08-09-阶段1-core-lib-cli.md` defines the rules; follow them exactly:

- **Language**: Go 1.22+. Module path `ousheng` (imports like `ousheng/internal/card`).
- **v1 已知边界**：v1 card 无 actor 注册表，C2 的 approver 无法验 human 类型——v1 场景下 C2 仅为非空检查；v0.3 已在写入路径（`workspace.validateRefs`）+ converge 审计双层强制（2026-09-13 语义轴推演裁决）。
- **Sole external dependency**: `gopkg.in/yaml.v3`. No JSON Schema lib, no go-git (call system `git` via `os/exec`).
- **Layout**: `cmd/board/main.go` (CLI: `init|read|write|converge|version`), `internal/{card,lifecycle,graph,store,board}/`.
- **Store**: a git repo, one card per file at `cards/<id>.yaml`. Card id regex `^[a-z0-9][a-z0-9-]*$`. Max card size 8192 bytes (projection gate).
- **Status enum is exactly 5**: `proposed | agreed | live | verified | deprecated`. `broken`/`stuck` are derived predicates, not statuses.
- **TDD per step**: write failing test → run to prove failure → minimal impl → run to prove pass → commit.
- **Commit messages**: normal prose (not caveman), trailer `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **Validation is hand-written** (reject unknown top-level keys, enforce size + content gates) — do not pull a schema library.

## Design philosophy (load-bearing for any code work)

The cornerstone doc establishes five principles that judge every decision; the full design runs each component through a four-gate check. Core stance: OuSheng is a **lead rope (牵引绳)**, not the ox. Minimal contact point, give direction only, never do the ox's work. Anything that "helps the ox pull" (contract auto-generators, semantic mergers, Ontology reasoners, central verification pipelines) is explicitly rejected. When in doubt about scope, read the cornerstone's §1 principles and §4 C1/C2/C3 boundaries before adding anything.
