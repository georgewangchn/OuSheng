# OuSheng 快速上手（单人 5 分钟）

OuSheng 解决：**多个 AI 编码窗口/多个协作者同时开发时，互相不知道对方在干什么** —— 接口改了没人知道、任务卡在谁的依赖上没人知道、做没做完没人知道。它不替你写代码，只做一根"牵引绳"：把工程上下文（谁/在哪/干什么/卡在哪）钉在 Git+YAML 上，人和 AI 开工前看一眼、收工后更新一下。

## 安装

```bash
go install ousheng/cmd/ousheng@latest   # 或从源码: go build -o /usr/local/bin/ousheng ./cmd/ousheng
```

## 一条命令开始（推荐）

```bash
mkdir myproj && cd myproj
ousheng setup
```

向导会问 3 个问题：项目名、你的用户名、系统列表。答完即可开工。

## 或手动 5 条（看清每一步）

```bash
ousheng init myproj && cd myproj   # 立项（名字默认=目录名）
ousheng me george --name George    # 我是谁 → 此后所有命令免 --actor
ousheng system add datax-ui        # 有哪些系统（--name 可选，默认=id）
ousheng todo "语义模型发布页"        # 第一个任务：自动 ID + 全默认值
ousheng view kanban                # 看板
```

`todo` 的默认值推导：ID 自动 `T-001` 递增 / type=task / assignee=me / role=dev（缺则自动建）/ accountable=唯一 human / assignment 隐式建立。多系统时才需要 `--system`。

## 日常工作循环（就 3 个动作）

```bash
ousheng context me                        # ① 开工：我的队列 + 阻塞
ousheng work update T-001 --status doing  #    干活（状态机: backlog→ready→doing→…）
ousheng progress report T-001 --value 0.7 #    汇报进度（basis 受控词表）
ousheng evidence add T-001 --type test_result --locator "test/x_test.go"   #    挂证据
ousheng work update T-001 --status done   # ③ 收工
ousheng converge                          #    收敛检查（阻塞/依赖环/契约门/进度警告）
```

所有状态是 Git 仓库里的 YAML（`.ousheng/`），`git log` 即审计，`git push` 即多机同步。

## 加人 / 加 AI 窗口

```bash
ousheng team add lisi --name 李四          # 第二个人（human）
ousheng me lisi                            # 在 lisi 的机器上执行 → 该机默认身份
ousheng todo "前端联调" --system datax-ui --assignee lisi
```

AI 编码窗口（opencode/Claude）= 一个 agent actor + 挂 session 启动/结束 hook，见 `adapters/opencode/`。接口有破坏性变更时挂 `contract: breaking: true`，必须 human `human_ack` 才能落盘 —— 防两个 AI 窗口互相改接口漂移。

## 命令全景

```
立项/身份   setup · init · me · system add · todo
日常       context me · work list/show/create/update/assign · bug report · progress report
证据/审计  evidence add/list · activity list
看板/收敛  view kanban|actor|system|version|project · converge · sync
```

详细参考：`docs/engineering-model.md`（工程模型）· `docs/state-store.md`（Git+YAML 存储约定）· `docs/context-protocol.md`（三时机协议）
