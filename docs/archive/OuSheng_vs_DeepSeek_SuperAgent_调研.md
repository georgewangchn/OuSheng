# OuSheng 与 DeepSeek Super-agent 能力对照调研（第一性原理）

> 调研对象：DeepSeek Harness（dsh，运行时框架，2026-08 开源）+ dsh-superagent-prototype（Super-agent 原型，2026-10-31 开源，MIT）。
> 对照对象：OuSheng（本项目多智能体协作治理工具，"绳"）。
> 结论前置：**两者解决的是同一大问题的两个半边——Super-agent 攻"编排/注意力"，OuSheng 守"治理/决策主权"。OuSheng 不应、也不必去做 Super-agent 的自主执行那一半；它正好是 DeepSeek 自己承认的空白。**

---

## 一、Super-agent 到底是什么（实证，非想象）

DeepSeek 实际上发布了**两件**东西，必须分开看，否则会误判：

| 产物 | 定位 | 关键事实 |
|---|---|---|
| **DeepSeek Harness (dsh)** | 智能体**运行时框架**（"Model + Harness = Agent"） | 2026-08-13 开源（MIT）；基于 Cordis 插件内核，"一切皆插件"（模型/工具/技能/会话/沙箱/存储/调度/UI 全是插件）；49 个包、100+ 第一方插件；**append-only Trajectory 日志**保证每次运行全量可追溯（系统提示、推理、工具调用、子代理调度、上下文注入全部落盘，可 resume/fork/replay）；模型无关，可接入近 40 家模型。 |
| **dsh-superagent-prototype** | 跑在 dsh 之上的**主动式编码智能体研究原型** | 2026-10-31 开源；面向**单个 GitHub 仓库**；监听 GitHub（可选飞书），授权后拆并行工作项、各自独立会话/目录执行、以 PR 交回。明确声明"研究原型、非官方产品、非生产可用、可能含 bug"。 |

**Super-agent 的核心机制（来自官方设计文章《Exploring Super-agent Design》）：**

1. **Router + Worker 双层**：Router 常驻、轻量、只分派不下场（甚至不带 Bash）；Worker 隔离会话、满配工具、并行干活。Session 降级为"系统实现细节"，不再是用户的认知负担。
2. **事件驱动 + 两级过滤**：Trigger（群聊/邮件/CI/PR 评论）驱动，而非人逐条下指令；Fast Path（程序规则先筛噪音）+ Slow Path（LLM 判断行动与否）。
3. **订阅与直通**：同一 Thread（一个 PR/一个 IM 话题）的后续事件由对应 Worker 直接订阅接收，绕开 Router，减少协调开销。
4. **上下文即缓存（Inline & Spill）**：近期信息内联进窗口，低频长文 spill 到窗外只留引用。
5. **投机执行**：不确定结果是否被采用，先在后台试，有价值再提交。
6. **存算分离**：编排循环在云、状态入 DB、本地仅轻量 daemon。
7. **Watchdog**：定时外部监控，侦测 Worker 陷入死循环并尝试恢复。
8. **权限三档**：off / observe（默认，只记录"本会做什么"不真动手）/ auto。
9. **配套 Web 面板**：展示"需要人处理的事项"。

**性能（自报，合成负载）**：~300 条消息、约 20 条工作请求下，p50 12.4 分钟、p95 15.7 分钟，对比单会话+子代理基线 29.5 分钟。**代价**：单次运行 cache token 约 2323 万→3177 万（用更多 token 换时间/稳定）。

**DeepSeek 自己承认的 5 个未解问题（关键，见第四节）：** ①长期记忆；②主动行为控制（哪些自动执行、哪些必须人确认）；③多任务互不干扰（冲突/版本/锁定）；④安全与权限；⑤价值衡量。

---

## 二、OuSheng 是什么（基于已落地代码与设计，非想象）

OuSheng 的核心不是"让 AI 跑得更快更自主"，而是**多智能体协作中的决策主权与可追溯**。已落地的实体与原则：

