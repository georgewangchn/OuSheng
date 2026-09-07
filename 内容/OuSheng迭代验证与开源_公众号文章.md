# 牵着牛走两步：OuSheng 迭代验证与开源

> 第一篇讲"为什么做"，第二篇讲"怎么做"，这篇讲"能不能跑"——绳子编出来了，套上牛，走两步。

---

## 一、开源了

仓库：`github.com/georgewangchn/OuSheng`。

中间发现五个 bug，逐个修掉，模拟两个新手从头协作一遍，才敢 push。怕的不是丢人，是有人 clone 下来跑不通，默默关掉。

---

## 二、第一关：新手跑通

清空缓存，从零 build，照 README 逐字敲。跑通了——但 `git archive HEAD`（新用户 clone 拿到的）只有 Phase 1，Phase 2-5 全没 commit。README 里 `go build -o ousheng-mcp ./cmd/mcp` 直接报 directory not found。

光顾着开发，忘了落到 git。一次大 commit 补上 30 个文件。再 archive、再 build、再跑教程——全绿。

**新用户看到的不是工作树，是 HEAD。**

---

## 三、第二关：TDD 逼出五个 bug

铁律：没看过测试失败，就不知道它测的对不对。

**Bug 1：进程崩溃后看板永久锁死。** 文件锁残留，所有写入阻塞。修复：锁文件写 PID，拿锁前检查 PID 存活，死了就清。

**Bug 2：GitLog 边界值炸了。** `n=0` 生成 `git log -0`，exit 128。修复：`n<=0` 直接 return 空。

**Bug 3：空看板收敛状态错了。** 空 converge 返回 IN_PROGRESS（暗示有活），应返回 CONVERGED（无事可做）。修复：去掉 `len(cards)>0` 守卫。

**Bug 4：坏依赖只查了 verified 卡。** proposed 卡依赖 deprecated 卡，converge 居然返回 IN_PROGRESS。修复：对所有非 deprecated 卡都查。

**Bug 5：并发写竞态（最严重）。** 10 goroutine 写同一卡，期望 1 成功 9 conflict。实际 7 个"board locked"+2 个"index.lock 冲突"+1 成功，锁失效。根因：stale-lock 检测的 TOCTOU 竞态。修复：Store 加 `sync.Mutex` 序列化进程内访问，文件锁留给跨进程。修复后：1 成功、9 conflict、0 错误。

---

## 四、第三关：两个新手协作

搭模拟环境：bare git 仓当 remote，两个独立目录当两个开发者。

**完整生命周期：** 后端提 proposed → 前端改 agreed → 后端 live → 前端带 evidence 推 verified → converge **CONVERGED**。中间测了 CAS 冲突（旧版本号被拒）、非法转换（verified→proposed 被拒）、context 拉历史、verify 查签名。全绿。

**破坏性变更：** breaking=true 无 human_ack 被拒，加了通过；deprecate 旧接口列出需迁移卡片；converge STUCK（有卡依赖 deprecated）；前端迁移依赖后 IN_PROGRESS。

C1、C2、CAS、状态机、DAG、F1、F2 全过一遍。Bug 4 的修复在真实场景里验证生效。

---

## 五、数字

105 测试全过 race-clean，5 bug 全 TDD 修，2 轮新手模拟，0 mock，3 commit 推 GitHub。

---

## 六、为什么这么较真

开源的第一次接触决定生死。跑通留下，跑不通关掉，没有第二次解释机会。README 每行命令自己敲，每个错误路径模拟，每个并发场景压测。**不是相信代码，是相信证据。**

---

## 七、写在最后

三篇三个阶段：为什么做 → 怎么做 → 能不能跑。走两步的结果：能跑，能协作，bug 都修了，开源了。

但这只是最小场景。C1 生死线在真实企业网络里成不成立、三人协作会不会崩、离线重连怎么同步——都还没碰。每一环都对应一个可证伪的判断，都要像今天这样走一遍。

**牵引绳实现简单，尝试应用是关键。**
