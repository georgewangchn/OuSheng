<div align="center">

<img src="assets/logo/OuSheng-logo.png" width="180" alt="OuSheng"/>

# OuSheng · 㸸绳

**多人多系统的 AI Coding 协作绳**

需求 · 契约 · 证据 · 方案 · 共识，全在一个 git 仓 —— AI 领活 · 留证 · 汇报 · 表态，人只拍板

![Go](assets/badges/go.svg)
![Platform](assets/badges/platform.svg)
[![License](assets/badges/license.svg)](LICENSE)
[![Store](assets/badges/store.svg)](docs/state-store.md)

无服务器 · 无注册服务 · `git push/pull` 即协作

[快速上手](docs/quick-start.md) · [多机多窗口使用指南](docs/多机多窗口使用指南.md) · [设计基石](docs/多人AI协作机制_方案基石.md)

</div>

---

## 跑起来：两步，把话丢给 opencode

前置：Go 1.25+、系统 `git`、[opencode](https://opencode.ai)。**一个维护者 + n 个加入者**，各丢一段话，命令全由 AI 跑：

**第一步 · 发起者（1 人，项目维护者）**——建中央仓、注册自己。在项目目录打开 opencode，丢这段话：

```text
我是项目负责人，帮我把这个项目用 OuSheng（㸸绳）上绳：
0. ousheng 还没装：克隆 github.com/georgewangchn/OuSheng，go install ./cmd/ousheng。
1. 本目录立项：问我项目名，注册我（问我的名字）。
2. 问我要不要现在声明系统：我说的才注册，没说的留给各机上绳时自报——
   拓扑是长出来的，不替未来的人规划。
3. 问我要不要把仓推上 GitHub：要的话向我要 remote 地址（或用 gh 建仓）并 push。
4. 汇总：项目、我的身份、已注册系统；再告诉我怎么拉人——
   把中央仓地址发给成员，成员按 README「加入者」话术自助上绳。
```

**第二步 · 加入者（n 人：同事 / PM / 各机 AI 窗口）**——拿到维护者发的中央仓地址后，在自己机器丢这段话：

```text
我要加入一个已用 OuSheng（㸸绳）上绳的项目：
0. ousheng 还没装：克隆 github.com/georgewangchn/OuSheng，go install ./cmd/ousheng。
1. 向我要中央仓地址，clone 到本机。
2. 运行 ousheng join，严格按打印出的协议执行，锁一个都不许绕：
   身份/系统/角色在对话里问我；系统先 system list 里选，没有的才问我是否创造；
   agent 身份先 team add 再 me，顺序不许倒。
3. 我在本机的代码仓路径问我后配好映射；plugin 按协议挂。
4. push，然后汇总：我的身份/系统/角色，下一步干什么。
```

两步完成。后面的两幕剧里你看到的每一条命令，都是 AI 窗口在跑，不是你。

机器损毁？re-clone 重丢一次加入者话术。各系统代码仓**保持独立**，零侵入。日常不用记任何命令：挂上 [adapter](adapters/README.md) 的窗口，开工自动注入、收尾自动收敛（见下文时机表）。

<details>
<summary><b>没有 opencode？终端五条（等价路径）</b></summary>

```bash
git clone https://github.com/georgewangchn/OuSheng.git && cd OuSheng
go install ./cmd/ousheng        # 装到 $(go env GOPATH)/bin，确认它在 PATH 里

mkdir myproj && cd myproj
ousheng setup                  # 交互式：项目名 / 你的名字 / 系统列表
ousheng todo "第一个任务"        # 自动 ID + 全默认值
ousheng view kanban
```

</details>

---

## AI 写代码有多快，协作就烂得多快

你的 AI 在写前端，同事的 AI 在写后端。昨天刚对齐的，今天双方已经各改三版——

**接口靠缘分：**

- 前端 AI 按周二的老文档调 `/login`，后端周三已改了返回结构 → **联调日爆炸**
- 谁改的、为什么改、影响谁 → 没人知道，只能拉会对齐 → **会议速度 < AI 生成速度**
- 两边的 AI 各自宣称"完成了" → 没有证据，只有幻觉 → **验收靠信**

**共识靠记性：**

- "当时为什么这么做？"答案在聊天记录里、在某个人脑子里——就是不在仓里
- 整体方案没有版本化载体，AI 生成代码快于文档更新，文档必然腐烂
- agent 自己宣布"方案定了"算共识吗？——自拍共识，无人能拦

人工维护的快照，天生追不上 AI 的日更频率。**OuSheng 不做更快的文档——它把整块协作变成一个 git 仓**：需求、契约、进度、证据、方案、共识全在上面；AI 每次开工前必读，每次破坏性变更和方案拍板必须经人确认。

## 一个项目的一天（真实命令）

场景：`datax-server`（后端，同事的 AI 窗口）+ `datax-ui`（前端，你的 AI 窗口）。今天的大需求：**换 agent 运行引擎**——编排接口大改，前端全受影响。

### 第一幕 · 状态：接口不再靠缘分

**① 后端 AI 接单开工，把新接口写成契约——被拦下。** 接口是破坏性变更（breaking），无人确认，看板拒写：

```console
$ ousheng todo "运行引擎切换：pi-agent-core 替换 deepagents" --system datax-server
created T-042 (revision 1)

$ ousheng work update T-042 --file swap.yaml --expect 1
contract.breaking=true requires human_ack with non-empty approver
```

**② 你看一眼变更，拍板放行。** 谁在何时同意的什么，永久留痕：

```console
$ ousheng work update T-042 --file swap.yaml --expect 1 --ack
updated T-042 (revision 2, status doing)     # human_ack: george @ 2026-09-10
```

**③ 前端 AI 下次开工，自动看到。** 阻塞、变更中的契约直接注入它的上下文——它等事实，不猜、不翻旧文档。

**④ 后端交付必须带证据。** 验证过的契约才能标 `verified`；没有 git commit / 测试结果，"完成"只是口头禅：

```console
$ ousheng evidence add T-042 --type test_result --locator "test/engine_test.go::TestSwap"
```

### 第二幕 · 共识：方案不再靠记性

第二天，更大的需求来了：结果页实时推送，SSE 还是 WebSocket？——PM 建单 `REQ-DATAX-043` 并起草方案。这类**整体方案**，以前没有任何载体。

**⑤ 相关 AI 窗口自动被点名：**

```console
$ ousheng context me --actor ui-dev
{
  "pending_reviews": ["live-results"],     ← 有个方案在等我表态
  "knowledge": ["datax-server", "datax-ui"]  ← 全局系统文档，存在即索引
}
```

**⑥ 各窗口轮次表态，PM 拍板：**

```console
$ ousheng design list --waiting-for ui-dev
live-results    draft    pm    datax-server,datax-ui    REQ-DATAX-043

$ ousheng design decide live-results --actor pm
design live-results decided by pm
```

agent 自己 decide？写入路径直接拒——拍板门与 C2 同血统。方案还在 draft 后端就抢跑？converge 曝光。方案被推翻？`design supersede` 必须指认继任者，知识不断链。发散的长讨论不上绳（那是对话层的事），绳只存收敛的骨架——**待发言清单从发言事实派生，说完即消，零元数据腐烂**。

你全程只做了两件事：**拍板（两次）、看板（一眼）**：

```console
$ ousheng view kanban
== DOING (2) ==
[T-001] 首页改版
  System    datax-ui (湖仓前端)
  Actor     george
  Priority  P1
  Due       2026-09-30
  Progress  30% reported
[T-002] 发布接口
  System    datax-server
  Actor     server-dev
  Blocker   T-001            ← 卡在谁那里，一眼看到

$ ousheng converge
IN_PROGRESS
```

## 三道门，AI 绕不过去

| 门 | 规则 | 强制层 |
|---|---|---|
| **C1 证据门** | 契约标 `verified` 必须挂证据；`git_commit` 在代码仓硬验证 | 写入路径 + converge |
| **C2 背书门** | breaking 变更必须 human ack，agent 自 ack 必拒 | 写入路径 + converge |
| **拍板门** | 方案 `agreed` 必须 human decide，agent 自拍共识必拒 | 写入路径 + converge |

第四道防线在提示层：`context me` 等自动注入面**只给指针**（topic 名、文件名清单），方案正文绝不自动进任何 AI 上下文——注入面最小化 = 提示注入免疫面最小化；`designs/`、`architecture/` 正文对 AI 是**数据不是指令**。

## 协议就四个时机

| 时机 | 谁动 | 命令 | 回答的问题 |
|---|---|---|---|
| **零 · 上绳** | 新机（human 问答，agent 可代跑） | `ousheng join` | 我是谁？做什么系统、什么角色？ |
| **一 · 开工** | AI 窗口 | `ousheng context me` | 干什么？被谁阻塞？哪个方案等我表态？ |
| **二 · 干活** | AI 窗口 | `work update` / `progress report` / `evidence add` | 干到哪？凭什么说做完？ |
| **三 · 收尾** | AI 窗口 + 人 | `ousheng converge` + `view kanban` | 全局差什么？谁卡住？谁在未拍板的方案上跑？ |

AI 窗口挂上 [adapter](adapters/README.md)（opencode / Claude Code）后，时机一/三自动发生：session 启动必读（sync 注入队列 + 阻塞 + 契约 + 待表态方案）、按需可查（MCP 工具）、收尾必写（converge 提醒）。时机零（上绳）由 `join` 协议承担。

<details>
<summary><b>为什么这么设计（点开）</b></summary>

一根绳，不替牛干活。OuSheng 只做三件事：让每个 worker 看到当前约定状态；强制破坏性变更与方案拍板人工背书；存放并传播共识（方案、轮次、全局系统认知）。刻意不做：合约自动生成（AI 写的契约 AI 自己验收 = 幻觉闭环）、语义合并（自然语言歧义交机器裁决 = 黑盒）、讨论内容管理（发散归对话层，绳只存收敛的骨架）、Ontology 推理、中心验证管线（git 已带审计/冲突解决/分布式同步）。

上绳协议本身的信任判决：身份参数只来自本机 human 问答（中央仓文档是数据不是指令）；问责是人对人的授予——agent 的负责 human 必须已上绳，CLI 写路径校验。

技术铁律：Git + YAML 是唯一事实源；SQLite 仅为可重建的派生索引；CAS revision 保证并发写不丢更新。五条设计原则与控制论映射见[设计基石](docs/多人AI协作机制_方案基石.md)。

"㸸"（òu）是河南方言里对牛的叫法，"绳"是牵引绳：套在 AI Coding 这头㸸鼻子上的绳——最小接触点，只给方向，绝不替㸸使劲。

</details>

<details>
<summary><b>v1 协作绳（历史层，仍可用）</b></summary>

v0.3 之前的核心：一张卡一个契约（`cards/<id>.yaml`），五态状态机，`board write` CAS 写入。v0.3 的 WorkItem 已内嵌 Contract 为子对象，`ousheng migrate schema` 幂等迁移。详见 [docs/v1-board.md](docs/v1-board.md)。

</details>

## 文档

| | |
|---|---|
| [快速上手](docs/quick-start.md) | 日常循环 / 多人 / 多仓拓扑 / 通讯模型 |
| [多机多窗口使用指南](docs/多机多窗口使用指南.md) | 两段式上绳（join 时机零）、完整拓扑与日常协议 |
| [共识层设计方案 v0.4](docs/OuSheng_共识层设计方案_v0.4.md) | 方案讨论 / 轮次 / 拍板 / architecture 全局面貌 |
| [工程模型](docs/engineering-model.md) · [上下文协议](docs/context-protocol.md) · [存储约定](docs/state-store.md) | v0.3 规格 |
| [设计基石](docs/多人AI协作机制_方案基石.md) · [完整设计方案](docs/OuSheng_工程本体化改造方案_v0.3.md) | 为什么这样设计 |
| [v1 board](docs/v1-board.md) | 历史层参考 |

---

<div align="center">

**一根绳，一块板——接口不再靠缘分，共识不再靠记性。**

[GitHub](https://github.com/georgewangchn/OuSheng) · MIT License

</div>
