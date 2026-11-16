# 测试交接信号（tester 待测池）设计

## 0. 起因：实栈死锁

**事故（2026-10-20）**：mlake 说 T-036/BUG-037 需要 test 测试；test 说"没有要测试的"；PM 只能口头指派 test 才开始测。

**死锁结构**：

| 单 | system | assignee | status | acting_role |
|---|---|---|---|---|
| T-036 | mlake | mlake | testing | backend |
| BUG-037 | mlake | mlake | testing | backend |
| T-038 | data | test | blocked | tester |

- T-038 depends_on [T-035, T-036, T-037]；T-035 doing、T-036 testing、T-037 done
- mlake 视角：T-036 在 testing = 「我交测了，等 test」
- test 视角：T-038 blocked on T-036（未 done）= 「还没完成，我继续等」
- **循环等待，无人推进**

**已有诊断**：commit `d3b4c5a`（2026-10-19）已定位同一根因（"data doing→testing 不 reassign，test 的 context me active_work 为空"），处置是加 converge warning。**修法不完整**——warning 只在时机③（session.idle）显示，agent 开工的时机①（session.created → sync → context me）看不到。

---

## 1. 第一性原理推演

### 1.1 基本事实

1. `GetMyContext` 的 `ActiveWork` 只按 `Idx.ByAssignee(actorID)` 填充（`internal/context/context.go:158`）
2. `lifecycle.go`：`doing → testing → done`，**testing 态不动 assignee/acting_role**
3. test 有 `tester × {data,mlake,ui}` 的 executor assignment（实栈 `assignments.yaml` 已核实）
4. `Idx.BySystem` / `Idx.ByStatus` / `Idx.AssignmentsBySystem` 均已存在（memory + sqlite 双实现）
5. `context me` 调 `GetMyContext`（`cmd/ousheng/context_cmd.go:47`），`ActorView` 内嵌 `MyContext`
6. converge 现有 warning 正确触发（实栈输出 5 条 `testing item with non-tester role "backend"`），但时机在 session.idle
7. `WorkItemActive` 含 testing（workitem.go:32）——SystemView 已能看到 testing 活（system 维度不缺）

### 1.2 正向推演（当前机制）

| 角色 | 时机 | 看到什么 | 结论 |
|---|---|---|---|
| test | ① session 启动 sync | `context me`：ActiveWork=[T-038 blocked]，blockers=[T-035,T-036] | T-036 只是一个 blocker ID，不知其 status=testing |
| test | ② 遇问题查上下文 | 同上 | 同上 |
| test | ③ session 空闲 converge | 5 条 warning「reassign to tester」 | 已收工，晚 |
| mlake | ① 启动 sync | 自己的 T-036 testing | 认为已交测 |
| PM | 巡检 converge | 5 条 warning | 需人工转达 test |

**断点**：tester 在开工时机完全看不到「该我测的活」——只知道"我在等 T-036"，不知道"T-036 在等我测"。

### 1.3 逆向推演（加 AwaitingTest 后）

| 角色 | 时机 | 看到什么 | 结论 |
|---|---|---|---|
| test | ① 启动 sync | ActiveWork=[T-038 blocked] + **AwaitingTest=[T-036, BUG-037, BUG-038, BUG-039, T-039]** | 立刻知道 5 个待测活 |
| test | 认领 T-036 | `work assign T-036 --assignee test --role tester` | 问责清晰，进入执行 |
| test | 测完 | `evidence add … test_result` + `work update --status done` | T-038 blocker 解除 |
| mlake | ① 启动 sync | 不变（mlake 无 tester assignment → AwaitingTest 空） | — |
| PM | 巡检 | warning 仍显示（改措辞，见 §6） | 问责缺口仍可见 |

**死锁打破**：test 在开工时机即见待测活，无需 PM 口头指派。

### 1.4 大胆假设与求证

**假设 1：待测池应为派生视图，不新增存储字段**

求证：
- 数据源全部现成：`BySystem` × `AssignmentsBySystem` + `Status=testing`
- 与 §25（BlockedOn/WaitedOnBy 纯派生）同族
- 反例：若新增存储字段（如 `testing_by: test`），需写路径维护 + 四路同步 + CAS 冲突处理 —— 绳变重，且信息会与 assignment 漂移
- **成立**

**假设 2：判据应排除"已被其他 tester 认领"的活**

求证：
- 若只排除 `assignee ≠ me`：多 tester 场景下 test2 会看到 test1 已领的活（噪音）
- 精确判据：`assignee 不是该 system 的 tester`（查 AssignmentsBySystem）
- 成本：每个候选活一次 map 查询（system 级 testerSet 预建，O(1)）
- 反例：若 tester 之间需要"看到彼此在测什么"——SystemView 已提供 system 维度全量，不需要在 AwaitingTest 重复
- **成立**

