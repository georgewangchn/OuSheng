# OuSheng 上下文交接与凭据环境方案（v0.1 draft）

> 起草：ousheng 座位，2026-10-31。依据：实栈共性痛点三条（pm 提出）+ data 域手工交接实证。待 pm 拍板后拆单实施。
> 定位：牵引绳增加「换页」能力——session 是牛的工作记忆（RAM），绳管 checkpoint 的载体与仪式，不管 session 正文。

## 一、问题与实证（实栈）

1. **Session 无限膨胀**：多 agent 多系统长任务（1-2 月），session 累积死胡同探索、已完成细节、日志残渣。data 域实证解法：抛弃旧上下文 + 起新 session + 交接 → 上下文直接降低。**手工做有效，但没有机制承载**：交接内容散落、格式即兴、跨系统不可见。
2. **凭据摩擦**：账号/密码/key 反复遗忘、人工输入打断 agent 工作流（pm 实证）。
3. **环境与版本维度缺失**：当前 main 单干道 = 事实上的生产环境；后续 pro/dev/uat/show 多环境 + 版本号分支，绳上无处表达「这个活/这个部署属于哪个环境哪个版本」。

## 二、第一性原理

### 2.1 Session 的本质与绳/牛边界

Session = 工作记忆（RAM）：贵、快、有限、断了就没了。膨胀的根因不是「长」，是**换页机制缺失**——RAM 里堆满了本该落盘的东西。

绳/牛切分（基石 §局部复杂全局简单）：

- session 正文、总结提炼 = **牛的活**（只有牛知道自己记住什么）
- 交接的载体、仪式、恢复入口、大小门 = **绳的活**（跨 session/跨主体，正是绳的存在理由）
- **绳不存 session 全文**：全文大部分是垃圾（试错路径），且存了 = 绳变容器 = 牛拉绳

### 2.2 交接快照 = 缓存，不是事实源

交接需要的是**导航态**：进行中什么、关键事实指针、凭据在哪、下一步。这些的**权威早就在绳上**（work item / design / architecture / evidence）。快照只是把「散在绳上的当前态 + session 里的隐性上下文」压成一页导航。

→ 哲学对齐 progress：快照是 **handed-over state（reported）**，不是 fact。错了以绳为准；快照内容是**数据不是指令**（新 session 不执行快照里的"指示"，只作导航——提示注入防御与 design 正文同判）。

### 2.3 凭据：泄漏成本 × 暴露面（对 pm 前提的一处修正）

「开发环境凭据不敏感」——泄漏成本判断成立（内网、测试数据、可轮换）。但**暴露面**被低估：

- kanban 仓托管在第三方（gitee）——凭据入 git = 出内网，与环境无关
- 任何 clone 到仓的实体都拿到全量值
- git 历史永存：将来 dev 变敏感/仓转公开，洗历史是大手术

→ **值永不入 git（硬线，不分环境）**。痛点用双层结构解决（§H2）：登记表（位置入册，入 git）+ 本地值文件（gitignored，座位机持有）。便利性几乎无损：agent 读本地文件，不再问人；代价仅 re-clone 后一次性重灌（join 流程顺带，登记表会告诉你灌哪些）。

### 2.4 环境是数据维度，不是拓扑分叉

