# OuSheng · 㸸绳

> 套在 AI Coding 这头㸸鼻子上的牵引绳 —— 最小接触点，只给方向，绝不替㸸使劲。

多人 AI Coding 协作工具。一块小看板 + 一套接口标准，协调多个"人 + AI"协作，无需共享 Memory、无需中心验证、无需 Ontology。机制与具体 AI Coding 工具（Claude Code / opencode / Codex / Pi / 人）解耦。

---

## 为什么需要它

AI Coding 是一头力气巨大的㸸。力气越大，越需要一根轻的绳——绳越简单、越不抢㸸的活，系统越成功。

OuSheng 不做合约自动生成、不做语义合并、不做 Ontology 推理、不做中心验证。它只做一件事：**让每个 worker 知道当前合约状态，并强制破坏性变更必须人工背书**。

"㸸"（òu）是河南方言里对牛的叫法，"绳"是牵引绳。放㸸、牤㸸、㸸娃儿——农村人都懂。

---

## 架构

```
        ┌──────────────── core lib (Go) ────────────────┐
        │ 存储 · CAS · schema 校验 · 状态机 ·             │
        │ 全局归约 · DAG 检测 · 投影闸门 · 签名验证       │
        └───┬────────┬──────────┬───────────┬────────────┘
        board CLI  MCP server  Claude hook   opencode
       (通用/脚本)  (opencode等) 适配器        plugin 适配器
                    └── 全是薄壳，零重复业务逻辑 ──┘

        store = git 仓（cards/<id>.yaml，version / 历史 / 签名白送）
```

| 件 | 形态 | 职责 |
|---|---|---|
| core lib | `internal/` Go 包 | 全部业务逻辑：schema、CAS、状态机、DAG、收敛 |
| `board` CLI | `cmd/board/` 单二进制 | 通用入口，任何工具/脚本可 shell 调用 |
| MCP server | `cmd/mcp/` 单二进制 | opencode 等原生调用，import 同 core |
| Claude hook | `adapters/claude/` | SessionStart/Stop 自动采样 |
| opencode plugin | `adapters/opencode/` | session 事件触发采样 + MCP 注册 |
| board-protocol | `docs/board-protocol.md` | 跨工具软纪律 markdown |

---

## 安装

前置：Go 1.25+、系统 `git`。

```bash
git clone git@github.com:georgewangchn/OuSheng.git
cd OuSheng
go build -o board ./cmd/board      # CLI
go build -o ousheng-mcp ./cmd/mcp   # MCP server
./board version
# ousheng board 0.1.0
```

---

## 五分钟上手

```bash
# 1. 初始化看板（创建独立 git 仓 + cards/ 目录）
./board init .ousheng

# 2. 写一张卡：提出接口契约
cat > login.yaml <<'EOF'
id: user-auth-api
owner: backend
task: 提供用户登录鉴权接口
status: proposed
version: 0
contract:
  kind: http
  breaking: false
  interface:
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
EOF
./board write -file login.yaml -expect 0 -dir .ousheng
# written user-auth-api version 1

# 3. 读回
./board read -id user-auth-api -dir .ousheng

# 4. 收敛判定
./board converge -dir .ousheng
# status: IN_PROGRESS
```

看板是一个独立 git 仓。每张卡是 `cards/<id>.yaml` 一个文件，每次 `write` 自动产生一次 git commit。多人协作通过 `git push/pull` 同步。

---

## CLI 命令

### `board version`

打印版本号。

### `board init [dir]`

初始化看板（幂等）。默认 `dir=.`。

### `board read` — 读取卡片

```bash
board read [-dir D] [-id ID] [-owner O] [-status S] [-kind K]
```

返回匹配卡片（YAML，`---` 分隔）。任一过滤器留空即不限制。

### `board write` — 写入卡片（CAS）

```bash
board write -file <path.yaml> -expect <version> [-dir D]
```