| 实体/机制 | 作用 | 治理含义 |
|---|---|---|
| **design topic**（`designs/<topic>/design.md`） | 方案/决策卡；`status: draft→agreed→superseded/withdrawn`；`doc` 指向现行方案正本 | **人类拍板为硬门**：`agreed` 必须 `decided_by: human`（`converge.go:272` 强校验）；撤回/取代也须 human 门 |
| **work item** | 任务（todo/done），可挂 `related_items` 关联 design、可挂 evidence locator | 任务的"归属"与"佐证"可追溯 |
| **system / team / agent** | 边界上下文、角色、责任人类 | **单一认领不变量**：受管辖文档有且只有一个认领主体 |
| **context me** | 出处/血缘查询：某 artifact 当前状态与来源 | 人看到的始终是"谁、何时、凭什么" |
| **doc 认领层（本轮新增）** | 文件认领、孤儿曝光、陈旧告警 | 文档作为治理对象，不被机器漏掉也不被机器乱改 |
| **投影 vs SSOT 原则** | "投影在 OuSheng，SSOT 在实栈" | OuSheng 是受治理的投影，**不替代**系统的事实源 |

一句话：OuSheng 把"**谁决定了什么、为什么、现行版本是不是它、AI 的产出有没有被人类批准**"做成可审计的事实层；它假设"协作与编排"已经存在，要补的是"决策可信"。

---

## 三、逐能力对照（Super-agent 能力 → OuSheng 现状 → 判据）

| # | Super-agent 能力 | OuSheng 现状 | 判据（能做 / 不该做 / 待补） | 备注 |
|---|---|---|---|---|
| C1 | Router-Worker 并行分派 | **部分**：design→work item、human 派 agent 是"受治理的分派"；但**无自主 Router** 监听环境自动分派 | 待补（且非核心） | OuSheng 的 Worker 概念 ≈ team 成员（人/agent），已有"派活"语义，缺的是"自动触发" |
| C2 | 多渠道接入（GitHub/飞书/IM） | **无**：只吃自身 CLI + MCP | 待补 | 应走 MCP connector，而非 OuSheng 自己写 webhook |
| C3 | 持久 Worker 上下文（连续性） | **强**：`context me` + design 卡 = 受治理的权威上下文，比 Super-agent 的"缓存"更稳（有权威、有版本） | 已强于对方 | 这是 OuSheng 的护城河 |
| C4 | 事件驱动 + 两级过滤 | **无**：pull/命令驱动 | 待补（若做自主） | — |
| C5 | 主动性（投机执行、静默修 bug） | **刻意不做**：fail-closed、`decided_by: human`；不会"无人 @ 也自改" | **不该做** | 这是治理选择，不是缺陷 |
| C6 | 持久化 + Watchdog 自愈 | 状态 git 持久化（持久 ✅）；**无自主 Watchdog** | 持久已具备；自愈待补（仅当引入自主） | — |
| C7 | 权限三档（off/observe/auto） | **部分**：human 批准闸 ≈ auto 的硬门槛；`draft` ≈ observe 雏形 | 部分具备 | — |
| C8 | 运行可追溯（Trajectory） | **强但不同维度**：OuSheng 追的是**决策/出处**可追溯（design.decided_by、context me）；Super-agent 追的是**单次运行**可追溯 | 互补，OuSheng 更强于治理维度 | 两者日志语义不同：run-trace vs decision-trace |
| C9 | 暴露"需人处理事项"面板 | **强**：converge 的 orphan / stale / 未拍板 design 正是"需人处理"清单 | 已具备 | 与 Super-agent 的 web panel 同构 |

**结论**：9 项里，OuSheng **已强于对方 3 项（C3/C8/C9）**、**部分具备 2 项（C1/C7）**、**刻意不做 1 项（C5）**、**待补 3 项（C2/C4/C6 中的自愈）**。没有任何一项是"对方有、OuSheng 本该有却完全缺失且属于核心"的。

---

## 四、第一性原理推演（正向 + 逆向）

### 4.1 根因不同：瓶颈不在同一处

