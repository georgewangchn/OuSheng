# OuSheng 工程本体化改造方案

> 版本：v0.3  
> 日期：2026-09-07  
> 对象：`georgewangchn/OuSheng`  
> 核心目标：在不牺牲 OuSheng “轻、弱介入、工具无关、Git 可同步、CAS、防破坏性变更”这些核心特性的前提下，将其从“Contract Card 协作绳”演进为**面向 1～3 人 + 多 AI Agent 的轻量 Engineering Context Runtime**。  
> 核心策略：**先升级工程语义，再升级查询能力；Git 继续作为 canonical state，SQLite 第一阶段只作为可重建派生索引，不作为唯一事实源。**

---

# 1. 结论先行

v0.2 对 OuSheng 下一阶段问题的诊断基本成立：

```text
Card
```

已经不仅承担“协作契约”，还被迫承担：

```text
谁
以什么角色
负责哪个系统
哪个版本
正在做什么
当前进展
有哪些阻塞
有哪些证据
```

因此 OuSheng 确实需要从：

```text
Card / Contract Coordination
```

演进到：

```text
Engineering Context Coordination
```

但 v0.2 将：

```text
语义模型不够
```

进一步推导为：

```text
SQLite 必须成为 authoritative state
Git 退出状态同步
```

这个结论并不成立。

v0.3 的核心判断是：

> **工程语义升级是现在就应该做的；存储主从关系不应该现在反转。**

推荐架构：

```text
                    Engineering State Model
                              │
              ┌───────────────┴───────────────┐
              │                               │
        Canonical State                  Derived Index
          Git + YAML                       SQLite
              │                               │
              └───────────────┬───────────────┘
                              ↓
                         Core Repository
                              │
              ┌───────────────┼───────────────┐
              ↓               ↓               ↓
            Board            MCP           Context
                                              │
                                              ↓
                                            Agent

Code Git / CI / Test / K8s
              ↓
           Evidence
              ↓
         WorkItem links
```

也就是说：

```text
Git
```

继续负责：

```text
工程状态的 canonical representation
版本历史
Diff
跨机同步
冲突暴露
人工审阅
签名 / 审计能力
```

而：

```text
SQLite
```

第一阶段负责：

```text
本地查询索引
关系聚合
Projection 加速
Context 查询加速
```

并且必须满足：

```bash
rm .ousheng/cache/index.db
ousheng index rebuild
```

即可从 Git canonical state 完整恢复。

只有当真实使用证明 Git + YAML 已经在**原子事务、关系完整性或高频关系更新**上成为实际瓶颈，并且多机同步问题已经有明确方案时，才考虑把 SQLite 提升为 authoritative store。

---

# 2. 第一性原理：OuSheng 到底在解决什么

OuSheng 不是项目管理工具。

它也不应该以“表达完整的软件工程世界”为目标。

它真正解决的是：

> **在多个互不共享 Memory 的 Human / Agent 之间，用尽可能小、可验证、可同步的协调信号，让每个 worker 的下一步动作与别人的当前状态保持一致。**

可以把目标函数抽象成：

```text
协作价值
=
降低协调错误
-
上下文成本
-
状态维护成本
-
同步复杂度
-
工具介入程度
```

因此当前 OuSheng 的几个重要设计不是偶然的：

```text
小 Card
Contract
CAS
Git
human_ack
evidence
Board
Core Lib + Thin Adapter
```

它们都在服务同一个目标：

> **最小协调信号 + 可验证状态 + 不替 AI 做事。**

v0.3 的任何设计都必须继续满足这一目标函数。

---

# 3. v0.3 的产品定位

v0.2 使用：

> Engineering Ontology Runtime

作为新定位。

这个方向在技术上可以理解，但在产品层面容易制造错误预期：

```text
Ontology
→ RDF
→ OWL
→ SPARQL
→ Reasoning
→ Knowledge Graph
```

这些都不是 OuSheng 当前要解决的问题。

因此 v0.3 建议对外优先使用：

> **Engineering Context Runtime**

中文：

> **轻量工程上下文运行时**

或者：

> **Engineering State Runtime**

内部仍然可以使用：

```text
ontology/model
```

描述工程实体和关系，但不要让“Ontology”成为产品引力中心。

新的定位可以定义为：

> **OuSheng 是一个让 1～3 个人与多个 AI Agent 共享同一份最小、结构化、可同步工程上下文的运行时。**

它不是：

```text
Jira
Linear
Trello
Gitea Issues
```

也不是：

```text
Agent Memory
Knowledge Graph
Ontology Reasoner
Workflow Engine
```

而是：

```text
Engineering State
+
Contract
+
Evidence
+
Projection
+
Agent Context
```

---

# 4. 必须保留的 OuSheng 核心资产

本次演进不是推倒重来。

以下能力必须作为兼容性约束保留。

## 4.1 Core Lib + Thin Adapter

继续保持：

```text
                  Core
                   │
       ┌───────────┼───────────┐
       ↓           ↓           ↓
      CLI         MCP        Adapter
```

所有业务规则：