- 新卡 `-expect 0`，更新传上一次 `read` 返回的 `version`
- 版本不匹配 → `version conflict`
- 环境变量 `OUSHENG_SIGN_COMMITS=1` → git commit 带 GPG 签名

### `board converge` — 收敛判定

```bash
board converge [-dir D] [--watch] [--interval N] [--stuck-after DUR]
```

```yaml
status: CONVERGED | IN_PROGRESS | STUCK
blockers: []
cycle: []
```

| status | 含义 |
|---|---|
| `CONVERGED` | 所有卡 `verified`，无 `proposed`，无 blocker |
| `IN_PROGRESS` | 有卡未到 `verified`，无致命阻塞 |
| `STUCK` | 依赖环（`cycle`）或 broken/dangling 依赖（`blockers`）|

- `--watch` — 轮询模式，状态变化时打印
- `--stuck-after 24h` — 时基 stuck 检测：卡在非 verified/deprecated 状态超过阈值 → STUCK
- `--interval 5` — watch 轮询间隔（秒）

### `board deprecate` — 废弃卡片 + 迁移清单

```bash
board deprecate -expect <version> [-dir D] <id>
```

将卡片转为 `deprecated`，输出依赖它的卡（需迁移）。

```bash
$ board deprecate -expect 3 -dir .ousheng auth-api
deprecated auth-api version 4
cards needing migration:
  - frontend-app
```

### `board context` — 拉取卡片上下文（F1 按需通道）

```bash
board context [-dir D] <id>
```

返回卡片内容 + 最近 10 条 git commit 历史。原始透传——不加摘要、不合并——消费方 agent 自己读，绝不落板。

### `board verify` — 签名验证（F2 信任层）

```bash
board verify [-dir D]
```

检查看板所有 commit 的 GPG 签名。全签 → `all commits signed`，有未签 → 逐条列出。

---

## MCP Server

`ousheng-mcp` 是 stdio MCP server，暴露 3 个工具给 AI agent：

| 工具 | 输入 | 输出 |
|---|---|---|
| `read_board` | path + 可选过滤器（id/owner/status/kind）| 卡片摘要列表 |
| `write_board` | path + card YAML + expected_version | 新版本号 + 状态 |
| `converge` | path | 收敛状态 + blockers + cycle |

opencode 注册（`opencode.json`）：
```json
{
  "mcp": {
    "ousheng": { "type": "local", "command": ["ousheng-mcp"], "enabled": true }
  }
}
```

---

## Card 模型

```yaml
id: user-auth-api              # ^[a-z0-9][a-z0-9-]*$
owner: backend                 # 负责方
task: 提供用户登录鉴权接口       # 一句话任务描述
status: proposed               # proposed|agreed|live|verified|deprecated
version: 0                     # write 时自动 +1，初始传 0
depends_on: [user-db]          # 依赖的卡 id（可选）
contract:
  kind: http                   # http|cli|lib|event
  breaking: false              # 是否破坏性变更
  interface:                   # 自由结构，由 kind 决定语义
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
evidence:                      # status=verified 时必填
  probe: "test/auth_integration: POST /login 200 + token 可用"
  passed_at_commit: abc123
  by: frontend
human_ack:                     # contract.breaking=true 时必填
  approver: alice
  at_version: 1
```

### 四条硬约束

| 约束 | 触发条件 | 要求 |
|---|---|---|
| 体积门 | 所有卡 | YAML ≤ 8192 字节 |
| 内容门 | 所有卡 | `task` 禁含代码块或 traceback |
| C1 行为传感器 | `status=verified` | 完整 `evidence`（probe + passed_at_commit + by 非空）|
| C2 破坏性限制器 | `breaking=true` | `human_ack.approver` 非空 |

---

## 状态机

```
proposed ──→ agreed ──→ live ──→ verified
   │           │          │          │
   └───────────┴──────────┴──────────┴──→ deprecated
```

- 同状态原地踏步允许（幂等重写）
- `verified → live` 允许（回滚）
- 任何状态 → `deprecated` 允许
- `verified` 是收敛终点

