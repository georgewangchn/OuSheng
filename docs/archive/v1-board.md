# v1 board CLI 参考（协作绳历史层）

v1 的核心：一张卡一个契约（`cards/<id>.yaml`），五态状态机，`board write` CAS 写入。v0.3 的 WorkItem 已内嵌 Contract 为子对象，`ousheng migrate schema` 可从 cards/ 幂等迁移（原文件不动）。v1 工作流继续可用。

## 命令

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

将卡片转为 `deprecated`，输出依赖它的卡（需迁移）：

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

# 后端：上线（agreed → live）→ 前端集成测试通过，带 evidence 推到 verified
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

./board converge -dir .ousheng   # status: CONVERGED
```

CAS 冲突：两 worker 同时基于 v1 写卡，先写者赢，后写者 `write -expect 1` 失败 → 重新 `read` 再改。

## 工具无关性证明

设计要求"≥2 工具同板协作"，由架构保证：

```
opencode agent ──MCP──┐
                      ├──→ core lib (Go) ──→ git 仓 (.ousheng/)
Claude Code agent ─hook─┤
                      │
human ──CLI────────────┘
pi/codex ──CLI/skill──┘
```

4 种工具共享同一 git 仓，所有写入经过同一 core lib 的校验门。两 agent 用不同工具同板协作 = 两进程对同一 git 仓做 CAS 写 + git push/pull。CAS 保证不覆盖，git 保证一致。

**实测路径**：
1. opencode agent 通过 MCP `write_board` 写卡 → git commit
2. Claude Code agent 启动 → hook 自动 `board read` → 看到该卡
3. 两 agent 通过 `git push/pull` 同步，CAS 防覆盖
