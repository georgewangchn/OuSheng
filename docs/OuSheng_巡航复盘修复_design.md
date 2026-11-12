# OuSheng 巡航复盘修复（2026-11-12，A 机双巡航复盘裁决）

来源：A 机（阿里云）11-11 晚→11-12 晨两次巡航实栈复盘。取证：板仓 git 全量、
opencode.db 236 session、四系统仓 dev 提交、驱动器脚本三代（loop/loop2/loop3）。
裁决全文见档案 §37；本文是执行设计。

## 复盘结论（一句话版）

1. **BUG-136 身份缺陷**：`me` 兜底静默错配（单槽位 × 四座位共享板仓）。
2. **token 结构性烧钱**：236 session / 364M cache read，四因叠加——续代链断
   （233 轮全换代）、值守分层失效（flash 轮=0）、零 delta 维持轮不停、重上下文座位。
3. **meta 根因 = 驱动器分叉**：产品 canonical cruise-loop.sh 的 attach 补记 /
   /tmp 隔离 / 三闸日志被实栈手搓 fork（loop2/loop3）丢弃，且驱动日志零留存。
4. **session 不可读**：233 个标题全同 + 代号 ID + 黑话正文，中间层投影缺位。

## 修复设计

### F1 身份解析收紧（BUG-136 方案 A 收紧版）

**规则**：`machine.ResolveActor` 的 `me` 兜底**仅当 CWD == workspace（同目录）时生效**。
CWD 无座位配置且 `--dir` 指向别处 → 直接报 `--actor required`（fail-loud）。

- 覆盖形态②（CWD=/tmp + --dir 板仓 → 静默取 me=data）：灭。
- 保留形态（PM 单机 cd 板仓：CWD==workspace → me 兜底合法）：不伤。
- CLI 与 MCP 同链单点修复（actingActor / mcp.resolveActor 都走 ResolveActor）。

**读命令 --actor 语义判决**：不加装饰 flag（work list/show 无身份消费，加 flag =
界面腐化）。`context me` 已有 flag 且空身份已 fail-loud；收紧后全链自动生效。
协议文本措辞改为「写命令一律显式 --actor；读命令座位自动解析、多座位机建议显式」。

**锁**：`TestResolveActorMeFallbackOnlyColocated`（internal/machine）+
`TestDirBoardMeFallbackRejected`（cmd 层，/tmp CWD + --dir 板仓 + me=data → 写命令拒）。

### F2 巡航简报轮摘要（L2）

`cruise report` 增「座位轮摘要」节：activity 按 actor × 时间聚簇（间隔 >15min 分轮），
每座位一行：轮数、触单集、状态变迁数、evidence 数、**纯记账轮数**（该轮无
created/status_changed/assigned/progress_reported，只有 evidence_added——零 delta
维持轮的直接曝光）。`--json` 同步。数据源 = activity（审计面），不新增存储。

**锁**：`TestCruiseReport` 扩展（轮摘要行存在 + 纯记账轮计数 + JSON 面）。

### F3 web 驾驶舱补座位交接快照（L3 补口）

/cruise 已有 plan/status/presence。补「座位快照」节：`handover list` + 各座位
快照正文渲染（读面，exec CLI 复用，无新写路径）。PM 侧「这座位最后说了什么人话」
一屏可见。边界：不出现 handover 编辑按钮（web 是 PM 面，档案 §31 死法记录）。

**锁**：`TestWebCruiseHandoverSection`。

### F4 驱动器归一（L0 + L1，资产面）

- **L0**：canonical `adapters/opencode/cruise-loop.sh` 的 round prompt 首行注入
  `round N | plan 活单 ID | 模式`（session 自动 title 由此可区分）；
  `adapters/opencode/agents.md` 协议段增「handover 首行 = 一句话人话摘要」。
- **L1 归一**：实栈废 fork，重跑 `adapter install` 取回 canonical（含 attach 补记、
  /tmp 按座位+PID 隔离、三闸日志）。**驱动器 = 协议资产，改动走产品仓**——
  实栈改驱动器 = 分叉 = 复发。
- **零 delta 退避**：idle 分支已退避；维持轮退避判据记档（见 §37 遗留）。

**锁**：`TestCruiseLoopSyntax` 扩展（prompt 轮头存在）。

### 执行序（TDD）

F1 → F2 → F3 → F4 → 板归一（另见 `OuSheng_板归一与env维度_design.md`）。
每步：失败测试 → 最小实现 → `go test ./... && go vet ./...` → commit。

## 实栈重跑清单（二进制升级后，A 机）

1. `ousheng` 二进制替换（F1 生效即 fail-loud 全面恢复）。
2. 四仓 `adapter install` 重跑（驱动器归一 + 协议段升级）。
3. 板仓确认无 `me` 文件（已删，保持）。
4. 下次巡航晨检：`cruise report` 轮摘要 + session title 可区分性。