```text
模型
约束
CAS
状态迁移
Evidence 规则
Context 构建
Projection
```

都放在 Core。

MCP、CLI、Hook、UI 不承载业务逻辑。

---

## 4.2 CAS

CAS 仍然是协作核心机制。

但必须明确区分两个完全不同的概念：

```text
revision
```

与：

```text
target_version
```

旧 Card 中用于并发控制的 `version`，在 v0.3 中统一重命名语义为：

```text
revision
```

示例：

```yaml
revision: 7
target_version: v2.0
```

含义：

```text
revision
= 当前工程对象的 CAS revision

target_version
= 当前 WorkItem 面向的产品 / 系统版本
```

任何迁移都不得再做：

```text
card.version
→ 产品版本
```

这种语义映射。

---

## 4.3 Contract

Contract 不删除。

它从：

```text
Card 的核心
```

变成：

```text
WorkItem 的重要子对象
```

例如：

```text
WorkItem
  └── Contract
       ├── API
       ├── CLI
       ├── Event
       └── Library
```

Contract 生命周期继续使用：

```text
proposed
agreed
live
verified
deprecated
```

---

## 4.4 C1 / C2 / Human Ack

继续保留：

```text
verified 必须有 evidence
breaking=true 必须 human_ack
```

并逐步扩展到关键工程动作：

```text
Breaking API
Architecture Change
Release
Production Deploy
Critical Data Migration
```

但 v0.3 不把所有工程动作都纳入强审批。

继续坚持：

> **关键动作才需要 Human Ack。**

---

## 4.5 小上下文原则

OuSheng 仍然不应该把整个工程世界塞进 Agent Context。

新的模型不是为了：

```text
给 Agent 更多信息
```

而是为了：

```text
更精确地只给 Agent 当前需要的信息
```

因此新版核心能力之一必须是：

> **Progressive Context Disclosure**

---

# 5. 当前 Card 模型真正缺什么

当前 Card 可以很好回答：

```text
谁负责什么？
当前状态是什么？
依赖什么？
是否 breaking？
有什么 evidence？
谁 human_ack？
```

但很难稳定回答：

```text
这个 owner 是 Human 还是 Agent？
Agent 的最终责任人是谁？
它以什么角色行动？
它负责哪个 System？
它正在处理哪个产品版本？
它还有哪些相关 WorkItem？
一个 System 当前有哪些 Blocker？
同一个人为什么同时出现在两个系统中？
```

因此需要扩展的是：

> **工程身份、作用域和关系语义。**

不是为了做 Jira，而是为了更精确地回答：

```text
Who am I?
What role am I acting as?
Which system am I responsible for?
Which target version am I working on?
What work is active?
What dependencies or blockers matter now?
What evidence is relevant?
```

---

# 6. v0.3 最小工程模型

v0.2 一次性引入了 Project、System、Component、Person、Actor、Role、Assignment、Version、WorkItem、Event、Evidence 等大量一级概念。

v0.3 第一版收缩为：

```text
System
Actor
Assignment
WorkItem
Contract
Evidence
```

辅助概念：

```text
Role
TargetVersion
ProgressReport
Activity
HumanAck
```

Project 第一版作为 workspace/config 顶层存在，不必建立重量级实体。

Component 第一版不单独引入，通过：

```text
System.parent
```

支持层级。

Person 合入 Actor。

Version 第一版不冻结为独立复杂实体，只作为明确语义的：

```text
target_version
```

存在。

Event 第一版不作为事实源，只作为 Activity / Audit Record。

---

# 7. Actor：统一 Human 与 Agent

第一版统一为：

```text
Actor
```

类型：

```text
human
agent
```

示例：

```yaml
id: zhangsan
type: human
display_name: 张三
```

Agent：

```yaml
id: backend-agent
type: agent
display_name: Backend Agent
responsible_human: zhangsan
```

这样可以同时表达：

```text
backend-agent 执行
        ↓
zhangsan 最终负责
```

避免额外维护：

```text
Person
Actor
Human
Agent
```

四套身份概念。

---

# 8. Role：第一版保持轻量

Role 有价值，但第一版不需要复杂实体生命周期。

Role 可以是 project-scoped registry：

```yaml
roles:
  - id: backend
    name: Java后端开发

  - id: tester
    name: 测试

  - id: pm
    name: 项目经理
```

未来如果真实需求出现：

```text
Role inheritance
Permission
Capability
Organization hierarchy
```

再升级。

第一版 Role 只回答：

> **Actor 以什么职责视角参与当前 System。**

---

# 9. System：工程上下文的核心锚点

System 是 v0.3 最重要的一级实体之一。

例如：

```text
智能湖仓
│
├── datax-mcp
├── aidp-cp
├── datax-backend
├── datax-ui
├── lakehouse-data
└── lakehouse-k8s
```

注意：

> **System ≠ Repository**

一个 System 可以映射：

```text
一个 Repo
多个 Repo
一个 Service
一个 Deployment
一个 Data Domain
```

示例：

```yaml
id: datax-backend
name: DataX Backend

repositories:
  - github.com/example/datax-backend
```

