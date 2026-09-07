# context-protocol — 上下文协议与查看时机（v0.3）

> Agent 不需要扫描整个仓库。`get_my_context()` 一步得到下一步行动的最小工程上下文。
> 对应方案 v0.3 §25/§26/§39，Phase 2 核心能力。

## 1. 查看时机协议（三时机）

看板查看是**低频、事件驱动**的，按人的思路决定何时看，**不做每 loop 轮询**：

| # | 时机 | 动作 | 工具 |
|---|------|------|------|
| ① | 每日 / session 启动 | git pull + 索引刷新 + 项目概览 + 我的上下文 | `ousheng sync [--actor ID]`（adapter 自动） |
| ② | 出现 bug / 问题 / 被阻塞 | 查看相关 WorkItem、依赖、blockers | `ousheng context me` / `query_work_items` / `get_system_context`（agent 主动） |
| ③ | 当前任务结束 | 更新状态 / 汇报进度 / 补证据，然后再看板 | `ousheng work update` / `progress report` / `evidence add` → `converge` + `context me`（adapter 提醒） |

原则：**看板频率不高；每次查看都有明确理由。** 高频轮询 = 上下文成本失控 =
违背 OuSheng 目标函数（协作价值 = 降协调错误 − 上下文成本 − 维护成本 − 同步复杂度 − 介入程度）。

## 2. Progressive Context Disclosure（三层展开）

```
第一层  get_my_context        身份/角色/系统/版本/进行中工作/blockers（≤1.5KB）
          ↓ 需要细节时
第二层  get_work_item <id>     单项全量 + 直接依赖摘要
          ↓ 需要证据时
第三层  get_evidence <id>      证据明细
          ↓ 需要系统关系时
        get_system_context     责任人/执行者/active work/open bugs
```

默认**不**注入：整个 git log、所有 closed WorkItem、全部 evidence、
其他 actor 的全部任务。

## 3. 第一层返回形状

```json
{
  "actor": "backend-agent",
  "actor_type": "agent",
  "responsible_human": "zhangsan",
  "roles": ["backend"],
  "systems": ["datax-backend"],
  "target_version": "v2.0",
  "active_work": [
    {"id": "FEAT-CDC-001", "title": "CDC 增量同步", "status": "doing", "progress_reported": 0.7}
  ],
  "blockers": ["K8S-003"]
}
```

验收（§47）：backend-agent 无需扫描项目即知——我是谁 / 最终负责人张三 /
角色 Java 后端 / 负责 DataX Backend / 目标 v2.0 / 正在做 CDC / 当前有 BUG-017 /
阻塞来自 K8S-003。

## 4. MCP 工具清单（§38 第一批）

| 工具 | 层 | 说明 |
|---|---|---|
| `get_my_context` | 1 | 我的工程上下文（描述内嵌查看时机提示） |
| `get_actor_context` | 1 | Actor 视图（human：下属 agents、问责系统） |
| `get_system_context` | 1 | System 视图 |
| `get_work_item` | 2 | 单项全量 + 依赖摘要 |
| `query_work_items` | 2 | 过滤查询 |
| `create_work_item` / `update_work_item` / `assign_work_item` | 写 | CAS on revision |
| `report_bug` | 写 | type=bug + detected_by |
| `report_progress` | 写 | reported state，非 fact |
| `add_evidence` / `get_evidence` | 写/3 | typed evidence；git_commit 经 adapter 验证 |
| v1: `read_board` / `write_board` / `converge` | 兼容 | cards/ 工作流不变 |

## 5. Agent 行为守则（写给 agent 的软纪律）

1. session 启动：先 `get_my_context`，再开始工作；
2. 写代码遇阻（bug / 依赖缺失 / 被别人破坏）：`query_work_items` + `get_system_context`，
   不要闷头猜；
3. 完成一个任务：`update_work_item`（status）+ `report_progress`（若有意义）+
   `add_evidence`（若有验证物），然后 `get_my_context` 重新对齐；
4. 汇报进度必须带 basis；没有可靠依据就报 `manual`，不要编造精度；
5. 你是 agent：`verified`/`breaking` 的最终裁定属于 `accountable_human`，不要自审自发；
6. 遇到 CAS conflict：重新读、合并意图、重写——不要覆盖别人的 revision。