- **Super-agent 的瓶颈**：人同时只能跟 3–5 个活跃会话 → 多任务上限受**人的注意力**限制。它的解法是"把会话管理与上下文同步从人移到系统，AI 调度 AI，人只管目标与验收"。
- **OuSheng 的瓶颈**：多智能体协作里，**决策会丢失、知识碎在会话里、谁拍的板不可问责、AI 产出未经验证就进生产**。它的解法是"把决策主权与出处做成受治理的权威层，人必须留在拍板环里"。

两者是**同一大问题（让多智能体软件研发真的能在规模上跑起来）的两个半边**：一边是"怎么协调得动"，一边是"动出来的东西可信不可信"。

### 4.2 逆向推演（反事实）

- **R1（OuSheng 去学自主执行）**：若 OuSheng 也做 Router-Worker 自动分派 + 静默修 bug，它就退化成又一个 Super-agent，**丢掉"决策可审计"的护城河**，并与"人类必须批准生产变更"的治理需求直接冲突（谁授权自动驾驶改生产代码？）。→ 不成立。
- **R2（Super-agent 只做自主不做治理）**：DeepSeek 文章**自己列出 5 个未解问题**——长期记忆 / 主动行为控制 / 多任务互不干扰 / 安全与权限 / 价值衡量——这 5 个**恰好全是治理问题**，不是编排问题。且 Super-agent 已经有 `observe` 模式 + "展示需人处理事项"的面板，说明它**已经在向治理领地伸手**，只是还没成体系。→ Super-agent 迟早需要 OuSheng 这类东西。
- **R3（两者合并为一套）**：正确形态应是"**Super-agent 当 Router/Worker 执行引擎，OuSheng 当 design authority + 出处 + 人工拍板闸**"。OuSheng 不应重写 Router，而是把 `design.decided_by=human` 这条闸，嵌进 Worker "提交 PR" 的前一步——Worker 要落地变更前，先来 OuSheng 验"这件事有没有被人类拍板（agreed）"。

### 4.3 命名巧合（值得点出）

两者都用 "harness/绳" 隐喻：DeepSeek Harness 是**模型→Agent 的运行时外壳**；OuSheng（绳）是**人-智能体协作的治理外壳**。同词不同层，不是竞品。

---

## 五、结论与产品建议

1. **OuSheng 不能、也不该"做 Super-agent 的事情"**——自主并行执行那一半（C2/C4/C5/C6 自愈）不是它的战场，强行做会自毁差异化。
2. **OuSheng 已经在做、且比 Super-agent 更彻底的是另一半**：决策主权、可追溯、人类在环、需人处理事项清单（C3/C7/C8/C9）。这正好是 DeepSeek 公开承认的空白。
3. **差异化定位一句话（给 CTO）**：OuSheng = 企业级多智能体的**治理中枢**（谁拍板、谁产出、现行版是不是它）；Super-agent = 多智能体的**执行引擎**（并行、自主、自愈）。前者管"可信"，后者管"能跑"。
4. **若要补全，该补的是"接口"而非"引擎"**：把 OuSheng 的 design authority 通过 MCP 暴露出去，让任何执行引擎（包括 dsh-superagent）在"提交变更前"回调 OuSheng 验"是否已 agreed（人类拍板）"。这既守住治理，又不重造 Router/Worker。
5. **风险标注（severity: 中）**：若市场叙事把"更自主"误读为唯一方向，OuSheng 需在对外材料中**主动划清"治理层 ≠ 执行层"**，避免被拿来和 Super-agent 做苹果对桔子的对比。

---

## 六、待办（如需推进）

- [ ] 把"OuSheng 作为治理闸嵌入执行引擎"的 MCP 接口草拟（design authority query API）。
- [ ] 在对外材料里固化"OuSheng = 治理中枢"定位语句，备 CTO 评审用。
- [ ] 本调研与 `OuSheng_文档认领层设计.md` 互为支撑：认领层证明 OuSheng 把"文档"也纳入治理，进一步强化"治理中枢"叙事。
