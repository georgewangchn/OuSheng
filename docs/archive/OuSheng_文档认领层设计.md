# OuSheng 文档认领层设计

> 通用机制：任何使用 ousheng 的项目都会积累文档，且文档有多种生命周期。产品必须能机械地回答"这篇文档归谁、它还作不作数"，而不是靠人记住。
>
> 本设计不引入新实体、不新增配置面、不硬编码任何项目的目录结构。

## 一、问题

任何长期使用 ousheng 的项目都会遇到同一件事：文档数随工作时间增长，而没有任何机制知道"哪一篇是现行的、它归谁"。

典型症状三条：

1. **版本靠文件名**——`_v2`/`_v4` 反复出现，因为没有一个权威位置说"现行是哪个"
2. **一物多份**——同一篇文档在多个目录各存一份，逐字节相同，无人知道哪份是正本
3. **归档即遗忘**——旧版本扔进 `archive/` 就不再被任何机制看见

根因不是"文档太多"。是**认领缺失**：写下这篇文档的动作零成本，且不需要回答"它属于哪个知识单元"。

ousheng 的定位是牵引绳——不做文档容器。所以它不该存内容，但**必须能回答认领关系**。这正是它现在缺的那一层。

## 二、第一性原理：三条不变量

### D1 单一认领

受管辖的文档，**有且只有一个认领主体**。

主体是 ousheng 已有实体之一：Design topic / WorkItem / System / Contract。认领不等于占有——文件仍在 git，认领等于"我知道它归谁"。

### D2 角色由认领来源决定，不存为字段

文档角色不需要单独存储，它由**认领机制**唯一决定：

| 认领机制 | 角色 | 陈旧可判 | 理由 |
|---|---|---|---|
| `design.doc` 字段 | 决策 | 是（对该 topic 的 done 单） | 方案应随实现演进 |
| `.ousheng/architecture/<sys>.md`（约定路径） | 现状 | 是（对该系统的 done 单） | 现状卡应随系统演进 |
| `cards/*.yaml`（约定路径） | 契约 | 否 | 已有 `human_ack` 版本机制 |
| work item 的 `type: document` 证据 locator | 证据 | 否 | 已是完成时快照，本就不该再变 |

这条不变量砍掉了一整个字段维度：不需要 `role: decision|status|...`，因为角色的唯一合法来源就是"谁认领了它"。

### D3 未知即曝光

在**受监控区域**内，未被认领的 `.md` 是孤儿，必须报出。

关键在于"受监控区域"怎么定义。硬编码目录清单是错解——每个项目目录结构不同，写死就是把某个项目的习惯变成产品约束。

**正解：受监控区域由认领关系自动推导。**

```
roots = { dirname(claim.path) : claim ∈ 全部认领 }
```

- 某目录树里有**至少一篇**已认领文档 → 该树自动纳入监控
- 某目录树里**零**认领 → 自动豁免（参考区、产品材料区天然免打扰）
- 零配置

推论：**豁免不需要配置，它是"没有认领"的自然结果。** 这让产品不必知道任何项目的目录名。

## 三、两条边界

### 边界 1：结构性文件不参与孤儿判定

`README.md` 与 `INDEX.md` 是目录自述，不属于任何知识单元，豁免。

这是**唯一**的硬编码文件名白名单，因为它与项目无关——任何项目的目录自述都叫这两个名字。

### 边界 2：越界指针允许，但降级

指针可指向工作区外（`architecture/mlake.md` 已有先例指向外部仓）。但：

- 越界只能**读**，ousheng 永不写工作区外路径
- 越界文件拿不到 git 首次可判时间 → **不参与陈旧判定**
- `doc` 字段本身要求**相对路径**（禁绝对路径），保证跨机器可移植；越界用 `../` 表达

## 四、机制：一个字段，三个查询

### 4.1 `DesignDoc.Doc`

```yaml
doc: docs/design/某方案.md
```

