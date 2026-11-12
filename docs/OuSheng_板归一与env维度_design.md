# OuSheng 板归一与 env 维度（2026-11-12，pm 终局拍板：一个看板）

pm 拍板：**ousheng 终局产品形态 = 一个看板**。不做两个板。备份仓 ≠ 第二块板。

## 1. 现状事实（2026-11-12 取证）

- 板分叉点：`13cc1ef2` 2026-11-05 17:24。之后 A 机侧 2810 commits、本地侧 68 commits。
- **ID 撞号实锤**：T-099/T-100/T-101 双板并存且内容不同（独立发号 7 天）。
- 两板同 pm、同项目（tide-datax）、同系统四件（data/ui/mlake/lhcp）、
  actor 表含 pm/pm-dev（同一人两座位形态）。
- 分叉非设计：A 机板 origin 被换成本机 bare（`/data/tide/kanban.git`）后未回推。

## 2. 判决（档案 §38）

1. **板数由治理定**：一个 pm、一个项目、一套系统 → 终态一块板。
2. **环境由字段定**（H3，`docs/OuSheng_上下文交接与凭据环境方案.md` §H3）：
   env 是数据维度不是拓扑分叉；两板并存时 env 恒为常量 = H3 白建——
   归一是 H3 兑现的前提。
3. **备份由仓库定**：`kanban-aliyun` 仓 = A 机板镜像备份，不是第二块板，
   归一完成后降级为归档或删除。
4. **归一顺序**：先归一、env 字段随迁打标（`env=aliyun` / `env=local`）——
   字段从第一天就有区分度。

## 3. H3 四件套（产品实现面）

1. `systems.yaml` 条目加可选 `envs: [dev, uat, pro, show]`（登记面，非配置分发）。
2. `WorkItem` 加可选 `env` 字段（部署目标环境；默认空=dev，不强制——诚实降级）。
3. `work list --env` 过滤；四路判决：`work show` / `context me` / `work list` 接入，
   `view kanban` 明文出局（env 是过滤维度不是列）。
4. deployment/release 证据约定记 `env + target_version`（evidence note 头部约定，
   非新字段——evidence 已有自由 note，约定格式即可，不造结构）。

**锁**：`TestWorkEnvFields`（四路）+ `TestWorkListEnvFilter` +
`TestSystemsEnvsRegistry`（严格解码：未知键拒绝，envs 可选）。

## 4. 归一迁移 runbook（一次性工程，动土即执行）

前提：gitee 私仓 `kanban-aliyun` 已建（pm），A 机板已推备份。

1. **盘点**：双侧 work ID 全集 diff → 撞号清单（已知 ≥ T-099/100/101）。
2. **基准**：A 机侧为基准（2810 commits 主体、生产侧）。本地侧 68 commits
   中撞号卡重编号（新 ID 段，如 T-9xx 或时间戳段），非撞号卡原 ID 合入。
3. **机械迁移**：脚本重映射 ID + `env=aliyun`/`env=local` 打标 + actors/systems
   同名合并（pm/pm-dev 保双座位=机器×身份，合法）。
4. **历史**：不做 git merge（同路径双侧改）；本地侧以新 commit 重放（卡的现状
   快照 + 迁移说明），原历史留 `kanban` 仓 main 可考。
5. **A 机 re-join**：origin 指回中心仓（gitee kanban-aliyun → 后或正名），
   `ousheng sync` 验证；四座位座位配置不动（.opencode/ousheng.json 本地）。
6. **验证**：`converge` 零 BLOCKER + `cruise report` 双 env 可过滤 + 抽样卡
   evidence locator 仍可解析。

## 5. 边界与死法（沿 §2.4 / §31 原判）

- 板仓永不分 env 分支（含归一前后）。
- `envs` 登记不做配置分发；web 不出现 env 管理界面。
- 归一迁移脚本 = 一次性工具，入 `scripts/`（§5 新剧本纪律：事故驱动——
  本次分叉即事故），不进产品二进制。
