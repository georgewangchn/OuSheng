# OuSheng · 㸸绳

**多个 AI 编码窗口一起干活时，互相不知道对方在干什么。** 接口改了没人知道、任务卡在谁的依赖上没人知道、说"做完了"却没证据。

OuSheng 把「谁 / 在哪 / 干什么 / 卡在哪」钉在一个 git 仓上。无服务器、无配置中心，`git push/pull` 就是协作。

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

前置：Go 1.25+、`git`。

```bash
git clone git@github.com:georgewangchn/OuSheng.git && cd OuSheng
go build -o /usr/local/bin/ousheng ./cmd/ousheng

mkdir myproj && cd myproj
ousheng setup                  # 交互式：项目名 / 你的名字 / 系统列表
ousheng todo "第一个任务"        # 自动 ID + 全默认值
ousheng work update T-001 --status doing
ousheng view kanban
```

单人开多窗口 → 各窗口 `--dir` 指向同一个看板目录即可；多人 → 看板仓 push 到 GitHub，每人 clone 后开工前 `ousheng sync`。

## 日常就三个动作

| 时机 | 命令 |
|---|---|
| 开工 | `ousheng context me` —— 我的队列 + 阻塞 |
| 干活 | `ousheng work update` / `progress report` / `evidence add` |
| 收工 | `ousheng converge` + `ousheng view kanban` |

## 加人 / 加 AI 窗口

```bash
ousheng team add lisi --name 李四 --role ui --system datax-ui        # 同事
ousheng team add ui-dev --type agent --role ui --system datax-ui     # AI 窗口
```

AI 窗口挂上 [adapter](adapters/README.md)（opencode / Claude Code）后自动遵守三时机：启动 sync、按需查、收尾 converge。破坏性接口变更（`contract: breaking`）必须 human 确认才落盘 —— 两个 AI 窗口改不出互相漂移的接口。

## 原则

一根绳，不替牛干活：只让每个 worker 看到当前状态 + 强制破坏性变更人工背书。刻意不做合约自动生成、语义合并、Ontology 推理、中心验证。Git + YAML 是唯一事实源，SQLite 仅为可重建的派生索引。

## 文档

| | |
|---|---|
| [快速上手](docs/quick-start.md) | 日常循环 / 多人 / 多仓拓扑 / 契约门 |
| [工作原理](docs/quick-start.md#它是怎么工作的) | 看板仓通讯模型 |
| [工程模型](docs/engineering-model.md) · [上下文协议](docs/context-protocol.md) · [存储约定](docs/state-store.md) | v0.3 规格 |
| [设计基石](docs/多人AI协作机制_方案基石.md) · [完整设计方案](docs/OuSheng_工程本体化改造方案_v0.3.md) | 为什么这样设计 |
| [v1 board](docs/v1-board.md) | 历史层（契约卡协作绳，仍可用） |

---

[github.com/georgewangchn/OuSheng](https://github.com/georgewangchn/OuSheng) · MIT