- 一个 topic 至多一篇现行方案文档
- `design decide` 时若 `doc` 非空，文件必须存在，否则拒绝（fail-closed：不能拍板一个指向空的方案）
- 空 = 该 topic 不需要外部正本（卡片自身即全文），合法

**为什么不给 WorkItem 加同名字段**：WorkItem 已有 `evidence`（`type: document` + `locator`），那本就是认领。再加字段会撞四路验证、注入面禁自由文本、8192 字节门三条铁律。

### 4.2 `ousheng doc map` — 户口本

```
PATH                                        ROLE        OWNER                UPDATED
docs/design/分层分级_技术实现方案.md          decision    layered-data-dev     2026-10-16
docs/design/数据面与OM关系_开发方案.md        decision    om-data-plane        2026-10-24
docs/acceptance/T-037-ui-自测报告.md          evidence    T-037                2026-10-20
.ousheng/architecture/data.md                status      data                 2026-10-29
cards/mlake-lakehouse-endpoints.yaml        contract    mlake-lakehouse-endpoints  —
docs/design/MQ评估提案.md                     —           —（无认领）           —
```

只读。未认领的行显式留空，不给假归属。

### 4.3 `ousheng doc check` — 审计

```
$ ousheng doc check
doc orphan: docs/design/MQ接口统一架构与可观测性-评估提案.md
doc stale: design layered-data-dev — 3 done deliveries after doc update (latest T-034 at 2026-10-20)
```

两类信号，分工明确：

| 信号 | 回答 | 判定 | 覆盖面 |
|---|---|---|---|
| `doc orphan` | 这篇归谁 | 集合（有/无认领） | 不依赖任何申报，全量 |
| `doc stale` | 还作不作数 | 时间（文档提交 vs 交付） | 仅限有认领且角色可判者 |

**自清除**：补认领消前者，更新文档提交消后者。没人需要"关掉"告警，动作本身就是清除。

### 4.4 `ousheng doc census` — 未纳管区域普查

D3 有一个天然盲区：**零认领的目录树永不被扫**。若有人在 `docs/newstuff/` 里堆 50 篇，永远不可见。

普查解决它——但不是报 50 个孤儿（firehose），而是报**区域**：

```
$ ousheng doc census
unclaimed region: docs/newstuff/  — 50 .md files, 0 claims
```

判据用 `git ls-files '*.md'`（天然遵守 `.gitignore`，不需要另做排除），按目录聚合，`≥3 篇且 0 认领` 报为未纳管区域。

这是**目录级**信号，一条就能推动人处理一整片，不会淹没在文件级噪音里。

## 五、与既有架构的关系

| 铁律 | 本设计如何遵守 |
|---|---|
| **不新增实体** | 无新实体。`doc` 是 Design 的字段，认领从既有数据派生 |
| **不加配置面** | 零配置。扫描区域由认领关系推导，豁免由"无认领"推导 |
| **注入面最小化（T10）** | `doc` **不进** `context me`。查询走 `doc map`，与 `Knowledge = architecture 文件名` 同列，按需自取 |
| **8192 门** | 不碰 WorkItem，不触发 |
| **四路验证** | `doc` 属 Design 而非 WorkItem，不适用该条；但需补一条反向锁：`doc` 值不得出现在注入面 |
| **一语义一实现** | 语义收 `internal/docs`（纯函数），CLI / converge / MCP 三入口只做参数适配 |
| **S5 等价** | `doc` 与认领关系不进 `Index` 查询面，只在 `Knowledge` 显式携带（与 `ArchitectureUpdated` 同机制） |
| **失败关闭** | decide 时 doc 非空但文件不存在 → 拒绝，不静默降级 |

## 六、实现落点