第一版如需 Component，可写：

```yaml
parent: data-platform
```

而不是新建独立 Component 实体。

---

# 10. Assignment：Actor × Role × System

Assignment 继续保留，因为它是真正承重的关系。

模型：

```text
Actor × Role × System
```

示例：

```yaml
actor: backend-agent
role: backend
system: datax-backend
responsibility: executor
active: true
```

另一个：

```yaml
actor: zhangsan
role: backend
system: datax-backend
responsibility: accountable
active: true
```

这样可以区分：

```text
谁执行
谁负责
以什么角色
在哪个系统
```

而不是继续把所有含义压进：

```yaml
owner: backend
```

---

# 11. WorkItem：Card 的语义升级

v0.3 引入：

```text
WorkItem
```

但不删除 Card。

关系定义为：

```text
WorkItem = canonical engineering work concept
Card     = WorkItem 的 Board Projection / compatibility view
```

第一版 WorkItem 类型：

```text
requirement
feature
bug
task
test
deployment
release
```

不建议把：

```text
progress
```

定义成 WorkItem 类型。

Progress 是状态报告，不是工作本身。

---

# 12. WorkItem 最小字段

推荐：

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

depends_on:
  - K8S-003

related_to:
  - FEAT-CDC-001

contract:
  ...

progress:
  value: 0.7
  actor: backend-agent
  reported_at: 2026-09-07T10:30:00+08:00
  basis: implementation-checklist

evidence:
  - type: git_commit
    locator: abc123

human_ack:
  ...
```

---

# 13. WorkItem 状态与 Contract 状态必须分开

Contract：

```text
proposed
agreed
live
verified
deprecated
```

WorkItem：

```text
backlog
ready
doing
blocked
testing
done
cancelled
```

不要再让一个状态字段同时承担：

```text
协作契约生命周期
+
工程执行生命周期
```

---

# 14. Project：第一版作为 Workspace

对于目标规模：

```text
1～3 Human
+
N Agents
```

第一版可以假设：

```text
一个 OuSheng workspace 对应一个 Project
```

Project 信息放在：

```text
.ousheng/project.yaml
```

例如：

```yaml
schema_version: 1

project:
  id: smart-lakehouse
  name: 智能湖仓
