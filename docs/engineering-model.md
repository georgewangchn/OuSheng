# OuSheng 工程语义模型（Engineering Model）

> 版本：v0.3（对应 `docs/OuSheng_工程本体化改造方案_v0.3.md` Phase 0 产物）
> 状态：已实现（`internal/model`，fixtures 见 `fixtures/lakehouse/`）

## 1. 定位

OuSheng 是**轻量工程上下文运行时（Engineering Context Runtime）**：
让 1~3 个人与多个 AI Agent 共享同一份最小、结构化、可同步的工程上下文。

目标函数：

```
协作价值 = 降低协调错误 − 上下文成本 − 状态维护成本 − 同步复杂度 − 工具介入程度
```

一切模型决策服从该函数：**最小协调信号 + 可验证状态 + 不替 AI 做事。**

## 2. 实体一览

一级概念（v0.3 §6 收缩后）：

| 实体 | 说明 | 存储 |
|------|------|------|
| System | 工程上下文锚点；≠ Repository；层级用 `parent` | `.ousheng/systems.yaml` |
| Actor | 统一 Human / Agent（`type: human\|agent`）；agent 必须绑 `responsible_human` | `.ousheng/actors/*.yaml` |
| Assignment | 承重关系 Actor × Role × System（executor / accountable） | `.ousheng/assignments.yaml` |
| WorkItem | canonical 工程工作概念（schema_version=2）；Card 是其 Board 兼容投影 | `.ousheng/work/*.yaml` |
| Contract | WorkItem 的子对象；5 态生命周期独立于 WorkItem 状态 | 内嵌于 WorkItem |
| Evidence | typed append-only 证据列表 | 内嵌于 WorkItem |

辅助概念：Role（project-scoped registry）、TargetVersion（字符串语义，非实体）、
ProgressReport（reported state，非 fact）、Activity（audit record，非事实源）、HumanAck。

Project 第一版只是 `.ousheng/project.yaml` workspace 配置，不是实体。
Component 不引入（用 `System.parent`）。Person 并入 Actor。

## 3. 语义判词（Phase 0 必答，v0.3 §44）

| 问题 | 答案 |
|------|------|
| Event 是否权威？ | **否**。权威字段始终是 WorkItem 本身；Activity 只做审计/时间线。Event Sourcing 需单独 ADR。 |
| Progress 是否事实？ | **否**。Progress 是 Reported State：`{value, actor, reported_at, basis}`，展示时必须标 `reported`。 |
| Version 是什么？ | `target_version`（WorkItem 面向的产品/系统版本字符串）；暂不冻结 Release/SystemVersion 实体。 |
| Project 是否实体？ | 否，第一版是 workspace config。 |
| Component 是否实体？ | 否，`System.parent` 表达层级。 |
| Person 是否独立于 Actor？ | 否。 |
| revision 与 target_version？ | `revision` = CAS 并发控制计数；`target_version` = 产品版本。**任何迁移不得做 `card.version → target_version` 映射。** |
| 谁执行 / 谁负责？ | `assignee`（可 agent）执行；`accountable_human`（必须 human）负责。 |

## 4. WorkItem

### 4.1 类型（封闭集合）

```
requirement | feature | bug | task | test | deployment | release
```

`progress` 不是类型——进度是状态报告，不是工作本身。

### 4.2 执行生命周期（7 态，与 Contract 5 态分离）

```
backlog → ready → doing → testing → done
           │       │  ↖      │
           │       ↓   ↖     ↓
           │    blocked → doing
           ↓
        cancelled（backlog 可 reopen；done 只可 reopen 回 doing）
```

合法迁移表见 `internal/model/lifecycle.go`。要点：

- 不许跳级（backlog→doing 拒绝）；
- `blocked → doing` 是唯一解除路径；
- 终态 reopen：`done → doing`、`cancelled → backlog`。

### 4.3 最小字段（示例）

```yaml
schema_version: 2
id: BUG-017
type: bug
title: CDC checkpoint 恢复失败
system: datax-backend
target_version: v2.0
assignee: backend-agent
acting_role: backend
accountable_human: zhangsan
status: doing
revision: 7
depends_on: [K8S-003]
progress:
  value: 0.7
  actor: backend-agent
  reported_at: 2026-09-07T10:30:00+08:00
  basis: implementation-checklist
evidence:
  - type: git_commit
    source: git
    locator: abc123
```

### 4.4 校验规则（手写，无 schema 库）

- `schema_version` 必须 = 2；未知顶层键拒绝（严格解码）；
- id：`^[A-Z0-9][A-Z0-9-]*$`；尺寸门 8192 字节；禁 ``` 与 Traceback 内容；
- C1：`contract.status=verified` 必须有 evidence（v1 规则保留）；
- C2：`contract.breaking=true` 必须有 human_ack（approver 非空）；
- progress：value ∈ [0,1]、basis 封闭集合、reported_at 必须 RFC3339；
- evidence.type 封闭集合（见 §6）；
- 引用完整性（system/actor/role 存在性）由 state 层在写入时校验；
- 自依赖拒绝；悬空 depends_on 允许写入、由 converge/projection 标记（弱介入）。

## 5. Contract（WorkItem 子对象）

```
proposed → agreed → live → verified
                       ↑      │
                       └──────┘（回归）
任意态 → deprecated
```

kind：`http | cli | lib | event`。与 v1 Card 共用语义，但状态字段独立于 WorkItem.status。

## 6. Evidence（typed append-only list）

类型封闭集合：

```
git_commit | pull_request | test_result | ci_run | deployment | log | manual_check | document
```

State 回答"现在是什么状态"；Evidence 回答"为什么相信这个状态"。二者语义分离，
不自动推导存储介质分离（v0.3 §16）。

## 7. Activity（audit，非事实源）

`.ousheng/activity/YYYY-MM.jsonl`，字段 `{ts, actor, action, work_item, detail}`。
用途：审计、时间线、调试、上下文补充。当 `WorkItem.status=doing` 与
Activity 最新记录矛盾时，权威字段是 `WorkItem.status`。

## 8. 五场景 fixture

`fixtures/lakehouse/` 一份 canonical 数据驱动五个验收场景（§45）：

1. **Requirement**：REQ-UI-001（datax-ui，v2.0）
2. **Bug**：BUG-017（test-agent 发现，backend-agent 执行，张三负责）
3. **Progress**：FEAT-CDC-001 progress 0.7（reported，basis=implementation-checklist）
4. **Agent Context**：backend-agent manifest → get_my_context
5. **Board Projection**：全量 WorkItem → Kanban/Actor/System/Version 投影

**反事实验收声明（工程决策记录）**：上述五场景全部在 Git + YAML + Go 内存索引上跑通，
未引入 SQLite authoritative store——证明语义模型成立，且 SQLite 不是这些场景的必要条件。
SQLite 仅为可删除、可重建的派生索引（`.ousheng/cache/index.db`）。

## 9. 存储与同步

- Canonical state = Git + YAML（`.ousheng/`）；版本历史 / Diff / 跨机同步 / 冲突暴露 / 审阅 / 签名全走 Git。
- CAS 在 Core 层：`write expect_revision=N`，不匹配即 `CONFLICT`。Git commit 不是 CAS 替代。
- 多机：`git pull` + `ousheng index rebuild` 即进入同一状态；SQLite 不参与 merge。
- v1 Card（`cards/*.yaml`）继续可用；`ousheng migrate schema` 做 v1→v2 迁移，owner 不可解析时留
  `legacy_owner` + `migration_status=needs_resolution`，由 `migrate resolve-owner` 人工解决。
