# board-protocol — OuSheng 采样纪律

> 跨工具可移植的协议 markdown。任何 AI Coding 工具（Claude Code / opencode / Codex / Pi / 人）读此文件即知如何与 OuSheng 看板协作。等价于 AGENTS.md 类规则文档，但专管采样与投影。

本文档是**软纪律**。硬闸门（schema 校验、CAS、状态机）落在 core lib 的 `write_board`，软纪律管的是"何时采样"与"采什么"。软纪律会被偶尔忽略，硬闸门不会——但软纪律是协作效率的保证，不是可选的礼貌。

---

## v0.3 重要更新：查看时机协议取代逐 loop 采样

v0.3 起，工程工作（`.ousheng/work/` WorkItem）的协作遵循**三时机协议**
（详见 [`context-protocol.md`](context-protocol.md)）：

1. **session 启动** → `ousheng sync`（pull + 刷新 + 我的上下文）
2. **遇到问题** → `ousheng context me` / `query_work_items`
3. **任务结束** → `work update` / `progress report` / `evidence add`，再看板

不再要求每个控制周期 read→write。看板查看是低频、事件驱动的——
按人的思路：早上开工看一眼、出事看一眼、干完活更新再看一眼。

本文其余章节继续适用于 **v1 Card 协作**（`cards/`，契约生命周期）；
两套工作流可共存（`ousheng migrate schema` 迁移）。

---

## 0. 角色与心智模型

你是协作环中的一个节点。你的本地工作（写代码、跑测试、改实现）是**plant 内部闭环**——那是你的事，OuSheng 不介入。

OuSheng 只管你与他人的**边界**：每个控制周期（一次 loop 迭代），你在两个点与看板交互：

```
周期开始 ── read_board ──► 采样参考 r（你的契约 + 依赖方契约变更＝扰动）
   │
   ├─ 你 + 你的工具在本地闭环：编码 → 跑测试 → 修误差
   │
周期结束 ── write_board ──► 发布投影 y（你的契约/状态），成为他人的 r / 扰动
```

**第一性事实**：对外协作所需信息 C ≪ 内部完整状态 I。你只向看板投影 C，绝不投影 I。

---

## 1. 采样律（何时读写）

### 1.1 必须读

**每个工作周期开始，先 `read_board`。**

读什么：
- 你 own 的卡（你的契约 + 当前状态）
- 你依赖的卡（`depends_on` 指向的卡，看它们是否变更、是否已 verified/deprecated）
- 依赖你的卡（反向查，看消费方是否有新需求或漂移）

不读就开工 = 在陈旧或空的参考上瞎跑 = 开环失控。这是**最常被忽略的纪律**，也是**最致命的**。

### 1.2 必须写

**每个工作周期结束，`write_board` 发布本周期产出。**

写什么（只写 C，见 §2）：
- 契约变更（interface 形状/behavior 变了）
- 状态迁移（proposed→agreed→live→verified）
- evidence（你作为消费方跑通集成探针的结果）

即使本周期没有契约变更，若状态推进了也要写。若什么都没变，不写（避免噪声 commit）。

### 1.3 冲突处理

`write_board` 返回 `version conflict` 时：

1. **立即 `read_board`** 拿到最新版本
2. **调和**：你的变更与他人变更是否兼容？
   - 兼容（改的是不同字段/不同方面）→ 在最新版本上重放你的变更，重写
   - 不兼容（改了同一字段、语义冲突）→ **停手，上抛人类**。不要自动覆盖，不要静默放弃
3. 调和后重写时，`-expect` 用新读到的 version

**禁止**：忽略 conflict 直接覆盖。CAS 是OuSheng 的并发保护，绕过它等于破坏协作信任。

### 1.4 收敛判定

周期结束时若想确认全局状态，`converge`：
- `CONVERGED` → 全板契约已 verified，可收工
- `IN_PROGRESS` → 还有卡未到 verified，继续推进
- `STUCK` → 有依赖环或 broken 依赖，**人介入**打断（拆分契约或引入仲裁点），工具不会自动解环

---

## 2. 投影律（采什么/写什么）

### 2.1 只投影 C

**C = 跨边界必需信息**。具体到 OuSheng，C 是：
- 契约形状（`contract.interface`：method/path/request/response/behavior）
- 契约状态（`status`：proposed/agreed/live/verified/deprecated）
- 依赖关系（`depends_on`）
- 行为证据（`evidence`：探针描述 + 通过的 commit + 验证方）
- 破坏性标记与人工背书（`contract.breaking` + `human_ack`）

### 2.2 绝不投影 I

**I = 局部实现信息**。以下**严禁**写进卡片：

