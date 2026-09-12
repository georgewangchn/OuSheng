<div align="center">

<img src="assets/logo/OuSheng-logo.png" width="180" alt="OuSheng"/>

# OuSheng · 㸸绳

**多人多系统的 AI Coding 敏捷看板**

看板就是一个 git 仓 —— AI 窗口领活 · 留证 · 汇报，人只拍板

*An agile kanban for AI coding — the board itself is a git repository.*

![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)
![License](https://img.shields.io/badge/license-MIT-green)
![Store](https://img.shields.io/badge/store-Git%20%2B%20YAML-orange)

无服务器 · 无配置中心 · `git push/pull` 即协作

</div>

---

## 你大概正卡在这里

**AI 写代码有多快，接口对不齐就有多痛。**

**你的 AI 在写前端，同事的 AI 在写后端。** 昨天刚对齐的接口文档，今天双方的 AI 已经各改了三版——

- 前端 AI 按周二的老文档调 `/login`，后端 AI 周三已改了返回结构 → **联调日爆炸**
- 谁改的、为什么改、影响谁 → 没人知道，只能拉会对齐 → **会议速度 < AI 生成速度**
- 两边的 AI 各自宣称"完成了" → 没有证据，只有幻觉 → **验收靠信**

接口文档这种"人工维护的快照"，天生追不上 AI 的日更频率。**OuSheng 不做更快的文档——它把整块看板变成一个 git 仓**：需求、契约、进度、证据全在上面，双方 AI 每次开工前必读，每次破坏性修改必须经人确认。

**举个例子。** 你的项目有两个系统：`datax-server`（后端，同事的 AI 窗口在写）、`datax-ui`（前端，你的 AI 窗口在写）。今天的需求：**把 agent 运行引擎从 deepagents 换成 pi-agent-core** —— 编排接口要大改，前端全受影响。

**① 后端 AI 接需求、开工**

```console
$ ousheng todo "运行引擎切换：pi-agent-core 替换 deepagents" --system datax-server
created T-042 (revision 1)
```

**② 它把新接口写成契约提交 —— 被拦下**

接口是破坏性变更（breaking），没有人工确认，看板**拒绝写入**：

```console
$ ousheng work update T-042 --file swap.yaml --expect 1
contract.breaking=true requires human_ack with non-empty approver
```

**③ 你看一眼变更，拍板放行**

同一个文件，加 `--ack`。谁在何时同意的什么，永久留痕：

```console
$ ousheng work update T-042 --file swap.yaml --expect 1 --ack
updated T-042 (revision 2, status doing)     # human_ack: george @ 2026-09-10
```

**④ 前端 AI 下次开工，自动看到**

它的窗口 session 一启动，sync 就注入它的上下文：「对话编排页」被 `T-042` 阻塞中，接口正在变。**它等事实，不猜、不翻旧文档。**

**⑤ 后端交付必须带证据**

```console
$ ousheng evidence add T-042 --type test_result --locator "test/engine_test.go::TestSwap"
```

验证过的契约才能标 `verified`（C1 门）。前端 AI 看到的，永远是**有证据的接口**。

你全程只做了两个动作：**拍板（一步）、看板（一眼）**。

看板长这样（真实输出）：

```console
$ ousheng view kanban
== DOING (2) ==
[T-001] 首页改版
  System    datax-ui (湖仓前端)
  Actor     george
  Progress  30% reported
[T-002] 发布接口
  System    datax-server
  Actor     server-dev
  Blocker   T-001            ← 卡在谁那里，一眼看到

$ ousheng converge
IN_PROGRESS
```

## 跑起来（3 分钟）

前置：Go 1.25+、系统 `git`。

```bash
git clone git@github.com:georgewangchn/OuSheng.git && cd OuSheng
go build -o /usr/local/bin/ousheng ./cmd/ousheng

mkdir myproj && cd myproj
ousheng setup                  # 交互式：项目名 / 你的名字 / 系统列表
ousheng todo "第一个任务"        # 自动 ID + 全默认值
ousheng work update T-001 --status doing
ousheng view kanban
```

看板就是一个 git 仓：单人多窗口共用一个目录；多人把它 push 到 GitHub，每人开工前 `ousheng sync`。各系统代码仓**保持独立**，零侵入。

## 日常就三个动作

| 时机 | 命令 | 回答的问题 |
|---|---|---|
| 开工 | `ousheng context me` | 我该干什么？被谁阻塞？ |
| 干活 | `ousheng work update` / `progress report` / `evidence add` | 干到哪？凭什么说做完？ |
| 收工 | `ousheng converge` + `ousheng view kanban` | 全局还差什么？谁卡住了？ |

## 加人 / 加 AI 窗口

```bash
ousheng team add lisi --name 李四 --role ui --system datax-ui        # 同事
ousheng team add ui-dev --type agent --role ui --system datax-ui     # 同事的 AI 窗口
```

AI 窗口挂上 [adapter](adapters/README.md)（opencode / Claude Code）后自动遵守三时机：**启动必读**（sync 注入队列+阻塞+契约）、按需可查（MCP 工具）、**收尾必写**（converge 提醒）。`verified` 契约必须带证据（C1），breaking 必须人 ack（C2）—— 写入路径和收敛检查双层强制，AI 绕不过去。

<details>
<summary><b>为什么这么设计（点开）</b></summary>

一根绳，不替牛干活。OuSheng 只做两件事：让每个 worker 看到当前约定状态；强制破坏性变更人工背书。刻意不做：合约自动生成（AI 写的契约 AI 自己验收 = 幻觉闭环）、语义合并（自然语言歧义交机器裁决 = 黑盒）、Ontology 推理、中心验证管线（git 已带审计/冲突解决/分布式同步）。

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
| [多机多窗口使用指南](docs/多机多窗口使用指南.md) | 4 系统 × 4 机器 + 产品经理的完整拓扑、安装与日常协议 |
| [工程模型](docs/engineering-model.md) · [上下文协议](docs/context-protocol.md) · [存储约定](docs/state-store.md) | v0.3 规格 |
| [设计基石](docs/多人AI协作机制_方案基石.md) · [完整设计方案](docs/OuSheng_工程本体化改造方案_v0.3.md) | 为什么这样设计 |
| [v1 board](docs/v1-board.md) | 历史层参考 |

---

<div align="center">

**一根绳，一块板，接口不再靠缘分。**

[GitHub](https://github.com/georgewangchn/OuSheng) · MIT License

</div>