```

未来如果真实出现：

```text
一个 workspace 管理多个独立 Project
```

再把 Project 提升为正式实体。

---

# 15. Version：先解决语义，不急着实体化

v0.2 的 Version 同时暗含：

```text
System Version
Project Release
```

这两个概念并不相同。

例如：

```text
DataX Backend v2.0
```

是 System Version。

而：

```text
智能湖仓 v2.0
```

可能是 Project Release。

v0.3 第一版不冻结复杂 Version 模型。

WorkItem 只保留：

```yaml
target_version: v2.0
```

如需要项目级 Release，可以额外增加：

```yaml
target_release: lakehouse-2026.09
```

等真实使用明确二者关系后，再决定是否引入：

```text
Release
SystemVersion
```

两个正式实体。

---

# 16. State 与 Evidence：逻辑分离，而不是强制物理分离

v0.2 正确提出：

> State ≠ Evidence

v0.3 保留这一原则，但明确：

> **语义分类不自动推导成存储介质分类。**

可以同时存在：

```text
Git State Repo
  └── work/*.yaml
```

和：

```text
Code Git / CI / Test / K8s
  └── evidence
```

虽然都可能来自 Git，但语义完全不同。

State 回答：

```text
现在工程世界是什么状态？
```

Evidence 回答：

```text
为什么相信这个状态？
```

---

# 17. Evidence 第一版改成 Typed Append-only List

旧 Evidence 能力继续保留，但扩展为：

```yaml
evidence:
  - type: git_commit
    source: github
    locator: abc123
    observed_at: 2026-09-07T11:00:00+08:00

  - type: test_result
    source: pytest
    locator: CDC-017
    result: passed
    observed_at: 2026-09-07T11:15:00+08:00
```

Evidence 类型第一版建议：

```text
git_commit
pull_request
test_result
ci_run
deployment
log
manual_check
document
```

Adapter 只负责：

```text
观察外部事实
→ 转成 Evidence
→ 关联 WorkItem
```

不直接修改工程状态，除非经过明确规则。

---

# 18. Event 不再定义为 Fact

v0.2：

```text
Event = Fact
Current State = Projection
```

这个定义会不小心把系统带入 Event Sourcing。

v0.3 第一版明确：

```text
Current State = authoritative mutable state
Activity      = audit / observation history
Evidence      = support for claims/state
```

Activity 示例：

```text
09:00 backend-agent started FEAT-CDC-001
11:00 backend-agent reported progress 60%
13:00 tester-agent reported BUG-017
14:00 backend-agent moved BUG-017 to doing
```

Activity 的作用：

```text
审计
时间线
调试
上下文补充
```

但不是第一版 authoritative state。

因此发生：

```text
WorkItem.status = doing
Activity latest = "reported blocked"
```

时，权威字段仍然是：

```text
WorkItem.status
```

如果未来要做 Event Sourcing，必须单独做 ADR，不在 v0.3 MVP 中隐式引入。

---

# 19. Progress 不是客观事实

v0.2 使用大量：

```text
70%
80%
50%
```

但没有定义百分比的含义。

在 AI Coding 场景中：

```text
Agent 自己说完成 80%
```

并不等于工程事实。

因此 v0.3 定义：

> **Progress 是 Reported State，而不是 Fact。**

推荐：

```yaml
progress:
  value: 0.7
  actor: backend-agent
  reported_at: 2026-09-07T10:30:00+08:00
  basis: implementation-checklist
```

`basis` 第一版可选：

```text
manual
implementation-checklist
test-cases
subtasks
story-points
milestone
```

如果没有可靠计算方式，不要自动生成：

```text
System Progress = 83%
```

可以展示：

```text
3/5 WorkItems done
2 active
1 blocked
```

这往往比虚假的百分比更可靠。

---

# 20. Board 是 Projection

这个抽象继续保留。

```text
Canonical Engineering State
          ↓
      Projection
          ↓
        Board
```

因此删除 Board 输出，不影响状态。

同一份状态可以生成：

```text
Kanban View
Actor View
System View
Version View
Project View
Risk View
Agent Context
```

---

# 21. Board Projection 第一版

建议卡片展示：

```text
┌────────────────────────────┐
│ CDC 增量同步                │
│                            │
│ System   DataX Backend     │
│ Version  v2.0              │
│ Role     Java后端开发       │
│ Actor    backend-agent     │
│ Human    张三               │
│ Progress 70% reported      │
│ Status   Doing             │
│                            │
│ Blocker: K8s Runtime       │
└────────────────────────────┘
```

注意明确写：

```text
70% reported
```

而不是把它伪装成系统计算出的精确事实。

---

# 22. Actor View

```text
backend-agent

Type:
Agent

Accountable Human:
张三

Roles:
Java后端开发

Systems:
DataX Backend

Active Work:
- CDC 增量同步
- BUG-017 checkpoint 恢复失败

Blocked By:
- K8s Runtime
```

Human Actor：

```text
张三

Responsible Systems:
- DataX Backend
- AIDP CP

Agents:
- backend-agent
```

---

# 23. System View

```text
DataX Backend

Responsible:
张三 / Java后端开发

Executor:
backend-agent

Testing:
李四 / 测试

Target Version:
v2.0

Active Work:
- CDC
- Flink checkpoint
- API

Open Bugs:
1

Blockers:
- K8s Runtime
```

---

# 24. Version / Release View

第一版不要制造假精度。

推荐：

```text
DataX Backend / v2.0

WorkItems:
Done       8
Doing      3
Blocked    1
Testing    2

Reported Progress:
CDC        70%
Flink      40%

Blockers:
K8s Runtime
```

如果未来建立明确权重模型，再增加整体百分比。

---

# 25. Agent Context Manifest

这是 v0.3 第一优先级能力。

目录：

```text
.ousheng/
  actors/
    backend-agent.yaml
```

示例：

```yaml
schema_version: 1

actor:
  id: backend-agent
  type: agent
  responsible_human: zhangsan

defaults:
  role: backend

systems:
  - datax-backend

target_version: v2.0
```

Agent 启动时调用：

```text
get_my_context()
```

Core 根据：

```text
Actor
 ↓
Assignment
 ↓
System
 ↓
TargetVersion
 ↓
Active WorkItem
 ↓
Dependencies / Blockers
 ↓
Relevant Evidence
```

构造最小 Context。

---

# 26. Progressive Context Disclosure

第一层只返回：

```json
{
  "actor": "backend-agent",
  "responsible_human": "zhangsan",
  "role": "backend",
  "systems": ["datax-backend"],
  "target_version": "v2.0",
  "active_work": ["FEAT-CDC-001"],
  "blockers": ["K8S-003"]
}
```

Agent 需要细节时再调用：

```text
get_work_item(FEAT-CDC-001)
```

再需要证据：

```text
get_evidence(FEAT-CDC-001)
```

再需要系统关系：

```text
get_system_context(datax-backend)
```

避免一次把：

```text
整个项目
所有卡片
全部历史
全部 evidence
```

塞进上下文。

---

# 27. Git 继续作为 Canonical State

v0.3 第一阶段明确：

> **Git + YAML 继续是工程状态的 canonical representation。**

推荐目录：

```text
.ousheng/
├── project.yaml
├── systems.yaml
├── roles.yaml
├── assignments.yaml
├── actors/
│   ├── zhangsan.yaml
│   └── backend-agent.yaml
├── work/
│   ├── FEAT-CDC-001.yaml
│   └── BUG-017.yaml
├── activity/
│   └── 2026-09.jsonl
└── cache/
    └── index.db
```

如果希望保持更小，也可以：

```text
actors.yaml
systems.yaml
assignments.yaml
work/*.yaml
```

第一版不强制复杂目录。

---

# 28. 为什么第一阶段不把 SQLite 设成事实源

SQLite 很适合：

```text
关系查询
事务
索引
聚合
本地状态
```

但从：

```text
SQLite 适合关系数据
```

不能直接推出：

```text
SQLite 应取代 Git 成为 canonical source
```

目标用户虽然只有：

```text
1～3 人
```

但并不意味着：

```text
1 台机器
```

真实场景可能是：

```text
Laptop
Remote Dev
CI
Container
Cloud Agent
```

因此第二个协作者或第二台机器加入的第一天，就会遇到：

```text
engineering.db 如何同步？
```

如果 SQLite 是唯一事实源，必须额外解决：

```text
同步
定序
冲突
审计
恢复
离线编辑
```

而这些能力当前 Git 已经提供。

所以 v0.3 第一阶段不主动放弃这些成熟能力。

---

# 29. SQLite 的正确第一阶段角色：Derived Index

SQLite 路径：

```text
.ousheng/cache/index.db
```

必须满足：

```text
非 canonical
可删除
可重建
不提交 Git
```

命令：

```bash
ousheng index rebuild
```

流程：

```text
Git YAML
   ↓
Parse / Validate
   ↓
Engineering Model
   ↓
SQLite Index
   ↓
Projection / Query / Context
```

如果索引损坏：

```bash
rm .ousheng/cache/index.db
ousheng index rebuild
```

即可恢复。

---

# 30. 小规模下不依赖 SQLite 也必须可运行

v0.3 不能让 SQLite 成为运行前提。

Core Repository 应提供：

```text
InMemoryIndex
SQLiteIndex
```

接口类似：

```go
type Index interface {
    WorkByActor(...)
    WorkBySystem(...)
    WorkByVersion(...)
    Blockers(...)
    Assignments(...)
}
```

小项目可以：

```text
YAML
↓
Go in-memory index
```

直接工作。

查询量上升后自动或显式使用 SQLite。

这样可以验证：

> **需要的是语义模型，而不是数据库本身。**

---

# 31. 多机协作：继续使用 Git

v0.3 第一阶段同步模型不变：

```text
Machine A
   ↓
Git commit / push

Machine B
   ↓
Git pull
   ↓
ousheng index refresh
```

SQLite 索引不参与 Git merge。

因此不会出现：

```text
engineering.db binary merge conflict
```

也不依赖：

```text
共享 NFS SQLite
中心服务器
Redis
Kafka
Consensus
```

---

# 32. State Store 抽象必须提前存在

虽然第一阶段 canonical store 是 Git + YAML，但 Core 不应该把业务逻辑绑死在 Git。

接口：

```go
type StateRepository interface {
    GetWorkItem(...)
    ListWorkItems(...)
    CreateWorkItem(...)
    UpdateWorkItem(expectRevision ...)
    GetActor(...)
    GetSystem(...)
    GetAssignments(...)
}
```

第一阶段实现：

```text
GitYAMLRepository
```

未来可以新增：

```text
SQLiteRepository
RemoteRepository
```

这保证：

> **存储可以换，但语义模型和业务规则不用重写。**

---

# 33. CAS 规则

所有可并发修改对象使用：

```text
revision
```

写操作：

```text
read revision=7
        ↓
modify
        ↓
write expect_revision=7
```

如果当前已是：

```text
revision=8
```

则返回：

```text
CONFLICT
```

Git commit 不是 CAS 的替代。

CAS 在 Core 层定义。

Git 只负责：

```text
持久化
历史
同步
```

---

# 34. Schema Version

当前 Card 解析使用严格字段策略，因此 v0.3 不能假设增加字段后旧二进制会自动兼容。

所有新 canonical 文件显式加入：

```yaml
schema_version: 2
```

并提供 migrator：

```bash
ousheng migrate schema
```

兼容策略：

```text
v1 binary
→ 只理解 v1

v2 binary
→ 可读取 v1
→ 内部 normalize 为 v2
→ 写回时显式升级
```

不能让旧字段的 CAS `version` 与新产品版本混在一起。

---

# 35. v1 Card 到 v0.3 WorkItem 的迁移

正确映射：

```text
card.id
    ↓
work_item.id

card.owner
    ↓
legacy owner / assignee resolution

card.task
    ↓
work_item.title

card.status
    ↓
contract.status

card.version
    ↓
work_item.revision

card.depends_on
    ↓
work_item.depends_on

card.contract
    ↓
work_item.contract

card.evidence
    ↓
work_item.evidence

card.human_ack
    ↓
work_item.human_ack
```

明确禁止：

```text
card.version
↓
target_version
```

因为二者语义完全不同。

---

# 36. Owner 迁移规则

旧：

```yaml
owner: backend
```

可能代表：

```text
角色
人
Agent
逻辑队列
```

因此不能自动假设。

迁移流程：

```text
owner
  ↓
resolver
```

如果匹配 Actor：

```text
assignee = actor
```

如果匹配 Role：

```text
acting_role = role
assignee = unresolved
```

如果无法判断：

```text
legacy_owner = backend
migration_status = needs_resolution
```

提供：

```bash
ousheng migrate resolve-owner
```

人工一次性解决。

---

# 37. CLI 设计

保留兼容：

```bash
board read
board write
board converge
```

新增：

```bash
ousheng context me
ousheng context actor <actor>
ousheng context system <system>

ousheng work list
ousheng work show <id>
ousheng work create
ousheng work update <id>
ousheng work assign <id>

ousheng bug report
ousheng progress report

ousheng actor list
ousheng actor show <id>

ousheng system list
ousheng system show <id>

ousheng assignment list

ousheng evidence add <work-id>
ousheng evidence list <work-id>

ousheng activity list

ousheng view kanban
ousheng view actor
ousheng view system
ousheng view version

ousheng index rebuild
ousheng index status

ousheng migrate schema
```

---

# 38. MCP 设计

保留：

```text
read_board
write_board
converge
```

新增第一批：

```text
get_my_context
get_actor_context
get_system_context

get_work_item
query_work_items

create_work_item
update_work_item
assign_work_item

report_bug
report_progress

add_evidence
get_evidence
```

第二批再考虑：

```text
get_project_status
get_version_status
get_risk_context
```

不要第一版一次暴露十几个复杂工具。

---

# 39. Context API 的设计原则

`get_my_context()` 不是：

```text
SQL query wrapper
```

而应该是：

```text
面向 Agent 下一步决策的最小上下文投影
```

它必须遵守：

```text
少
相关
可追溯
不总结成幻觉
需要时可展开
```

默认不要注入：

```text
整个 Git log
所有 closed WorkItem
全部 Evidence
其他 Actor 的所有任务
```

---

# 40. Converge 的演进

第一阶段保留旧语义：

```text
Contract verified
```

同时增加 WorkItem 级别：

```text
done
blocked
testing
```

新版：

```bash
ousheng converge
```

输出建议：

```text
CONVERGED
IN_PROGRESS
BLOCKED
```

但第一版计算逻辑应保持可解释：

```text
所有 required Contract 满足
所有 required WorkItem done
不存在 unresolved blocker
必要 evidence 存在
必要 human_ack 存在
```

不要引入复杂评分模型。

---

# 41. External Evidence Adapter

第一版优先：

```text
Git
```

未来：

```text
GitHub
CI
Test
K8s
```

Adapter 模型：

```text
External Source
      ↓
Normalize
      ↓
Typed Evidence
      ↓
Associate WorkItem
```

例如：

```text
commit abc123
```

变成：

```yaml
type: git_commit
source: git
locator: abc123
```

CI：

```yaml
type: ci_run
source: github-actions
locator: run-1024
result: passed
```

---

# 42. Human Accountability

Agent 可以成为：

```text
assignee
executor
activity actor
evidence producer
```

但关键责任仍可以绑定：

```text
accountable_human
```

例如：

```yaml
assignee: backend-agent
accountable_human: zhangsan
```

这样可以避免：

```text
AI 生成
→ AI 验收
→ AI 自己标 verified
```

形成闭环幻觉。

---

# 43. 推荐目录结构

第一阶段建议尽量小：

```text
OuSheng/
│
├── cmd/
│   ├── board/
│   └── mcp/
│
├── internal/
│   ├── model/
│   │   ├── actor.go
│   │   ├── system.go
│   │   ├── assignment.go
│   │   ├── workitem.go
│   │   ├── contract.go
│   │   └── evidence.go
│   │
│   ├── state/
│   │   ├── repository.go
│   │   └── gityaml/
│   │
│   ├── index/
│   │   ├── memory/
│   │   └── sqlite/
│   │
│   ├── context/
│   ├── projection/
│   ├── migrate/
│   └── converge/
│
├── adapters/
│   ├── git/
│   ├── github/
│   ├── claude/
│   └── opencode/
│
├── docs/
│   ├── engineering-model.md
│   ├── board-protocol.md
│   ├── state-store.md
│   └── context-protocol.md
│
└── .ousheng/
    ├── project.yaml
    ├── systems.yaml
    ├── roles.yaml
    ├── assignments.yaml
    ├── actors/
    ├── work/
    ├── activity/
    └── cache/
        └── index.db
```

不建议第一阶段建立：

```text
internal/ontology/schema/reasoning/...
```

避免命名推动过度设计。

---

# 44. Phase 0：语义模型验证

目标：

> **先证明工程语义有价值，不讨论数据库替换。**

产物：

```text
docs/engineering-model.md
model structs
schema examples
five-scenario fixtures
```

必须明确：

```text
Actor
System
Assignment
WorkItem
Contract
Evidence
Activity
ProgressReport
revision
target_version
```

并明确回答：

```text
Event 是否权威？——否
Progress 是否事实？——否
Version 是什么？——target_version，暂不冻结复杂实体
Project 是否实体？——第一版 workspace
Component 是否实体？——否
Person 是否独立于 Actor？——否
```

---

# 45. Phase 0 的反事实验收

在写 SQLite Runtime 之前，必须用 Git + YAML 实现并跑通：

```text
需求
Bug
Progress
Agent Context
Board Projection
```

如果这五个场景都能跑通：

> 证明语义模型成立。

同时也证明：

> SQLite authoritative store 不是这些场景的必要条件。

这一步必须显式写进工程决策记录。

---

# 46. Phase 1：WorkItem v2 + Actor/System/Assignment

在当前 Git Store 上实现：

```text
schema_version
Actor
System
Assignment
WorkItem
revision
target_version
```

保留旧 Card API。

目标：

```text
旧工作流不坏
新工程语义可用
```

---

# 47. Phase 2：Agent Context

实现：

```text
actor manifest
get_my_context
get_system_context
```

这是 v0.3 的最高价值 Phase。

验收：

```text
backend-agent
```

无需扫描整个项目即可知道：

```text
我是 backend-agent
最终负责人是张三
角色是 Java 后端
负责 DataX Backend
目标版本 v2.0
正在做 CDC
当前有 BUG-017
阻塞来自 K8S-003
```

---

# 48. Phase 3：Projection

实现：

```text
Kanban
Actor
System
Version
Project Summary
```

第一版从：

```text
Git YAML
↓
Go in-memory index
```

直接计算。

不要求 SQLite。

---

# 49. Phase 4：Evidence 升级

实现：

```text
typed evidence list
git evidence adapter
manual evidence
test evidence
```

保留：

```text
verified requires evidence
```

但 evidence 可以来自多个来源。

---

# 50. Phase 5：SQLite Derived Index

当关系查询开始明显复杂后，引入：

```text
.ousheng/cache/index.db
```

SQLite 只保存：

```text
可从 canonical YAML 重建的数据
```

验收：

```bash
rm .ousheng/cache/index.db
ousheng index rebuild
```

前后查询结果一致。

这是硬约束。

---

# 51. Phase 6：真实多机协作验证

使用至少：

```text
2 个 Human/Agent
2 台机器
```

完成：

```text
Machine A 修改 WorkItem
push

Machine B pull
refresh index

Machine B 基于旧 revision 写入
→ CAS conflict
```

验证：

```text
同步
CAS
历史
Projection
Context
Evidence
```

全部成立。

这比“第一版单机 SQLite”更符合 OuSheng 的实际产品目标。

---

# 52. 什么时候才考虑 SQLite Authoritative Store

必须至少出现以下一类真实证据：

## 条件 A：跨实体原子事务成为高频需求

例如：

```text
更新 WorkItem
+
更新 Assignment
+
更新 Release
```

必须强原子提交，否则频繁产生错误。

---

## 条件 B：关系完整性成为真实故障来源

例如：

```text
actor 被删除
assignment 悬空
workitem 指向不存在 system
```

且 Git schema validation 无法以合理成本解决。

---

## 条件 C：状态写频率让 Git 模型明显失效

例如：

```text
大量高频 progress/activity 写入
```

导致：

```text
commit 噪音
merge 冲突
同步成本
```

真实不可接受。

---

## 条件 D：已经有明确多机 State Sync 设计

至少必须回答：

```text
谁负责定序？
如何冲突解决？
如何离线工作？
如何审计？
如何恢复？
如何验证签名？
如何支持第二台机器？
```

在这些问题未回答之前：

> 不批准 SQLite 作为唯一事实源。

---

# 53. MVP：智能湖仓

继续使用真实目标项目验证。

Systems：

```text
datax-mcp
aidp-cp
datax-backend
datax-ui
lakehouse-data
lakehouse-k8s
```

Roles：

```text
项目经理
产品经理
UI开发
Java后端开发
湖仓数据开发
K8s/湖仓运维部署开发
测试
```

Actors：

```text
Human A
Human B
Human C
backend-agent
ui-agent
test-agent
```

Assignments：

```text
Actor × Role × System
```

---

# 54. MVP 必须跑通的 6 个动作

v0.2 的五个动作全部保留，并增加多机协作。

## ① Requirement

```text
DataX UI 增加任务配置页面
```

形成：

```text
WorkItem
type=requirement
system=datax-ui
target_version=v2.0
```

---

## ② Bug

```text
DataX Backend CDC checkpoint 恢复失败
```

形成：

```text
WorkItem
type=bug
system=datax-backend
detected_by=test-agent
assignee=backend-agent
accountable_human=张三
```

---

## ③ Progress

```text
DataX Backend v2.0 CDC 完成 70%
```

形成：

```text
ProgressReport
value=0.7
actor=backend-agent
basis=...
timestamp=...
```

而不是把 70% 当成客观事实。

---

## ④ Context

```text
get_my_context()
```

返回：

```text
Actor
Responsible Human
Role
System
Target Version
Active Work
Blockers
Relevant Evidence Summary
```

---

## ⑤ Projection

同一份 canonical state 投影：

```text
Kanban
Actor
System
Version
```

---

## ⑥ Multi-machine

第二台机器：

```text
git pull
index refresh
```

即可得到同一状态。

并验证旧 revision 更新产生：

```text
CAS conflict
```

---

# 55. 成功标准

## S1：工程上下文可表达

能够明确表达：

```text
Actor
Role
System
Assignment
WorkItem
TargetVersion
Contract
Evidence
```

---

## S2：关系可计算

能够回答：

```text
谁负责 DataX Backend？
哪个 Agent 正在执行？
最终责任人是谁？
以什么角色？
有哪些正在进行的 WorkItem？
哪个 Bug 由谁发现？
由谁修复？
目标版本是什么？
有哪些 Blocker？
```

---

## S3：State 与 Evidence 语义分离

可以明确区分：

```text
WorkItem.status = doing
```

与：

```text
commit abc123
test CDC-017 passed
```

后者是证据，不直接等于状态。

---

## S4：Board 不是事实源

删除 Board 输出后：

```text
Canonical State
```

仍存在。

重新 Projection 后可以完整恢复。

---

## S5：SQLite 不是单点

删除：

```text
.ousheng/cache/index.db
```

系统仍可运行。

重建后查询结果一致。

---

## S6：Agent 可直接使用

Agent 不需要扫描整个仓库或整个 Board。

通过：

```text
get_my_context()
```

即可得到下一步行动需要的最小工程上下文。

---

## S7：第二台机器成立

第二个 Human / Agent 不需要：

```text
共享 SQLite 文件
中心服务器
额外数据库
```

只依赖现有 Git 同步即可进入同一工程状态。

---

# 56. 非目标

v0.3 明确不做：

```text
OWL
RDF
SPARQL
Ontology Reasoning
Knowledge Graph
CRDT
Kafka
Redis
PostgreSQL
Central Coordination Server
Full Jira Workflow
Automatic semantic merge
Agent auto-acceptance
Complex progress scoring
Automatic architecture decision
```

这些能力未来只有在真实需求证明必要时再引入。

---

# 57. 推荐最终架构

```text
                              Human
                                │
                              Agent
                                │
                                ↓
                     ┌────────────────────┐
                     │      OuSheng       │
                     │ Engineering Context│
                     │      Runtime       │
                     └─────────┬──────────┘
                               │
                     Engineering Model
                               │
        ┌──────────────────────┼──────────────────────┐
        ↓                      ↓                      ↓
      Actor                  System                WorkItem
        │                      │                      │
        └────────── Assignment┴──────────────┐       │
                                             ↓       ↓
                                          Contract Evidence
                                             │       │
                                             └───┬───┘
                                                 ↓
                                           Current State
                                                 │
                             ┌───────────────────┼───────────────────┐
                             ↓                   ↓                   ↓
                           Board                MCP               Context
                                                                     │
                                                                     ↓
                                                                   Agent

Canonical State
     │
 Git + YAML
     │
     ├─────────────→ History / Diff / Sync / Review
     │
     ↓
Engineering Model
     │
     ├─────────────→ In-memory Index
     │
     └─────────────→ SQLite Derived Index
                              │
                              ↓
                       Query / Projection

External Sources
 Git / GitHub / CI / Test / K8s
              │
              ↓
         Typed Evidence
              │
              └────────────→ WorkItem
```

---

# 58. 最终决策

v0.3 的工程决策可以压缩成四句话：

### 1.

> **OuSheng 的下一阶段确实应该升级工程语义。**

Card/Contract 仍然有价值，但已经不足以独立承担：

```text
Actor × Role × System × TargetVersion × WorkItem
```

的上下文表达。

### 2.

> **语义模型升级与存储替换必须解耦。**

“需要关系模型”不等于“必须立刻把 SQLite 变成事实源”。

### 3.

> **第一阶段保留 Git canonical state，SQLite 作为派生索引。**

这样可以同时拿到：

```text
结构化查询
多视图
Agent Context
```

和：

```text
Git 同步
历史
Diff
CAS
审计
低基础设施
```

### 4.

> **只有真实运行证明 Git canonical state 已成为瓶颈后，才升级 SQLite 的地位。**

而且那时必须先解决：

```text
第二个人
第二台机器
离线编辑
冲突
定序
恢复
审计
```

这些实际问题。

---

# 59. OuSheng 的真正升级

第一代：

```text
Card
+
Contract
+
CAS
+
Git
+
Board
```

解决：

> **不要让多个 AI Worker 互相踩。**

第二代 v0.3：

```text
Engineering Model
+
Actor Context
+
WorkItem
+
Contract
+
Evidence
+
Projection
+
Git Canonical State
+
Optional SQLite Index
```

解决：

> **让 Human 与多个 Agent 在多个系统、多个角色、多个版本下共享同一份最小工程上下文，同时仍然保持可同步、可验证和低介入。**

因此最合适的演进不是：

```text
Git 协作绳
        ↓
SQLite 本体运行时
```

而是：

```text
Git 协作绳
        ↓
有工程语义的协作绳
        ↓
Engineering Context Runtime
```

核心精神不变：

> **少介入，不替人决策，不替 Agent 编码；只把“谁、以什么角色、在哪个系统、面向哪个版本、正在做什么、受什么约束、有哪些证据”讲清楚。**

这才是 OuSheng 第二代最值得投入的方向。