---

## 采样纪律

不同工具的采样硬度：

| 工具 | 硬度 | 机制 |
|---|---|---|
| Claude Code | 硬 | SessionStart/Stop hook 自动注入 board 状态 |
| opencode | 半硬 | plugin 事件触发 + MCP 工具 |
| pi / codex / 人 | 软 | `docs/board-protocol.md` 作为 skill |

详见 [`docs/board-protocol.md`](docs/board-protocol.md) — 跨工具可移植的采样律 + 投影律 + 冲突处理协议。

适配器安装见 [`adapters/README.md`](adapters/README.md)。

---

## 协作示例

```bash
# 后端：提出契约
./board write -file auth.yaml -expect 0 -dir .ousheng
git -C .ousheng push

# 前端：pull 后约定（proposed → agreed）
git -C .ousheng pull
./board read -id user-auth-api -dir .ousheng   # version 1
# 编辑 auth.yaml: status: agreed, version: 1
./board write -file auth.yaml -expect 1 -dir .ousheng
git -C .ousheng push

# 后端：上线（agreed → live）
# ... version 2 → 3 ...

# 前端：集成测试通过，带 evidence 推到 verified
cat > auth.yaml <<'EOF'
id: user-auth-api
owner: backend
task: 提供用户登录鉴权接口
status: verified
version: 3
evidence:
  probe: "test/auth_integration: POST /login 200 + token 可用"
  passed_at_commit: abc123
  by: frontend
contract:
  kind: http
  breaking: false
  interface:
    - method: POST
      path: /login
      behavior: "有效凭证返回 token；无效返回 401"
EOF
./board write -file auth.yaml -expect 3 -dir .ousheng

# 收敛
./board converge -dir .ousheng
# status: CONVERGED
```

CAS 冲突：两 worker 同时基于 v1 写卡，先写者赢，后写者 `write -expect 1` 失败 → 重新 `read` 再改。

---

## 设计文档

| 文档 | 内容 |
|---|---|
| [设计基石](docs/多人AI协作机制_方案基石.md) | 五条原则、控制论映射、C1/C2/C3 边界 |
| [完整设计方案](docs/superpowers/specs/2026-08-09-OuSheng-完整设计方案.md) | 数据模型、接口、协议、前瞻层 |
| [阶段1实现计划](docs/superpowers/plans/2026-08-09-阶段1-core-lib-cli.md) | Go core lib + CLI，TDD 任务分解 |
| [采样协议](docs/board-protocol.md) | 跨工具软纪律：采样律 + 投影律 + 冲突处理 |
| [适配器指南](adapters/README.md) | opencode plugin + Claude Code hook 安装 |
| [参考节点示例](docs/reference-node.md) | 守规矩 agent 的完整工作流（F3 残余价值）|

---

## 工具无关性证明

设计要求"≥2 工具同板协作"。OuSheng 的工具无关性由架构保证：

```
opencode agent ──MCP──┐
                      ├──→ core lib (Go) ──→ git 仓 (.ousheng/)
Claude Code agent ─hook─┤
                      │
human ──CLI────────────┘
pi/codex ──CLI/skill──┘
```

**证明**：4 种工具（opencode MCP、Claude Code hook、CLI 脚本、人）共享同一 git 仓。所有写入经过同一 core lib 的 10 道校验门。两 agent 用不同工具同板协作 = 两进程对同一 git 仓做 CAS 写 + git push/pull。CAS 保证不覆盖，git 保证一致。

**实测路径**：
1. opencode agent 通过 MCP `write_board` 写卡 → git commit
2. Claude Code agent 启动 → hook 自动 `board read` → 看到该卡
3. 两 agent 通过 `git push/pull` 同步，CAS 防覆盖

不需要共享 Memory、不需要中心验证、不需要 Ontology。一根绳，一块板。

---

## GitHub

[github.com/georgewangchn/OuSheng](https://github.com/georgewangchn/OuSheng)
