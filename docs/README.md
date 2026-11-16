# docs 目录导读

> 日常运营只看一份：**[../操作手册.md](../操作手册.md)**（仓库根，全局唯一权威运营入口）。
> 其余文档按「查什么」分流；`archive/` 是已执行完毕的历史复盘、专题设计与早期规格（只读存档，git 历史即版本链）。

## 权威层（读改需谨慎，改动应有裁决）

| 文档 | 角色 |
|---|---|
| `设计裁决与推演记录.md` | **档案**：§1–§52 全部产品裁决与事故推演（AGENTS.md 铁律的理由层） |
| `铁律清单.md` | **一切产品改动的前置门**：五类 20 条铁律×自查问句×破坏实例；commit 必带 `铁律对照:` 行（§52） |
| `多人AI协作机制_方案基石.md` | 基石：五原则 + 卡点不变式，一切决策的最终裁判 |
| `OuSheng_工程本体化改造方案_v0.3.md` | 工程层权威方案（.ousheng 工作区/Actor/WorkItem/Evidence） |
| `OuSheng_共识层设计方案_v0.4.md` | 共识层（architecture/ + designs/ + design 命令族） |
| `OuSheng_巡航模式设计方案.md` | 巡航（含 §51 绳牛裁决修正节） |

## 协议层（agent 侧权威，被 adapter 分发引用，路径不可动）

- `board-protocol.md` · `context-protocol.md` · `state-store.md` · `engineering-model.md`

## 指南层

- `quick-start.md`（5 分钟单机入门，v0.3 命令现行有效）
- `多机多窗口使用指南.md`（join 协议教程镜像，被 adapter_test 一致性锁读取——路径锁定）

## 存档层 `archive/`

- 已执行专题设计：跨系统交接信号 / 测试交接信号 / 文档认领层 / 上下文交接与凭据 /
  板归一与 env 维度 / 巡航复盘修复
- 历史复盘：`OuSheng_复盘_2026-11-13至16_A级四座巡航.md`（档案 §50 全文依据）
- 早期规格：`superpowers/`（完整设计方案与阶段计划——v1 规则定义文件仍在用，路径经 AGENTS 引用）
- 历史版本与宣传：`v1-board.md` · DeepSeek 调研 · 公众号稿 ×2 · `双模式操作手册.md`
  （已并入根目录操作手册）· `reference-node.md`（F3 残余示例）
