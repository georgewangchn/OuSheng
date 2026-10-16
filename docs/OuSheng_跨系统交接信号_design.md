# 跨系统交接信号补全设计

## 1. 问题陈述

单人开发多系统场景：A 系统的活需要 B 系统配合或 PM 拍板。dev 切到 B 座位执行时，
`context me` 看不到"A 在等我"；切到 PM 座位看不到"哪些活卡在等我出手"。

绳上信息断流 = 牵引绳失去牵引职能，退化为人脑脚注。

## 2. 第一性原理推演

### 2.1 基本事实

1. 单人开发多系统 = 一个人在不同系统/角色间快速切换
2. 切换后 AI session 启动 → `context me` → 获取注入面
3. 注入面 = 该 actor 作用域内的 active_work + blockers + knowledge + pending_reviews
4. 跨系统交接：A-001 (system A) `depends_on` B-001 (system B)。B 的 `context me` 应显示"A-001 在等 B-001"

### 2.2 正向推演（当前机制）

1. dev 在 A 座位：`context me` → active_work 含 A-001，blockers 含 B-001 ✓
2. dev 切到 B 座位：`context me` → active_work 含 B-001（若 assignee=dev）
3. B-001 只是 active_work 普通条目——**无"被等待"标记**
4. B 不知 A-001 卡在自己 → B 可能先做别的 → A 继续等

**断点确认：反向依赖信号全链缺失。**

### 2.3 逆向推演（加反向依赖后的效果）

- B 的 `context me`：B-001 的 `waited_on_by: [A-001]` → B 知道 A 在等 → 优先处理 B-001
- 这是方向信号（"有人在等"），不替 B 决定怎么做 → 牵引绳职能
- PM 的 `ActorView`：blocked 项带 `blocked_on` 指针 → PM 知道该出手帮谁解阻塞

### 2.4 反事实推演

**假设反向依赖信号会害**：B 看到 A 在等 → B 慌张跳过自己优先级 → 反而乱序。
→ 反例：信号是"有人在等"事实，优先级判断仍在 B。且单人项目 B=A=同一人，信号只防遗忘。**不成立。**

**假设计算成本过高**：反向依赖需遍历所有 work item 的 depends_on。
→ 反例：Index.Rebuild 时已有全量数据，O(n) 扫一遍建反向索引，成本可忽略。**不成立。**

**假设信息过载**：B 被很多 A 依赖 → context me 爆炸。
→ 反例：注入面铁律——只指针不内容。且依赖图是 DAG，天然有界。**不成立。**

**假设 blocked + "等 PM"标注会演变成审批中心**：
→ 反例：不新增审批机制。blocked + 空 depends_on = "等非 work item"，converge 标 warning。
PM 从 ActorView.BlockedWork 看到，自己判断。这是现有 blocked 信号的细化暴露，非新中心。**不成立。**

### 2.5 需求结论

需求合理。反向依赖信号是控制环参考信号 r 的内向半边——
v1 卡协议 `board-protocol.md:52-53` 已明文要求"依赖你的卡（反向查）"，v0.3 WorkItem 层丢了。

## 3. 三个断点分析

### 3.1 断点 1：反向依赖信号全链缺失

**现状**：
- `BlockersOf(id)` 只算正向（"我被谁阻塞"），无反向查询
- `context me` 的 WorkBrief 无"被等待"标记
- `work show` 的 `WorkItemDetail.Deps` 只有正向，无 `DependedBy`
- `view system` 的 `Blockers` 只含系统内被阻塞，不含"别的系统等我"

**修法**：
- Index 接口加 `DependedBy(id string) ([]string, error)`——反向依赖查询
- memory：Rebuild 时建反向索引 `reverseDeps`
- sqlite：查 `work_deps` 表 `WHERE dep_id = ?`
- `WorkBrief` 加 `WaitedOnBy []string`——per-work"谁在等这单"
- `WorkItemDetail` 加 `DependedBy []WorkBrief`——work show detail 层展示反向依赖

### 3.2 断点 2：blocked 不带"等谁"

**现状**：
- `WorkBrief` 无 blocked_on 字段
- `context me` 的 Blockers 聚合到顶层（dep id 列表），per-work 的阻塞对象信息丢了
- PM 的 `ActorView.BlockedWork` 只见"X blocked"不见"等谁"

**修法**：
- `WorkBrief` 加 `BlockedOn []string`——从 `BlockersOf` 派生（dep id 指针列表）
- 不新增字段存储——纯派生，`workBrief` 单点填充

### 3.3 断点 3：待 PM 拍板的非设计决策无轻量落点

**现状**：
- 设计级待拍板 → `designs/` draft → `pending_reviews` 注入 PM ✓
- 非设计级（"这单走 X 还是 Y"）→ 只能 `status=blocked`，但 `depends_on` 不能指向 PM
- C2 ack 只覆盖 breaking contract

**推演**：
- blocked + depends_on 非空 = 等具体 work item（断点 1 反向依赖覆盖）
- blocked + depends_on 空 = 等非 work item（含 PM 拍板、外部依赖）
- 绳不追问"等 PM 还是等外部"——那是语义，PM 看 description/title 自己判断
- **不新增"待拍板"机制**（死法记录：审批中心永久出局）

**判决**：
- converge 加 warning：blocked 且无 depends_on = "blocked without dependency (waiting on what?)"
- PM 的 `ActorView.BlockedWork` 已有 blocked 项 → `BlockedOn` 空的就是"需 PM 出手候选"
- 不新增机制，只暴露现有信号的缺失面

