# OuSheng 巡航模式设计方案（v0.1 draft）

> 起草：ousheng 座位，2026-11-01。需求：pm（实栈晚间/节假日无人值守自主优化）。
> 待 pm 拍板后实施。定位：**巡航不是新权限模型，是绳的循环形态**——门不变、时机不变、变的是驱动器与授权。

## 一、需求与实证

实栈开启系统自主优化：系统循环领活，需要其他系统就上绳（`depends_on`/blocked）继续等，对方交付后恢复；两系统冲突走协商或投票，**不再升级 pm**（无人值守）。场景：晚间/节假日（token 低谷）。要求：自主 ≠ 随意——严谨逻辑推演 + 反事实推演 + 第一性原理。

## 二、第一性原理：巡航 = 绳的原生形态

1. **绳天生为无人协作而生**：四时机（sync → 干活 → converge → handover）本就是循环体；converge 的 cycle/僵持/无主曝光本就是死锁检测；handover 本就是断点续航。巡航 = 把"人肉驱动的循环"换成"定时器驱动的循环"。
2. **无人值守的安全边界已经存在**：三道门在代码里不在对话里——agent 能做的一切都不需要人；需要人的一切（C2 breaking ack / design 拍板 / 生产权限）会被写路径硬拒。**巡航的正确语义：夜间遇到 human 门 = 挂起等天亮，不是绕过**。挂起清单就是晨间简报。
3. **投票的权力来源**：agent 之间的多数决只对**已被 human 授权的决策域**合法。授权载体 = **巡航宪章**（一张 agreed design 卡：范围/规则/时窗/预算）——用现有原语表达授权，零新 schema。投票**只解语义选择**（两条都合法的实现路线选哪条），**永不解门禁**（门在代码里，不可投票——三个 agent 投"我们不需要 human_ack"结构上不可能生效）。
4. **绳不代牛跑**：ousheng 不执行优化、不 spawn 大模型。它只回答三个问题：下一活是什么（cruise plan）、现在整体什么态（cruise status）、什么时候该停（停机条件）。执行循环者 = harness（opencode headless）或系统定时器，adapter 固化为资产。

## 三、机制（四薄层，零 schema 新增）

### 3.1 巡航宪章（cruise charter）——授权文书

约定 topic `cruise` 的 design 卡，PM 白天拍板（正门 decide，human 门不豁免），夜里生效。正文四节：

| 节 | 内容（示例，各项目自定） |
|---|---|
| 授权范围 | 巡航期可自建单类型：task/bug/test/notice ✅；feature/requirement ❌（动土先起 design，design 拍板是 human 门——结构上巡航期不可能合法开新 feature）；可自主决策的技术选择域 |
| 投票规则 | 法定人数 = **≥2 个不同系统**的注册座位（同系统多窗口计一票——防复本合谋）；多数决；**平票或触及范围边界 → 挂起等 human**；投票结果落 work item evidence（"round N：data/lhcp 2:0 选 B，依宪章 §2"）+ 必要时 notice 广播 |
| 时窗与预算 | 例 22:00–07:00、单夜 ≤N 轮、每座位 ≤M 轮、连续 idle ≥2 轮即停 |
| 红线 | 不动生产环境凭据（credentials 生产条目）；跨系统破坏性契约变更一律挂起；一切交付带证据（C1 照常） |

### 3.2 `ousheng cruise plan` —— 下一活（只读，agent 的驱动指令源）

扫描看板产出**结构化下一步**（不执行）：

```
$ ousheng cruise plan --actor data
1. resume: T-077 (doing, 我的) —— 上轮 handover 指针续跑
2. claim:  待领池 2 张（TASK-051 system=data ready 无主）
3. react:  T-070 依赖已关仍 blocked → 卧榻之侧的僵持，按宪章协商或挂起
4. new:    （可选）自提优化 bug/task —— 须过第一性推演，正文写推演过程
stop:     时窗外 / 预算尽 / 无活且连续 2 轮 idle / 触 human 门项已满 —— 巡航结束
```

优先序固定：**恢复 > 领活 > 反应 > 新提**（先干完自己的，再接别人的，最后才造新的——治"造活癖"）。无活输出 `cruise: idle`（驱动器据此停机/退避）。

### 3.3 `ousheng cruise status` —— 巡航态 + 晨间简报