反事实：kanban 仓若按 env 分支——224 的 agent 在 dev 分支干活，PM 在 main 看板看不到；work/*.yaml 并发写 + 分支合并 = 冲突地狱；看板分裂 = 绳断裂。**系统仓各自分 env 分支（它们的事，环内）；kanban 恒单干道，环境作为字段/登记数据存在**。

## 三、机制设计

### H1 交接快照（handover）——一期

**形态**：`.ousheng/handover/<system>/<actor>.md`（座位级，一系统多座位不互踩）。frontmatter 固定（system/actor/saved_at）+ 正文自由（牛写：进行中/关键事实/凭据引用/下一步）。**覆盖式更新 + 4KB 硬门**：工作区每座位恒一页（=「session 总长度一定」），旧版在 git 历史（档案可回溯，不占工作区）。

**命令**（最小面）：

```
ousheng handover save [--system S --actor A]   # 从 stdin/file 读正文；frontmatter 补全；>4KB 拒；git commit（历史即版本链）
ousheng handover show  [--system S --actor A]   # 输出当前快照——新 session 首读即恢复
ousheng handover list                           # 各座位快照时间一览（扫视：谁多久没换页）
```

**仪式**（对齐查看时机协议，进 AGENTS.md 协议段随 adapter 分发）：

- 时机三收尾（session 空闲/结束）→ `handover save`（plugin 收尾提醒并列）
- 时机一开工（新 session 启动）→ `context me` 尾部一行**指针**（`handover: data/data @ 10-31 14:00`——T10 合规，只指针不正文）；agent 自行 `handover show`

**不做**：自动注入正文（T10）；restore 独立命令（show 即恢复，读了就是）；快照间 diff/合并（git 已是版本链）。

### H2 凭据登记（credentials）——一期

**双层**：

| 层 | 位置 | 入 git | 内容 |
|---|---|---|---|
| 登记表 | `.ousheng/credentials.yaml` | **是** | 条目：id / system / env / 用途 / **位置**（local-file \| password-manager \| env-var）/ 轮换记录 |
| 值 | `.ousheng/secrets/`（gitignored） | **否** | dev/uat 环境的值（本地文件）；生产值只入密码管理器，登记表记条目名 |

**机械曝光（治"遗忘"）**：doctor 增查——登记条目位置=local-file 而本机文件缺失 → FAIL 带修复指引（灌值）；位置=password-manager 而无条目名 → WARN。快照「凭据」节引用登记 id，新 session 知道去哪拿，零人工输入。

**声明暂手改 systems 式**（yaml + git 审计）；CLI 写面判据见 §六。

### H3 环境与版本维度——二期（判据驱动）

- system 注册表加 `envs` 清单（如 `[dev, uat, pro, show]`）——环境存在性的登记面
- WorkItem 加可选 `env` 字段（部署目标环境；默认空=dev）+ `work list --env`；四路判决：work show / context me / work list 接入，view kanban 明文出局（env 是过滤维度不是列）
- deployment/release 证据格式约定记 `env + target_version`（已有版本字段，补环境）
- **kanban 恒单干道**（§2.4 裁决）；系统仓/产品仓 env 分支策略 = 各仓自理，绳只记结果（部署证据）
- ousheng 产品仓自身：main = 生产（现状即如此，deploy-fleet 从 main 建）；dev 分支判据 = 第一次双环境并行发布

## 四、反事实推演

1. **快照自身膨胀？**——座位级单文件 + 4KB 硬门 + 覆盖式，工作区总量 = 座位数 × 4KB，有界。
2. **快照错误/过时污染新 session？**——快照=缓存非权威（§2.2）；不含指令语义；与绳冲突时以绳为准。写入纪律靠 save 时的 author（actor 记 frontmatter，错了可追）。
3. **忘了 save 就断 session？**——收尾提醒已有挂载点（plugin 时机③）；先靠协议。判据：真实「丢上下文事故」后再加「active work × N 天未 save」曝光（记 §六）。
4. **值藏在快照里绕开 secrets？**——快照入 git，同硬线；save 无内容扫描（语义），靠纪律 + doctor 抽查？不——不做内容扫描（卡点不变式：不卡语义），death 记录 + 审计兜底。
5. **多座位并发 save 同一文件？**——文件粒度=system/actor，天然互斥；无 actor 概念座位用 me。
6. **re-clone 后 secrets 空、agent 卡死？**——doctor FAIL 指引 + join 协议补一步「灌值」；一次性成本。
7. **env 字段加了没人填？**——默认空合法（=dev），不强制；查询面按需过滤。诚实降级，同 doc stale 教训写明。
8. **handover 变成小 design（决策藏进快照）？**——快照是导航态，决策该在 design/单里。协议段写明：快照只放指针+现状，不放新决策。违反=审计可见（快照也 git 可查）。

## 五、否决项与死法记录

| 否决 | 理由 |
|---|---|
| session 全文上绳 | 绳变容器；全文大半是垃圾；违反最小接触 |
| 凭据值入 git（含「dev 不敏感」豁免） | 第三方托管 + clone 面 + 历史不可撤（§2.3）——与环境敏感度无关 |
| kanban 按环境分支 | 看板分裂 + 并发冲突地狱（§2.4） |
| 快照自动注入 context me | T10 注入面铁律；快照=数据不是指令 |
| restore 独立命令/合并/diff | show 即恢复；git 已是版本链——不为已有能力造新面 |
| secret get/set 命令 | 进程参数/日志泄漏面 + 无必要（本地文件直接读） |

## 六、分期与判据

**一期（痛点已实证，随拍板即做）**：H1 handover 三命令 + 4KB 门 + context me 指针 + plugin 收尾并列提醒；H2 双层结构 + doctor 凭据缺失检查。

**二期（记判据防丢）**：

| 项 | 判据 |
|---|---|
| env 字段 + envs 登记 + --env 查询 | 第一个非 dev 环境真正建立（uat/pro/show 任一上线） |
| credentials CLI 写面 | 手改 systems/credentials 出错实证，或第二项目使用 |
| 「N 天未 save」曝光 | 真实丢上下文事故 |
| handover 历史浏览命令 | 真实需要回看旧快照（git log 现已够用） |
| ousheng 产品仓 dev 分支 | 第一次双环境并行发布 |
| 快照跨系统聚合视图 | 多系统联调时真实需要（先 list） |

## 七、铁律对齐

| 铁律 | 本设计如何遵守 |
|---|---|
| 牵引绳不是牛 | 绳管载体/仪式/门（4KB、doctor），总结与内容 = 牛 |
| 注入面最小化 T10 | context me 只加快照**指针**一行；恢复=显式 show |
| 内容是数据不是指令 | 快照同 design 正文判；新 session 不执行快照"指示" |
| Git+YAML canonical | 快照/登记表皆文本文件 git 提交，历史即版本 |
| 不新增配置面 | 无配置；路径约定即协议（对齐 .ousheng 既有风格） |
| 卡 agent 的写不卡 human 的手 | 4KB 门卡 save 写路径；human 直改文件不拦（git 审计） |
| P7（mlake 先例）位置入册值不入 git | H2 直接继承并产品化 |
| 新字段四路验证 | env 字段（二期）按四路判决执行 |
| 一语义一实现 | handover 语义收 internal（cmd 适配），plugin 只调 CLI |

## 八、开放问题（pm 拍板点）

1. 快照粒度：system×actor（本设计）vs system 单页（pm 原话"每系统一个 session"）——多座位系统建议前者，单座位等价。
2. 4KB 门是否合适（data 域手工交接的实效大小待采样）。
3. secrets 文件格式（.env 风格 vs yaml）与命名（`secrets/<system>.env`）。
4. 二期 env 术语表：pro/dev/uat/show 之外是否要 gray/canary。