## 4. 四路判决

### 4.1 WaitedOnBy（反向依赖指针）

| 信道 | 判决 | 理由 |
|------|------|------|
| `context me`（WorkBrief） | **接入** | session 启动第一时机方向信号："这单有人在等" |
| `work show`（detail） | **接入** | `DependedBy []WorkBrief` 全量反向依赖摘要 |
| `work list`（扫视层） | **出局** | 扫视层不放深链接（与 design 信号同判决） |
| `view kanban`（扫视层） | **出局** | 同上 |

**引导职能**：B 看到 B-001 被 A-001 等 → 引导优先处理。牵引绳 ✓。

### 4.2 BlockedOn（blocked 项的等待对象派生）

| 信道 | 判决 | 理由 |
|------|------|------|
| `context me`（WorkBrief） | **接入** | "这单卡在等谁"= 地址化 |
| `work show`（detail） | 已有 `depends_on` 全量 ✓ | 不重复 |
| `work list`（扫视层） | **出局** | 扫视层不放深链接 |
| `view kanban`（扫视层） | **出局** | 同上 |

**引导职能**：B 看到 B-001 blocked_on=[B-002] → 引导先做 B-002。牵引绳 ✓。

### 4.3 blocked 无依赖 warning

| 信道 | 判决 | 理由 |
|------|------|------|
| converge | **接入**（warning） | "blocked without dependency" = 信息不足信号 |
| `context me` | 不单独接入 | BlockedOn 空 = 隐含信号（PM 看 BlockedWork + 空 BlockedOn） |
| `work list` | **出局** | 扫视层 |
| `view kanban` | **出局** | 扫视层 |

## 5. 卡点不变式检查

| 检查项 | 结果 |
|--------|------|
| 反向依赖 = 方向信号（不替 B 决策） | ✓ 牵引绳职能 |
| BlockedOn = 地址化（"等那个项"） | ✓ 可见性 |
| 不替 B 决定优先级 | ✓ 不卡语义 |
| 不新增审批机制 | ✓ 不卡 human 手 |
| 注入面只指针 | ✓ 免疫面最小化 |
| 不新增存储字段 | ✓ BlockedOn/WaitedOnBy 纯派生 |
| 一语义一实现 | ✓ DependedBy 接口共享，CLI/web 复用 |

## 6. 实现方案

### 6.1 Index 接口

```go
// DependedBy：反向依赖——哪些 work item 依赖此 id（且自身仍 open）。
DependedBy(id string) ([]string, error)
```

### 6.2 memory 实现

Rebuild 第二遍（建 blockers 的同一遍）建反向索引：
```go
reverseDeps map[string][]string  // dep_id → [work_id...]
```
只收录 open work item 发出的依赖（已关闭项的依赖是历史残留）。

### 6.3 sqlite 实现

```sql
SELECT work_id FROM work_deps d
JOIN work_items w ON w.id = d.work_id
WHERE d.dep_id = ? AND w.status NOT IN ('done','cancelled')
ORDER BY d.work_id
```

### 6.4 context.go 改动

```go
type WorkBrief struct {
    // ... 现有字段 ...
    BlockedOn   []string `json:"blocked_on,omitempty" yaml:"blocked_on,omitempty"`     // 这单被哪些 dep 阻塞（派生）
    WaitedOnBy  []string `json:"waited_on_by,omitempty" yaml:"waited_on_by,omitempty"` // 谁在等这单（反向依赖指针）
}
```

`workBrief` 单点填充（四路验证规则：新字段只改一处）。

`WorkItemDetail` 加：
```go
DependedBy []WorkBrief `json:"depended_by,omitempty" yaml:"depended_by,omitempty"`
```

### 6.5 context_cmd.go 渲染

`context me` 的 active_work 每条加 `blocked_on` / `waited_on_by` 渲染。

### 6.6 converge.go 改动

blocked 且 `len(DependsOn) == 0` → warning：`<id>: blocked without dependency (waiting on what?)`

## 7. 机械锁

| 测试名 | 断言 |
|--------|------|
| `TestDependedBy` | 反向依赖查询正确（A depends_on B → DependedBy(B) 含 A） |
| `TestDependedByExcludesClosed` | 已关闭项的依赖不计入反向（A done → DependedBy(B) 不含 A） |
| S5 等价 | `DependedBy` memory vs sqlite 深度相等 |
| `TestContextWaitedOnBy` | `context me` 的 WorkBrief 含 `waited_on_by` |
| `TestContextBlockedOn` | blocked 项的 WorkBrief 含 `blocked_on` |
| `TestWorkShowDependedBy` | `work show` 含 `depended_by` 反向依赖摘要 |
| `TestBlockedWithoutDependencyWarns` | blocked + 空 depends_on → converge warning |
| `TestLayer1SizeBudget` | 加字段后 context me JSON 仍 < 1500 字节（fixture 数据） |

## 8. 边界与延后

- **延后**：反向依赖的跨系统聚合视图（"系统 B 被系统 A 的 3 张单等"）——判据：真实出现 PM 需要系统级反向汇总。
- **不新增**：blocked_reason 自由文本字段（注入面铁律违反）；PM 审批队列（死法记录）；depends_on 指向非 work item 对象（破坏 DAG 语义）。

## 9. 变更记录

| 日期 | actor | 内容 |
|------|-------|------|
| 2026-10-16 | opencode | 初稿：三断点推演 + 两机制改动 + 一 converge warning |