```
$ ousheng cruise status
== 夜间摘要（22:00–06:52）==
交付 7 单（证据齐 7）｜新增 2 单（task×1 bug×1，无 feature ✓）｜投票 1 次（2:0）
挂起等 human 3 项：
  [拍板] design:stream-format（draft 齐轮次，等 decide）
  [C2]   T-083 breaking 契约（等 ack）
  [平票] T-086 缓存路线 1:1（等仲裁）
僵持 0 ｜环 0 ｜token 预算用 61%
```

数据全部已有（activity/converge/work）——纯投影，无新存储。

### 3.4 adapter cruise-runner —— opencode 无限循环的固化

pm 观察：opencode 能循环但要每次明说"循环等待"。固化形态（harness 各自适配，协议中立）：

- **协议段新增「巡航协议」节**（AGENTS.md 随 adapter install 分发）：领活优先序 / 等时不空转（blocked 就去领下一个，不轮询不 spin）/ 自提单推演义务 / 冲突先协商（§27 三级）后投票（宪章规则）/ 触线即挂起 / 每轮收尾 `handover save`。
- **`adapter install` 增可选资产 `scripts/cruise-loop.sh`**（不默认装）：`while ousheng cruise plan → opencode run --prompt "<plan 输出> + 巡航协议"`，间隔退避，停机条件透传。launchd/cron/手动皆可挂——**外层定时器驱动而非 plugin 自主 spawn**：可控（kill 即停）、可审计（每轮一个 session 事实）、可预算（轮数硬顶）。plugin 的 session.idle 只加一行"巡航态？下一活见 cruise plan"提示，不自主续命。

## 四、反事实推演（六条）

1. **夜间集体走偏（错误"优化"毁系统）？**——活动限于自系统仓；C1 证据照常；git 全程可回滚；宪章禁 feature；晨间 `cruise status` 一屏尽收。损失上限 = 一个夜间的提交，revert 即愈。
2. **投票被复本合谋（同系统三窗口刷票）？**——法定人数按**不同系统**计，同系统计一票；且投票只解语义选择，门禁在代码里不可投。
3. **无限循环烧钱？**——时窗/轮次/连续 idle 三闸，`cruise status` 报预算用量；驱动器外层可控可 kill。
4. **死锁环（A↔B 互等互不妥协）？**——converge cycle BLOCKER（既有）；§27 协商僵持 warning（既有）；巡航态僵持 = 双双 blocked 进晨间清单——**无人值守的正确终局是可见的等待，不是强行解**。
5. **夜间提示注入放大？**——T10 注入面只指针 + 内容是数据不是指令 + 拍板门在：注入最多诱导干蠢活（git 可回滚），无法越权（门在写路径）。
6. **agent 造活成瘾（为跑而跑）？**——优先序把 new 排最后；连续 idle 2 轮停机；自提单正文强制推演过程（审计可见"为什么这是值得做的事"）；feature 结构性不可能。

## 五、否决项与死法记录

| 否决 | 理由 |
|---|---|
| ousheng 内置 daemon/定时器 | 绳不代牛跑；执行循环归 harness/系统层；ousheng 只供 plan/status |
| plugin 内自主无限 spawn 下一 session | 失控面大（kill 不到、预算无顶、审计散）；外层脚本三闸可控 |
| 投票可决门禁（C2/拍板豁免） | 权力来源唯一：human 授权。多数 agent 意志不构成授权 |
| 巡航期自建 feature 单 | 动土先起 design + design 拍板是 human 门——无人期开 feature = 绕拍板门的新形态 |
| 新增"巡航状态"存储字段 | 一切巡航态 = activity/converge/handover 的投影；新存储 = 新漂移面 |

## 六、分期

| 期 | 内容 | 判据 |
|---|---|---|
| 一期 | 宪章约定（文档）+ `cruise plan/status` 两只读命令 + 协议段巡航节 + cruise-loop.sh 可选资产 + 实栈试跑一夜 | 本方案拍板 |
| 二期 | 投票收敛的机械辅助（round 计票提示）；web 面板巡航视图 | 一夜试跑后 pm 实用反馈 |
| 延后 | claude adapter runner；预算按 token 计（现按轮次） | 判据：非 opencode harness 巡航真实发生；token 计量需求实锤 |

## 七、待 pm 拍板

1. 宪章默认参数（时窗/轮顶/idle 退避）给一组产品默认值还是完全留白各项目自定？——倾向：文档给示例值，不进代码。
2. `cruise plan` 的 new 项（自提优化单）一期就开还是二期再开？——倾向：一期开但默认 `--no-new` 关闭，试跑稳了再默认开。
3. 投票落点：evidence 记录（本方案）够用，还是需要 notice 模板？——倾向：evidence 起步。