| 禁止写 | 为什么 |
|---|---|
| 源码块 / 代码片段 | 卡是契约不是草稿；硬闸门会拒（禁 ```` ``` ````）|
| stack trace / 调试日志 | 是 I，不是 C；硬闸门会拒（禁 traceback）|
| 推理过程 / 设计理由 | 是 I；契约只需声明形状，不需解释为什么 |
| Memory / 上下文 / 中间产物 | 是 I；跨节点共享 I 违反第一性原理 |
| 实现细节 / 内部架构 | 是 I；消费方只依赖契约形状 |

**反事实自查**：把这条信息写进卡里，去掉它系统还能协作吗？
- 能 → 它是 I，删掉
- 不能 → 它是 C，保留

### 2.3 behavior 字段是 C1 生死线

契约不能只有形状（method/path/schema），必须带 `behavior`——一句话说清"有效交互产出什么"。否则"形状对、语义错"的实现能骗过下游。

```
✗ behavior: ""
✗ behavior: "见接口文档"
✓ behavior: "有效凭证返回 token；无效返回 401"
```

消费方的集成探针依据 `behavior` 测真实接口，不是依据 schema 形状。

### 2.4 evidence 必须真实

`status=verified` 时必须带 `evidence`：
- `probe`：你实际跑的探针（测试名/命令/场景），不是"已测试"
- `passed_at_commit`：探针通过时的 git commit hash，可追溯
- `by`：验证方身份（**消费方**，不是生产方自证）

**禁止**：生产方自己验证自己写 `verified`。C1 要求消费方行为级探针——你依赖的卡由你验证，你提供的卡由消费方验证。

---

## 3. 生命周期纪律

### 3.1 状态迁移路径

```
proposed ──→ agreed ──→ live ──→ verified
   │           │          │          │
   └───────────┴──────────┴──────────┴──→ deprecated
```

- `proposed`：任一方可发起（生产方承诺 / 消费方提需求）
- `agreed`：**契约 Owner 仲裁**。消费方只能提议，不能强推 agreed。分歧上抛人类
- `live`：Owner 开始实现，契约成为下游可依赖的参考
- `verified`：**消费方**行为探针通过后写。生产方不能自证 verified
- `deprecated`：任何状态可放弃。依赖它的卡需迁移

### 3.2 破坏性变更必须人工背书

`contract.breaking=true` 时，**必须**带 `human_ack.approver`（非空人名）。否则 `write_board` 拒绝。

这不是工具能自动绕过的——硬闸门在 core lib。若你发起破坏性变更，先找人批。

### 3.3 不许假 verified

- 没有 evidence 不许写 verified
- evidence 不完整（字段空）不许写 verified
- 生产方自己跑通自己写 verified = 假 verified，污染下游

---

## 4. 工具适配硬度

不同工具的采样保证硬度不同，按原生能力取最硬档：

| 工具 | 硬度 | 机制 |
|---|---|---|
| Claude Code | 硬 | `SessionStart` hook 强制注入 read；`Stop` hook 强制 write |
| opencode | 半硬 | plugin 的 session 开始/结束事件触发 read / write |
| pi / codex / 人 / 无 hook 工具 | 软 | 读本文档，自觉遵守采样律 |

**软档工具**（无 hook）：把本文档放进你的 AGENTS.md / 系统提示 / 工作约定。每个周期开始前问自己"我 read_board 了吗？"，结束前问"我该 write_board 吗？"。

**硬档工具**（有 hook）：hook 会在你启动/结束时自动调 `board read` / `board write`，你无需自觉——但投影律（§2）仍需自觉，hook 管不了你往卡里塞什么。

---

## 5. 反模式（禁止）

| 反模式 | 为什么错 |
|---|---|
| 不 read 直接改代码 | 开环，在陈旧参考上瞎跑 |
| 改了契约不 write | 他人不知道契约变了，依赖方在旧契约上集成 → 集成地狱 |
| write 时塞实现细节 | 污染看板，违反 C≪I；硬闸门可能拒 |
| 忽略 conflict 强写 | 破坏 CAS 信任，覆盖他人变更 |
| 生产方自证 verified | C1 失效，假证据污染下游 |
| breaking 变更不找人批 | C2 失效，破坏性变更逃脱人工闸门 |
| 卡里写代码块/traceback | 违反投影律，硬闸门拒 |
| 依赖成环不处理 | STUCK 是诚实信号，不处理就永远卡死 |

---

## 6. 一次完整周期（示例）

你是 `backend`，要提供登录接口。`frontend` 依赖你。

```
1. read_board -owner backend -status proposed
   → 看到自己的卡 user-auth-api (proposed, v1)
   → read_board -id user-auth-api  确认契约形状

2. 本地实现：写代码、跑单元测试、修 bug
   （这是 plant 内部闭环，OuSheng 不介入）

3. 契约没变，状态推进 proposed→agreed
   write -file auth.yaml -expect 1
   → written user-auth-api version 2

4. 继续实现，上线 agreed→live
   write -file auth.yaml -expect 2
   → version 3

5. frontend 集成测试通过，由 frontend 写 evidence 推 live→verified
   （不是你自证，是消费方验证）

6. converge
   → 若全板 verified：CONVERGED，收工
   → 若有 STUCK：人介入
```

---

## 附：与硬闸门的关系

| 纪律 | 软（本文档）| 硬（core lib）|
|---|---|---|
| 何时采样 | ✓ 采样律 §1 | — |
| 写什么 | ✓ 投影律 §2 | 部分（禁代码块/traceback/超尺寸）|
| 状态迁移 | ✓ §3.1 | ✓ `lifecycle.CanTransition` |
| breaking 人批 | ✓ §3.2 | ✓ `Validate` 拒无 human_ack |
| verified 需 evidence | ✓ §3.3 | ✓ `Validate` 拒不完整 evidence |
| CAS 冲突处理 | ✓ §1.3 | ✓ `store.Write` 拒版本不匹配 |
| DAG 无环 | — | ✓ `graph.FindCycle` |

软纪律管"应该做什么"，硬闸管"绝对不能做什么"。两者纵深防御。
