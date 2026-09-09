# OuSheng · 㸸绳

> 多人 + 多 AI 编码窗口协作时，**互相不知道对方在干什么** —— 这件事的解药。

一个人开 4 个 AI 窗口开发 4 个系统，或一个团队里人和 AI 混合干活，很快会撞上：

- 窗口 A 改了接口，窗口 B 还在按旧接口写 —— **契约漂移**
- 任务卡在谁的依赖上、为什么阻塞，全靠脑子记 —— **状态失明**
- 说"做完了"，但没有测试、没有 commit、没有验收 —— **完工无凭**

OuSheng 把"谁 / 在哪个系统 / 干什么 / 到哪一步 / 卡在哪 / 有什么证据"钉在 **Git + YAML** 上。一个独立小仓（上下文仓）就是全部基础设施：看板、任务、契约、审计都在里面，`git push/pull` 就是协作通讯。无服务器、无中心验证、无共享 Memory、与具体 AI 工具（Claude Code / opencode / Codex / 人）解耦。

"㸸"（òu）是河南方言里对牛的叫法，"绳"是牵引绳：套在 AI Coding 这头㸸鼻子上的绳 —— 最小接触点，只给方向，绝不替㸸使劲。

---

## 快速开始（3 分钟）

前置：Go 1.25+、系统 `git`。

```bash
git clone git@github.com:georgewangchn/OuSheng.git && cd OuSheng
go build -o /usr/local/bin/ousheng ./cmd/ousheng

mkdir myproj && cd myproj
ousheng setup                    # 交互式向导：项目名 / 你的名字 / 系统列表
ousheng todo "第一个任务"          # 自动 ID + 全默认值
ousheng work update T-001 --status doing
ousheng view kanban              # 看板
ousheng converge                 # 收敛检查
```

手动路径（看清每一步）：

```bash
ousheng init myproj && cd myproj  # 立项（名字默认=目录名）
ousheng me george --name George   # 我是谁 → 此后所有命令免 --actor
ousheng system add datax-ui       # 有哪些系统
ousheng todo "首页改版"             # 开工
```

日常只有三个动作：**开工 `context me` 看队列和阻塞 → 干活 `work update` / `progress report` / `evidence add` → 收工 `converge` + 看板**。

完整指南（加人、加 AI 窗口、契约门、多仓拓扑）：**[docs/quick-start.md](docs/quick-start.md)**

---

## 它是怎么工作的

**没有消息通讯，只有共享状态。** 看板住在一个独立的"上下文仓"里（第三个 git 仓），所有人 / AI 窗口各自克隆、读写它：

```
  你的代码仓 repo-a          同事的代码仓 repo-b        （各系统独立 git 仓，OuSheng 零侵入）
          │                         │
          │ 代码提交（照旧）           │
          ▼                         ▼
  ┌─────────────────── 上下文仓（看板在这里）───────────────────┐
  │  .ousheng/work/*.yaml    任务/状态/依赖/契约/证据  ← 通讯内容  │
  │  .ousheng/activity/      审计流（谁在何时改了什么）            │
  │  .ousheng/{systems,actors,assignments}  关系表               │
  └──────────────┬──────────────────────────────┬──────────────┘
        git push │                    git pull   │ git pull
                 ▼                               ▼
          A 的窗口（opencode）              B 的窗口 / 你
     session 启动自动 sync：拉取 + 索引 + "我的队列 + 阻塞"注入
```

- **单人多窗口**：4 个窗口 `--dir` 指向同一个上下文仓目录即可，连 remote 都不用
- **跨机多人**：上下文仓 push 到 GitHub / 公司裸仓，每人 clone + `ousheng sync`
- **代码仓与上下文仓的连接**：`ousheng repo set <system> <本地代码仓路径>` —— `git_commit` 证据会到正确的代码仓里验证
- **冲突不会写坏**：本机 `.ousheng.lock` 写串行 → CAS revision 拒绝逻辑并发 → git rebase 处理传输并发；每次变更一个 commit，`git log` 即审计

---

## 多人 / 多 AI 窗口

```bash
ousheng team add lisi --name 李四 --role ui --system datax-ui      # 加同事
ousheng team add ui-dev --type agent --role ui --system datax-ui   # 加 AI 窗口（自动绑负责 human）
```

AI 窗口挂上 adapter（[`adapters/`](adapters/README.md)）后遵守**三时机协议**：session 启动自动 sync（时机①）、开发中按需查上下文（时机②，MCP 15 工具）、session 结束 converge 收尾提醒（时机③）——低频事件驱动，不做每轮轮询。