**假设 3：test 应通过 `work assign` 认领，而非直接测**

求证：
- 直接测（不改 assignee）：账面 assignee 仍是 mlake，但实际操作者是 test —— 问责链模糊
- 认领后测：`assignee=test, acting_role=tester` —— 账面与实际一致，且 CAS 防并发
- 反例：认领多一步命令 —— 但这是职责移交的事实记录，不是冗余
- **成立**（design 推荐流程，不作硬门——A2：职责=语义，绳不强制）

**假设 4：d3b4c5a warning 应改措辞而非删除**

求证：
- B 实现后，"for test actor visibility" 的理由失效（test 不需 reassign 也能看到）
- 但 warning 仍反映"状态移交未完成"（assignee 仍是 dev）—— 对 PM 问责巡检有价值
- 删除会丢失"assignee 移交缺口"信号
- **成立**：改措辞为反映真实信号（见 §6）

**假设 5：AwaitingTest 应进 MyContext（而非仅 ActorView）**

求证：
- `context me` 调 `GetMyContext`（context_cmd.go:47）—— 进 ActorView 则 `context me` 看不到
- ActorView 内嵌 MyContext，自动继承
- **成立**

### 1.5 反事实推演

**若 AwaitingTest 包含 `status=blocked` 的活**：
- test 会看到 T-038（blocked，assignee=test）—— 但 ActiveWork 已有
- 且 blocked 与 testing 语义不同（blocked=等外部，testing=等测试）
- **结论**：仅 testing。

**若 AwaitingTest 不排除 assignee=me**：
- test 自己有 testing 态的活 → ActiveWork 和 AwaitingTest 重复显示
- **结论**：排除 `assignee == me`（ActiveWork 已覆盖）。

**若 tester 不读 `context me`**：
- 绳无解 —— 绳只能把信号放进 context me，agent 不读则无任何机制可救（A1：绳只能治理流经它的写/读）
- 这是 agent 的职责，不是绳的缺口
- **结论**：不为此新增"推送"机制（无 daemon 铁律）。

**若 testing 活数量爆炸（如 50 条）**：
- 噪音风险：AwaitingTest 变长
- 缓解：排序（priority P0→P3，再按 ID）；WorkBrief 是指针（ID/title/status/system/priority），单条约 100 字节
- 反例：若加数量上限，会隐藏待测活（信息损失 > 噪音）
- **结论**：不设上限，靠排序 + 指针最小化。

**若 system 为空（`w.System == ""`）的 testing 活**：
- 不在任何 `myTesterSystems` → 不显示
- 合理：无 system 的活无法判定归属（validateRefs 已在写路径挡空 system，注册表非空时）
- **结论**：自然出局。

**若 bootstrap（assignments 空）**：
- `myTesterSystems` 空 → AwaitingTest 空
- 合理：无排班则无 tester 身份
- **结论**：自然出局。

### 1.6 边界推演

| 边界 | 行为 | 判定 |
|---|---|---|
| 多 tester 同 system | test1 认领后（assignee=test1），test2 的 AwaitingTest 不再含该活 | ✓ |
| 未交接（assignee=dev） | 该 system 所有 tester 都看到 | ✓ 待认领 |
| assignee=me | 排除（ActiveWork 已有） | ✓ |
| 非 tester actor | myTesterSystems 空 | ✓ |
| tester 但 system 无 testing 活 | 空 slice（omitempty 不显示） | ✓ |
| testing 活并发认领 | CAS（revision）防冲突 | ✓ 既有机制 |
| 归因（test 测完 assignee 仍 dev 若未认领） | activity 流记录实际操作者 | 既有审计，延后（§8） |

---

## 2. 需求结论

需求成立。AwaitingTest 是对齐牵引绳定位的**引导信号**：

- 给 tester 方向（"这些该你测"），不替 tester 使劲（不自动派活、不自动测）
- 不卡写：不阻止 dev doing→testing（A2：职责=语义，绳不卡）
- 不新增存储字段、不动 assignee 模型、不加硬门
- 与 §25 同族（派生指针进注入面）

---

## 3. 方案

### 3.1 字段

`internal/context/context.go` 的 `MyContext`：