| 层 | 文件 | 内容 |
|---|---|---|
| 模型 | `internal/model/design.go` | `Doc` 字段 + 相对路径校验 |
| 语义 | `internal/docs/docs.go`（新） | Claim / Ref / Roots / Orphans / Exempt / Stale 纯函数 |
| 状态 | `internal/state/gityaml/docs.go`（新） | `TrackedMarkdown()`（git ls-files）、`DocUpdated(paths)`（git log 批量取时间） |
| 审计 | `internal/converge/converge.go` | `Knowledge` 加 `DocRefs`；converge 报摘要（count + 前 N 条），明细走 `doc check` |
| 入口 | `cmd/ousheng/doc_cmd.go`（新） | `doc map` / `doc check` / `doc census` |
| 入口 | `cmd/ousheng/design_cmd.go` | `design list` 加 DOC 列；decide 加存在性校验 |
| 接线 | `cmd/ousheng/main.go` | 注册命令；构建 `Knowledge.DocRefs` |

## 七、反事实推演

**1. 若没人写 `doc` 字段？**
认领集退化为只剩 evidence locator。`docs/design/` 里零认领 → 该树不被监控 → 方案类文档全静默。
**对策**：`doc census` 会报 `docs/design/ — 5 .md files, 0 claims` 为未纳管区域。零认领的树不是不可见，是降级为"区域级告警"。机制不空转。

**2. 首次运行会 firehose 吗？**
以 tide 实测：`docs/design` 6 篇中 4 篇有认领 → 1 个孤儿；`docs/acceptance` 16 篇中 10 篇有认领 → 若干孤儿。量级 5–10 条，可处理。
若某项目首次跑出 50 条 → converge 只报 count，明细走 `doc check`，不刷屏。

**3. 若有人用 locator 洗白一堆垃圾？**
给某个 done 单加几十条 document 证据即可。**这是真风险。**
缓解：弱认领必须 ① 能解析到真实文件 ② 引用主体状态合法。仍不完美——done 的单可以指向任何东西。
彻底解法需让"单与文档的引用"本身成为需拍板的结构关系，超出本层范围，**记为已知缺口**。

**4. 若"移进 archive"比"认领"容易？**
归档是 `mv`，认领要建 topic + 写字段 + 人拍板。理性人选归档，archive 长成第二个垃圾场。
**对策**：archive 目录树若零认领，按 D3 自动豁免；但 `doc census` 会把它当"未纳管区域"报出来。归档不是免罪，只是降级为区域信号。

**5. 若 `doc` 指向的文档长期不更新？**
`doc stale` 触发直到更新。但若该 topic 的 `related_items` 为空，stale 永不触发——**覆盖面依赖 `related_items` 申报**，这是诚实的降级，写在文档里，不假装全覆盖。

## 八、与既有实栈方案的差异（重要）

本设计**取代**了 `doc-lifecycle` 方案中三处 tide 专属实现：

| 原方案 | 本设计 | 理由 |
|---|---|---|
| `doc orphans --dir <显式目录>` | 扫描根由认领自动推导 | 显式 `--dir` 要求人每次记住参数，产品不该带项目习惯 |
| 默认扫描 = 管辖域 + **写死镜像白名单**（`docs/infocards/**`） | 删掉白名单，改为 D3 自动推导 + `census` 区域普查 | 写死目录名 = 把某个项目的临时结构变成产品约束 |
| 新建 `ousheng doc list` / `doc orphans` 两条命令 | `doc map` / `doc check` / `doc census` 三条命令 | `check` 与 `census` 是两种信号，混在一条命令里会让人分不清"这篇无主"与"这片无人管" |

## 九、待验证

1. `DocUpdated` 批量取时间的性能：`git log --format='C %cI' --name-only` 单次遍历 vs 逐文件 `git log -1`。当前文档量级（<50）单次遍历即可，需实测
2. `doc census` 的阈值 3 篇是否合适（1 篇就报会吵，10 篇才报会漏）
3. `doc stale` 的比较基准：`done` 单最新证据时间 vs 最新 `git_commit` 证据时间
4. 越界指针（`../`）在 `doc map` 中如何标注才能让人一眼看出"这篇在别的仓"