**契约门（防漂移的核心）**：跨系统接口任务挂 `contract: breaking: true` 时，**必须 human `human_ack` 才能落盘** —— 两个 AI 窗口不可能互相改接口改出幻觉闭环；`verified` 契约必须有 typed evidence（C1）。两道门在写入路径和收敛检查上双层强制。

---

## 命令速查

```
立项/身份   setup · init · me · team add · system add · repo set · todo
日常       context me · work list/show/create/update/assign · bug report · progress report
证据/审计  evidence add/list · activity list
看板/收敛  view kanban|actor|system|version|project · converge · sync
索引       index rebuild/status（SQLite 派生索引，可随时删除重建）
```

收敛 `converge` 返回三态：`CONVERGED`（全部完成）/ `IN_PROGRESS`（正常推进）/ `BLOCKED`（显式阻塞、依赖环、悬空依赖、违反 C1/C2），另附警告（如 done 项进度未满）。

---

## 设计哲学：不做什么

AI Coding 是一头力气巨大的㸸。力气越大，越需要一根轻的绳。OuSheng **只做**：让每个 worker 知道当前合约状态，强制破坏性变更人工背书。它**刻意不做**：

- ✗ 合约自动生成（AI 写的合约 AI 自己验收 = 幻觉闭环）
- ✗ 语义合并（自然语言歧义交给机器裁决 = 黑盒）
- ✗ Ontology 推理、共享 Memory（引入中心化状态 = 要运维、要信任）
- ✗ 中心验证管线（git 本身已带审计 / 冲突解决 / 分布式同步）

五条设计原则与控制论映射见[设计基石](docs/多人AI协作机制_方案基石.md)。

---

## 架构

| 件 | 形态 | 职责 |
|---|---|---|
| core lib | `internal/` Go 包 | 全部业务逻辑：手写 schema 校验、CAS、双状态机、收敛、投影 |
| `ousheng` CLI | `cmd/ousheng/` | v0.3 主入口（onboarding + 日常 + 看板） |
| MCP server | `cmd/mcp/` | 15 工具（v0.3×12 + v1×3），import 同 core |
| `board` CLI | `cmd/board/` | v1 协作绳入口（cards/ 工作流，继续可用） |
| adapters | `adapters/` | Claude Code hook / opencode plugin / git evidence |
| 协议文档 | `docs/*-protocol.md` | 三时机、采样纪律、存储约定 |

铁律：**Git + YAML 是唯一事实源**；SQLite 仅派生索引（memory 与 sqlite 查询深度相等为硬约束，删库可重建）；WorkItem 七态执行状态机与 Contract 五态契约状态机分离；`revision`（CAS）≠ `target_version`（产品版本）。

---

## v1 协作绳（历史层，仍可用）

v0.3 之前的核心：一张卡一个契约（`cards/<id>.yaml`），五态状态机（proposed→agreed→live→verified→deprecated），`board write` CAS 写入。v0.3 的 WorkItem 内嵌 Contract 为子对象，`ousheng migrate schema` 可从 cards/ 幂等迁移（原文件不动）。v1 命令参考 / Card 模型 / 协作示例见 **[docs/v1-board.md](docs/v1-board.md)**。

---

## 文档

| 文档 | 内容 |
|---|---|
| [快速上手](docs/quick-start.md) | 5 分钟到日常循环，多人/多 AI/多仓拓扑 |
| [工程模型](docs/engineering-model.md) | v0.3 数据模型与五场景验收 |
| [上下文协议](docs/context-protocol.md) | 三时机查看协议 |
| [存储约定](docs/state-store.md) | Git+YAML canonical、S1-S7 硬约束 |
| [采样协议](docs/board-protocol.md) | v1 跨工具软纪律 |
| [v1 board 参考](docs/v1-board.md) | v1 命令 / Card 模型 / 协作示例 |
| [设计基石](docs/多人AI协作机制_方案基石.md) | 五原则、控制论映射、C1/C2/C3 边界 |
| [v0.3 设计方案](docs/OuSheng_工程本体化改造方案_v0.3.md) | 完整规格（59 节） |
| [适配器指南](adapters/README.md) | opencode plugin / Claude Code hook 安装 |

---

## GitHub

[github.com/georgewangchn/OuSheng](https://github.com/georgewangchn/OuSheng)
