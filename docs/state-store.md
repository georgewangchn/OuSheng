# state-store — 状态存储协议（v0.3）

> Git + YAML 是 canonical state；SQLite 只是可删除、可重建的派生索引。
> 对应方案：`docs/OuSheng_工程本体化改造方案_v0.3.md` §27–§34。

## 1. 两层存储

```
Canonical State                    Derived Index
Git + YAML                         SQLite (.ousheng/cache/index.db)
  │                                  │
  ├─ 版本历史 / Diff / 审阅           ├─ 本地查询加速
  ├─ 跨机同步（push/pull）           ├─ 关系聚合
  ├─ 冲突暴露（merge conflict）      └─ Projection / Context 加速
  ├─ 签名 / 审计
  └─ 离线编辑
```

**硬约束（S5）**：

```bash
rm .ousheng/cache/index.db
ousheng index rebuild
```

前后查询结果必须一致（有等价性测试证明）。SQLite 不是运行前提——
小项目用内存索引即可全功能运行（零基础设施模式）。

## 2. 目录布局

```
.ousheng/
├── project.yaml        # workspace 顶层（第一版非实体）
├── systems.yaml        # System 注册表（层级 parent）
├── roles.yaml          # Role 注册表
├── assignments.yaml    # Actor × Role × System
├── actors/             # Actor（human/agent）+ Agent Manifest
│   ├── zhangsan.yaml
│   └── backend-agent.yaml
├── work/               # WorkItem（schema_version=2）
│   └── BUG-017.yaml
├── activity/           # 审计记录（jsonl，按月分桶）——非事实源
│   └── 2026-09.jsonl
└── cache/              # gitignored
    └── index.db
```

v1 `cards/` 与 v2 `.ousheng/` 可共存于同一 git 仓（迁移期兼容）。

## 3. CAS

所有可并发修改对象使用 `revision`：

```
read revision=7 → modify → write expect_revision=7
                       ↓ 当前已是 8 → CONFLICT
```

- CAS 在 Core 层（`internal/state`），Git commit 不是 CAS 替代；
- Git 只负责持久化 / 历史 / 同步；
- `revision`（CAS 计数）≠ `target_version`（产品版本），永不混用。

## 4. 写路径（gityaml）

一次 WorkItem 写入 = 一把进程锁内：读当前 → CAS 比对 → 写 YAML →
追加 activity → **单次 git commit**（状态变更与审计原子同commit）。

锁：`.ousheng.lock`（PID + 陈锁检测，与 v1 语义一致）。

## 5. 多机协作（S7）

```
Machine A: ousheng work update ... → git commit → push
Machine B: git pull → ousheng sync（= pull + index rebuild + 概览）
```

- 无共享 SQLite、无中心服务器、无 NFS/Redis/Kafka；
- SQLite 不参与 git merge（gitignored）；
- 跨机 CAS：B pull 后基于旧快照写 → `ErrConflict`（有 e2e 测试）。

## 6. 迁移（v1 → v2）

```bash
ousheng migrate schema          # cards/ → .ousheng/work/（幂等，cards 不动）
ousheng migrate resolve-owner <id> --assignee A --role R [--accountable H]
```

映射铁律见 `docs/engineering-model.md` §3：`card.version → revision`，
**绝不** `→ target_version`。owner 无法解析 → `legacy_owner` +
`migration_status=needs_resolution`，人工一次性解决。

## 7. 存储可替换

Core 依赖 `state.Repository` 接口（`internal/state/state.go`），不绑死 Git。
未来若真实瓶颈出现（跨实体原子事务高频 / 关系完整性故障 / 写频率失控 /
多机同步已有设计），可新增 SQLiteRepository / RemoteRepository——
语义模型与业务规则不用重写。升级前必须回答 §52-D 的七个问题。