```go
type MyContext struct {
	// ... 现有字段
	ActiveWork []WorkBrief `json:"active_work,omitempty"`
	// AwaitingTest = 我作为 tester 负责的系统里、status=testing、尚未被 tester 认领的活。
	// 事实投影，不是派活指令：testing 态承担两种语义（交接 / dev 自测），绳不区分
	// （A2 职责=语义），是否该我测由 tester 自行 work show 判断。
	// 派生指针，注入面填充；解 testing 态接收方失明——实栈 2026-10-20 data↔test 死锁。
	AwaitingTest []WorkBrief `json:"awaiting_test,omitempty"`
	Blockers     []string    `json:"blockers,omitempty"`
}
```

### 3.2 判据与实现

`GetMyContext` 中，在 `ActiveWork` 填充之后：

```go
// 待测池：我是 tester 的系统 × status=testing × 未被 tester 认领。
// 与 ActiveWork 同源派生（BySystem + AssignmentsBySystem），不新增存储。
myTesterSystems := map[string]bool{}
for _, as := range assigns { // assigns 已在作用域计算时取得
	if as.Active && as.Role == "tester" && as.Responsibility == model.ResponsibilityExecutor {
		myTesterSystems[as.System] = true
	}
}
var awaiting []model.WorkItem
for sys := range myTesterSystems {
	testerSet := map[string]bool{}
	sysAssigns, err := s.Idx.AssignmentsBySystem(sys)
	if err != nil {
		return nil, err
	}
	for _, as := range sysAssigns {
		if as.Active && as.Role == "tester" && as.Responsibility == model.ResponsibilityExecutor {
			testerSet[as.Actor] = true
		}
	}
	ws, err := s.Idx.BySystem(sys)
	if err != nil {
		return nil, err
	}
	for _, w := range ws {
		if w.Status != model.StatusTesting {
			continue
		}
		if w.Assignee == actorID || testerSet[w.Assignee] {
			continue // 自己名下（ActiveWork 已有）或已被其他 tester 认领
		}
		awaiting = append(awaiting, w)
	}
}
sort.Slice(awaiting, func(i, j int) bool {
	pi, pj := priorityRank(awaiting[i].Priority), priorityRank(awaiting[j].Priority)
	if pi != pj {
		return pi < pj
	}
	return awaiting[i].ID < awaiting[j].ID
})
for _, w := range awaiting {
	ctx.AwaitingTest = append(ctx.AwaitingTest, s.workBrief(w))
}
if ctx.AwaitingTest == nil {
	ctx.AwaitingTest = []WorkBrief{}
}
```

**说明**：
- `workBrief` 是唯一构造点（AGENTS.md 四路验证铁律），AwaitingTest 复用它
- `BlockedOn`/`WaitedOnBy` 是否填充？**不填**——待测池的活是"待测"，其依赖已由 ActiveWork 维度覆盖；此处只给最小指针，避免双重语义
- priority 排序：context 包无现成 rank 函数，内联：

```go
func priorityRank(p string) int {
	if len(p) == 2 && p[0] == 'P' && p[1] >= '0' && p[1] <= '9' {
		return int(p[1] - '0')
	}
	return 99 // 无/非法优先级排最后（写路径已校验 P0..P3，历史数据兜底）
}
```

### 3.3 推荐流程（非硬门）

tester 看到 AwaitingTest 后：

```bash
ousheng work assign T-036 --assignee <me> --role tester   # 认领（CAS）
ousheng evidence add T-036 --type test_result --locator …  # 测试事实
ousheng work update T-036 --status done                    # 通过
```

不设为硬门（A2：职责=语义）。未认领直接测也合法（写路径只 requireActor），只是问责链较弱。

### 3.4 converge warning 改措辞（`d3b4c5a` 追认）

现状：
```
T-036: testing item with non-tester role "backend" — reassign to tester for test actor visibility
```

改为：
```
T-036: testing item with non-tester role "backend" — consider reassigning to tester (tester also sees it via awaiting_test)
```

**理由**（两处修正）：
1. 去掉已失效的 "for test actor visibility"——AwaitingTest 已保证可见性，reassign 不再是"看见"的前提
2. **保留"建议 reassign"**——reassign 是理想路径（问责清晰：assignee 指向实际执行者 + tester 的 ActiveWork 直接可见）；AwaitingTest 是兜底（不依赖 dev 记性）。二者互补，非二选一
3. 措辞用 "consider"（建议）而非 "handoff incomplete"（判定）——A2：职责=语义，绳不判定移交是否"不完整"

### 3.5 文档与铁律

- `AGENTS.md` v0.3 段新增一条：待测池信号（判据/四路判决/机械锁）
- 档案新增 §26：测试交接信号（记录死锁推演 + 裁决）

---

## 4. 卡点不变式检查

| 检查项 | 结果 |
|---|---|
| 卡 agent 的写 | 不卡（纯读派生，无写路径改动）✓ |
| 不卡 human 的手 | 不卡 ✓ |
| 卡跨主体伤害与状态合法性 | 不涉及（可见性）✓ |
| 不卡语义与质量 | 不判断"谁该测"（A2：tester 自己看到自己判断）✓ |
| 引导职能（给 tester 方向） | ✓ |
| 不新增存储字段 | ✓ 纯派生 |
| 注入面最小化（T10） | ✓ WorkBrief 指针，零正文 |
| 一语义一实现 | ✓ 逻辑收 context 层（`GetMyContext` 单点），CLI/MCP/web 共享 |
| fail-closed | 无排班 → 空池（不猜测身份）✓ |
| 无 daemon / 无推送 | ✓ 纯拉取视图 |

---

## 5. 四路判决

| 信道 | 判决 | 理由 |
|---|---|---|
| `context me`（GetMyContext） | **接入** | 核心——tester 开工第一时机即见 ✅ |
| `ActorView` | **继承接入** | 内嵌 MyContext，零代码 |
| `SystemView` | **已覆盖** | `ActiveWork` 用 `WorkItemActive`（含 testing），system 维度不缺 |
| `work show`（detail） | **出局** | 单条活不需要"待测池"概念；detail 已有 Evidence/Deps/DependedBy |
| `work list` | **已覆盖** | 现有 `--status testing` 过滤（work_cmd.go:45），不新增专用 flag |
| `view kanban` | **出局** | 扫视层（kanban 已显示 testing 列） |

---

## 6. 机械锁

| 测试名 | 断言 |
|---|---|
| `TestContextAwaitingTestShowsUnclaimed` | tester 的 context me 含 system 内 assignee=dev 的 testing 活 |
| `TestContextAwaitingTestExcludesOwn` | assignee=me 的 testing 活不在 AwaitingTest（在 ActiveWork） |
| `TestContextAwaitingTestExcludesClaimedByOtherTester` | assignee=另一 tester 的 testing 活不显示 |
| `TestContextAwaitingTestEmptyForNonTester` | 非 tester actor 的 AwaitingTest 为空 |
| `TestContextAwaitingTestEmptyWithoutAssignment` | bootstrap（无 assignment）为空 |
| `TestContextAwaitingTestPriorityOrder` | 按 P0→P3、同优先级按 ID 排序 |
| ~~`TestAwaitingTestEquivalence`~~ | **不新增**——AwaitingTest 只用 `BySystem`/`ByStatus`/`AssignmentsBySystem`，三者已在 `TestEquivalenceWithMemory`（sqlite_test.go:46）全等价（含 items 深度相等）；§17 不为已锁机制再造验证层 |
| `TestTestingNonTesterRoleWarns`（改） | converge warning 新措辞（handoff incomplete） |

---

## 7. 与既有机制的关系

| 机制 | 关系 |
|---|---|
| §25 BlockedOn/WaitedOnBy | 同族派生视图；AwaitingTest 补"testing 接收方"维度（§25 补"依赖方向"） |
| §8 越位曝光（跨域执行） | 不重叠：§8 查 assignee×executor，本 design 查 tester×testing 态 |
| §23 拍板身份门 | 不涉及（无权威操作） |
| d3b4c5a converge warning | 保留，改措辞（§3.4）——从"可见性理由"改为"移交缺口理由" |
| SystemView | 已含 active（含 testing），不重复 |
| work list --status | 已覆盖，不新增 |

---

## 8. 边界与延后

**不新增**：
- assignee 自动移交（写路径动 assignee = 绳替人做决定，A2）
- testing 态硬门（A2：职责=语义）
- 推送/通知机制（无 daemon 铁律）
- `testing_by` 字段（派生可见，无需存储）

**延后（记判据防丢）**：
- **assignee 移交机制**（判据：真实出现"谁负责 testing 态"的归因纠纷，或 tester 认领流程被证明摩擦过大）
- **多 tester 协商/认领锁**（判据：出现两个 tester 重复测同一活的真实事故；当前 CAS 已防并发写）
- **AwaitingTest 进 SystemView 的 tester 子视图**（判据：PM 需要在 system 维度区分"待测"与"在测"）
- **testing 双语义的模型级区分**（判据：AwaitingTest 假阳性——dev 自测闭环被 tester 误当待测——造成真实误工。届时才考虑 testing 细分状态或显式 handoff 字段；当前接受假阳性，成本=多看一眼）

---

## 9. 变更记录

| 日期 | actor | 内容 |
|---|---|---|
| 2026-10-20 | opencode | 初稿：AwaitingTest 派生视图 + warning 措辞修正 |
