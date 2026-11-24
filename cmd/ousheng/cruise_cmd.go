package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"ousheng/internal/context"
	"ousheng/internal/index"
	"ousheng/internal/machine"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

// cmdCruise 是巡航模式的只读驱动面（巡航模式设计方案 v0.2 §三 + 档案 §34）。
//
//	ousheng cruise plan   [--actor A] [--allow-new] [--wait N]  下一活（活到即醒）
//	ousheng cruise status [--hours N] [--stall-hours N]        巡航态 + 晨间简报
//	ousheng cruise ignite --seat A [--stop]                    点火/停火唯一入口（§50-B）
//
// 铁律：绳不代牛跑——plan 只产出指令源，执行归 harness/驱动器；status 纯投影。
// human 门不豁免：巡航遇 C2/拍板 = 挂起等天亮（status 列出），不是绕过。
func cmdCruise(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ousheng cruise <plan|status> [flags]")
		return 2
	}
	switch args[0] {
	case "plan":
		return cruisePlan(args[1:], stdout, stderr)
	case "status":
		return cruiseStatus(args[1:], stdout, stderr)
	case "report":
		return cruiseReport(args[1:], stdout, stderr)
	case "heartbeat":
		return cruiseHeartbeat(args[1:], stdout, stderr)
	case "dispatch":
		return cruiseDispatch(args[1:], stdout, stderr)
	case "ignite":
		return cruiseIgnite(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown cruise subcommand: %s\n", args[0])
		return 2
	}
}

// planItem 是 plan 清单的一条。
type planItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
}

// planData 是 cruise plan 的计算结果（--wait 探测与实算共用同一实现，防漂移）。
type planData struct {
	MC         *context.MyContext
	Resume     []planItem
	Claim      []planItem
	React      []planItem
	Idle       bool
	AttachSess string // 本代 opencode session id（会话代际制，档案 §34）
	State      string // 座位相关事实的内容指纹（驱动器静止闸，档案 §56）
}

// cruisePlan 输出某座位的下一活清单（驱动器把它拼进 opencode 的 prompt）。
func cruisePlan(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise plan")
	actor := fs.String("actor", "", "seat actor (default: me)")
	allowNew := fs.Bool("allow-new", false, "include the self-proposed new-item section (default off)")
	asJSON := fs.Bool("json", false, "output JSON")
	waitSec := fs.Int("wait", 0, "无活时阻塞等待（秒）：每 20s pull+重查，有活或超时返回——活到即醒")
	untilChange := fs.String("until-change", "", "静止闸（档案 §56）：state 指纹仍等于该值时继续等（与 --wait 同窗）；事实变了立即返回——变到即醒")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	// §42 三修②：分叉门——板仓与远端两源分叉时拒派活。11-14 事故机制：pm 侧
	// 积压 40 提交期间座位按旧态继续领活 → merge「取终态」拍平 5 卡（done 拍回
	// ready）。门在派活入口：分叉即断，积压不可能滚大。
	if ahead, behind, dv, derr := boardDiverged(fs.Dir()); dv {
		fmt.Fprintf(stderr, "板仓与远端分叉（本地超前 %d / 落后 %d）——先 ousheng sync 收口（diverged 人拍板逐卡决，禁批量 merge 拍平，§16/§42）再巡航\n", ahead, behind)
		return 1
	} else if derr != nil {
		fmt.Fprintf(stderr, "[warn] 分叉门降级：%v\n", derr)
	}
	// §55 入口自检（pm 提案：零模型 token 的机械前置检查——T-226 系事故的
	// 最后一道曝光面）。三联门 fail-closed，网络态降级警告（§48：抖动不停整夜）。
	if b, _ := gitOutErr(fs.Dir(), "rev-parse", "--abbrev-ref", "HEAD"); strings.TrimSpace(b) != "main" {
		fmt.Fprintf(stderr, "板仓分支=%s 非 main（板归一后恒 main，§38）——先 git switch main 再巡航\n", strings.TrimSpace(b))
		return 1
	}
	if st, _ := gitOutErr(fs.Dir(), "status", "--porcelain"); strings.Contains(st, "UU ") || strings.Contains(st, "AA ") {
		fmt.Fprintf(stderr, "板仓有未收口的合并冲突（T-206 教训：冲突标记入库=坏卡）——先解决冲突提交后再巡航\n")
		return 1
	}
	// review 🔴1：hard=remote 有本地缺的新（看旧板风险）拒；soft（未推/不可达）警告。
	// review 🔴3：拒词含「分叉」——驱动器 getpan 等待自愈白名单 grep「分叉」，兼容 §48 模式。
	if n, hard, soft := boardRemoteDrift(fs.Dir(), true); n >= 2 && len(hard) > 0 {
		fmt.Fprintf(stderr, "板仓多 remote 水位分叉（remote 有本地缺的新，T-226 盲区④）：%s——先 git pull <干道> 收口再巡航，禁看旧板盲跑\n", strings.Join(hard, "；"))
		return 1
	} else if n >= 2 && len(soft) > 0 {
		fmt.Fprintf(stderr, "[warn] 板仓 remote 未推平/部分不可达（低危，降级放行）：%s\n", strings.Join(soft, "；"))
	}
	a := *actor
	if a == "" {
		a = defaultActor(fs.Dir())
	}
	if a == "" {
		fmt.Fprintln(stderr, "--actor required（或本机先 ousheng me）")
		return 2
	}
	// --wait（P2）：无活时阻塞轮询，替代驱动器盲 sleep。
	// 步进（评审 §36 修正）：长窗恒 20s（拉取频率不变）；短窗（<20s）按 1/4
	// 窗口步进（≥1s）——原实现睡满剩余全程，活到也要等 deadline 才见。
	if *waitSec > 0 {
		deadline := time.Now().Add(time.Duration(*waitSec) * time.Second)
		for time.Now().Before(deadline) {
			pd, perr := buildPlan(fs.Dir(), a)
			if perr != nil {
				fmt.Fprintln(stderr, perr)
				return 1
			}
			// 静止闸（§56）：有活但「座位相关事实」自上轮起轮后未变 → 不算该醒
			//（上轮零 delta 且无人动过我的卡），继续等到变化或窗口到。
			quiet := *untilChange != "" && pd.State == *untilChange
			if !pd.Idle && !quiet {
				break
			}
			_, _ = gitOutTimeout(fs.Dir(), 30*time.Second, "pull", "--ff-only")
			d := time.Until(deadline)
			if d <= 0 {
				break
			}
			step := 20 * time.Second
			if d < step {
				step = d / 4
				if step < time.Second {
					step = time.Second
				}
			}
			time.Sleep(step)
		}
	}
	pd, err := buildPlan(fs.Dir(), a)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repo := gityaml.Open(fs.Dir())
	if *asJSON {
		printJSON(stdout, map[string]any{
			"actor": a, "resume": pd.Resume, "claim": pd.Claim, "react": pd.React,
			"idle": pd.Idle, "new": *allowNew, "attach_session": pd.AttachSess, "state": pd.State,
			"presence": presenceLine(repo),
			"charter":  "见 .ousheng/designs/cruise/（若立）——授权范围/投票规则/红线",
		})
		return 0
	}
	fmt.Fprintf(stdout, "cruise plan · actor=%s（优先序：恢复>领活>反应>新提）\n", a)
	fmt.Fprintf(stdout, "%s\n", presenceLine(repo))
	// state 行是驱动器静止闸的机械接口（§56）——不进 prompt，不给人读。
	fmt.Fprintf(stdout, "state: %s\n", pd.State)
	if pd.AttachSess != "" {
		fmt.Fprintf(stdout, "attach: %s（续同代省上下文重构：opencode run -s %s ...；换代=handover save 不带 --session）\n", pd.AttachSess, pd.AttachSess)
	}
	section := func(name string, items []planItem) {
		fmt.Fprintf(stdout, "\n%s (%d)\n", name, len(items))
		for _, it := range items {
			fmt.Fprintf(stdout, "  %s  %s\n      %s\n", it.ID, it.Title, it.Note)
		}
	}
	section("1·resume 恢复", pd.Resume)
	section("2·claim 领活", pd.Claim)
	section("3·react 反应", pd.React)
	if *allowNew {
		fmt.Fprintf(stdout, "\n4·new 自提（--allow-new）\n")
		fmt.Fprintf(stdout, "  自提优化单规则：只许 task/bug/test/notice（feature 结构性不可能）；\n")
		fmt.Fprintf(stdout, "  正文必须含第一性推演 + 反事实推演过程；只动自系统仓；C1 证据照常。\n")
	} else {
		fmt.Fprintf(stdout, "\n4·new 自提：关闭（--allow-new 开启；默认关——先稳主链路）\n")
	}
	if pd.Idle {
		fmt.Fprintf(stdout, "\ncruise: idle —— 无可做之事，驱动器应停机/退避（造活是病）\n")
	}
	return 0
}

// buildPlan 计算某座位的下一活（单实现：--wait 探测与最终输出共用）。
// 反应节四类：我的 blocked（依赖进行中/僵持）、待测池、全局收敛项（悬空/被砍
// 依赖——夜里无人修就僵到天亮）、协商信号（他人 24h 内在我的单留 evidence）。
func buildPlan(dir, actor string) (*planData, error) {
	svcCtx, err := context.New(gityaml.Open(dir))
	if err != nil {
		return nil, err
	}
	mc, err := svcCtx.GetMyContext(actor)
	if err != nil {
		return nil, err
	}
	idx := svcCtx.Idx
	var resume, claim, react []planItem

	// §42 修①：环境门——座位环境域来源优先级：OUSHENG_ENV 机器环境变量（权威，
	// 每机固定）> actor 后缀 -local（兼容约定）。卡 env（空=dev 缺省）不匹配的
	// 卡不进本座位任何计划节（resume/claim/awaiting_test/react 全覆盖）。
	seatEnv := strings.TrimSpace(os.Getenv("OUSHENG_ENV"))
	if seatEnv == "" {
		seatEnv = "dev"
		if strings.HasSuffix(actor, "-local") {
			seatEnv = "local"
		}
	}
	envOK := func(env string) bool {
		e := env
		if e == "" {
			e = "dev"
		}
		return e == seatEnv
	}

	for _, w := range mc.ActiveWork {
		if !envOK(w.Env) {
			continue
		}
		// §46（2026-11-14 实栈实锤）：恢复节须含「我名下的 ready 未开工」——
		// 11-14 空转事件：18 张 ready 有主卡在 plan 里完全隐身（恢复只收
		// doing/testing、领活只收无主 ready），座位以为没事干、记账空转 9h。
		// 此前「ready 有主=过渡瞬态」的假设被证伪：指派后未开工是常态。
		switch w.Status {
		case string(model.StatusDoing), string(model.StatusTesting):
			resume = append(resume, planItem{ID: w.ID, Title: w.Title, Note: "上轮 handover 指针续跑（ousheng handover show）"})
		case string(model.StatusReady):
			resume = append(resume, planItem{ID: w.ID, Title: w.Title, Note: "已指派未开工——直接开工（work update " + w.ID + " --status doing）"})
		}
	}
	for _, w := range mc.ActiveWork {
		if w.Status != string(model.StatusBlocked) {
			continue
		}
		note := "依赖进行中——去干别的，别轮询"
		if len(w.BlockedOn) == 0 {
			note = "依赖全关仍 blocked：僵持——按宪章协商/投票，或挂起等 human"
		}
		react = append(react, planItem{ID: w.ID, Title: w.Title, Note: note})
	}
	for _, w := range mc.AwaitingTest {
		if !envOK(w.Env) {
			continue
		}
		react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "待测池（testing 态，dev 未移交）——work show 判断后接手"})
	}
	if all, err := idx.All(); err == nil {
		for _, w := range all {
			if !model.WorkItemOpen(w.Status) || !envOK(w.Env) {
				continue
			}
			for _, dep := range w.DependsOn {
				d, ok, gerr := idx.Get(dep)
				if gerr != nil {
					continue
				}
				if !ok {
					react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "依赖悬空 " + dep + " ——建该单或摘除依赖（converge BLOCKER，夜里无人修就僵到天亮）"})
					break
				}
				if d.Status == model.StatusCancelled {
					react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "依赖被砍 " + dep + " ——协商替代路径或关单（converge BLOCKER）"})
					break
				}
			}
		}
	}
	if acts, err := gityaml.Open(dir).ListActivity(); err == nil {
		cutoff := time.Now().Add(-24 * time.Hour)
		noted := map[string]bool{}
		for i := len(acts) - 1; i >= 0; i-- {
			ac := acts[i]
			if ac.Action != "evidence_added" || ac.Actor == actor {
				continue
			}
			ts, perr := time.Parse(time.RFC3339, ac.TS)
			if perr != nil || ts.Before(cutoff) || noted[ac.WorkItem] {
				continue
			}
			for _, w := range mc.ActiveWork {
				if w.ID == ac.WorkItem {
					noted[ac.WorkItem] = true
					react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "24h 内 " + ac.Actor + " 在此单留言（可能是协商）——work show 读证据节并回应"})
					break
				}
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "[warn] 审计流读取失败——「24h 内他人在此单留言」反应节降级为空: %v\n", err)
	}
	mySystems := map[string]bool{}
	for _, s := range mc.Systems {
		mySystems[s] = true
	}
	if all, err := idx.All(); err == nil {
		for _, w := range all {
			if w.Status != model.StatusReady || w.Assignee != "" || !mySystems[w.System] || !envOK(w.Env) {
				continue
			}
			claim = append(claim, planItem{ID: w.ID, Title: w.Title, Note: "work assign 自领（CAS）"})
		}
	}
	sortItems := func(items []planItem) {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	}
	sortItems(resume)
	sortItems(claim)
	sortItems(react)

	// 会话代际指针：本座位最近快照携带的 session id。
	attach := ""
	for _, h := range svcCtxHandover(dir) {
		if h.Actor == actor && h.Session != "" {
			attach = h.Session
		}
	}
	state := seatStateFingerprint(dir, mc, resume, claim, react)
	return &planData{MC: mc, Resume: resume, Claim: claim, React: react,
		Idle: len(resume) == 0 && len(claim) == 0 && len(react) == 0, AttachSess: attach, State: state}, nil
}

// stateIDRe：静止闸指纹只把安全 ID 拼进路径（ID 来自板面解码，双保险防路径穿越）。
var stateIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// seatStateFingerprint（档案 §56）：「座位相关事实」的内容指纹——驱动器静止闸的
// 判据：与上一轮**起轮时**相同 ⇔ 上轮零 delta 且期间无人动过任何可能让我有活干
// 的文件。覆盖面按「下一次起轮会不会因此不同」逐类取：
//   - 恢复/领活/反应三节所指的卡（pm 落令、排位、他座位协商留言都落在卡面）；
//   - 我在途单的依赖目标卡（等的就是它）；
//   - 注册表（actors/systems/assignments/roles/presence——在场度改门行为）；
//   - 共识层 designs/（拍板/新轮次）与本域架构卡（契约指针）。
//
// 不含：handover 快照（只有本座位写）、他域卡、时间戳类易变输入。
//
// 为什么取代 §42④「plan 文本 cksum」：那把尺两头痛——attach 行每轮变 → 永不
// 触发（11-21 真板：四座 10-11 分钟一轮、零 delta 轮 50%，退避从未生效）；他人
// 往我的卡落 evidence 不改 plan 文本 → 该醒不醒。内容指纹是机械事实，不读语义。
// 成本：≤ 数十个 ≤8KB 文件的读取+哈希，毫秒级（--wait 每 20s 一次可承受）。
func seatStateFingerprint(dir string, mc *context.MyContext, lists ...[]planItem) string {
	root := filepath.Join(dir, ".ousheng")
	ids := map[string]bool{}
	for _, l := range lists {
		for _, it := range l {
			ids[it.ID] = true
		}
	}
	for _, w := range mc.ActiveWork {
		for _, d := range w.BlockedOn {
			ids[d] = true
		}
	}
	rels := []string{"systems.yaml", "assignments.yaml", "roles.yaml", "presence.yaml"}
	for id := range ids {
		if stateIDRe.MatchString(id) && !strings.Contains(id, "..") {
			rels = append(rels, "work/"+id+".yaml")
		}
	}
	collect := func(sub, ext string) {
		entries, err := os.ReadDir(filepath.Join(root, sub))
		if err != nil {
			return
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ext) {
				rels = append(rels, sub+"/"+e.Name())
			}
		}
	}
	collect("actors", ".yaml")
	for _, sys := range mc.Systems {
		rels = append(rels, "architecture/"+sys+".md")
	}
	if entries, err := os.ReadDir(filepath.Join(root, "designs")); err == nil {
		for _, topic := range entries {
			if !topic.IsDir() {
				continue
			}
			rounds, err := os.ReadDir(filepath.Join(root, "designs", topic.Name()))
			if err != nil {
				continue
			}
			for _, r := range rounds {
				if strings.HasSuffix(r.Name(), ".md") {
					rels = append(rels, "designs/"+topic.Name()+"/"+r.Name())
				}
			}
		}
	}
	sort.Strings(rels)
	h := sha256.New()
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			fmt.Fprintf(h, "%s\n<missing>\n", rel)
			continue
		}
		fmt.Fprintf(h, "%s\n%d\n", rel, len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// svcCtxHandover 读快照清单（buildPlan 辅助，错误吞掉——代际指针缺位不致命）。
func svcCtxHandover(dir string) []model.HandoverInfo {
	hs, err := gityaml.Open(dir).ListHandover()
	if err != nil {
		return nil
	}
	return hs
}

// cruiseStatus 巡航态 + 晨间简报（纯投影）。挂起三类都是 human 门在正确工作；
// stall 曝光（P3）：doing/testing 单超 N 小时无新 evidence = 空转嫌疑。
func cruiseStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise status")
	hours := fs.Int("hours", 12, "summary window in hours")
	stallHours := fs.Int("stall-hours", 12, "stall exposure threshold in hours（doing/testing 无新 evidence）")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	_, snap, err := loadSnapshot(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	since := time.Now().Add(-time.Duration(*hours) * time.Hour)
	acts, aerr := repo.ListActivity()
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "[warn] 审计流读取失败——基于审计流的读数（交付/新增/空转/战果/轮摘要）降级，不是真相: %v\n", aerr)
	}

	var delivered, created int
	createdBy := map[string]int{}
	for _, ac := range acts {
		ts, perr := time.Parse(time.RFC3339, ac.TS)
		if perr != nil || ts.Before(since) {
			continue
		}
		switch ac.Action {
		case "created":
			created++
			createdBy[typeOf(snap, ac.WorkItem)]++
		case "status_changed":
			if strings.Contains(ac.Detail, "-> done") {
				delivered++
			}
		}
	}

	var waitDecide, waitAck, waitArb []string
	for _, d := range snap.Designs {
		if d.Design.Status == model.DesignDraft && d.RoundExists {
			waitDecide = append(waitDecide, d.Topic)
		}
	}
	for _, w := range snap.WorkItems {
		if !model.WorkItemOpen(w.Status) {
			continue
		}
		if w.Contract != nil && w.Contract.Breaking && w.HumanAck == nil {
			waitAck = append(waitAck, w.ID)
		}
		if w.Status == model.StatusBlocked && len(w.DependsOn) == 0 {
			waitArb = append(waitArb, w.ID+"（blocked 无依赖——等什么？）")
		}
	}
	// P3 空转曝光：doing/testing 单，最后 evidence 早于 stall 阈值。
	var stalled []string
	lastEvid := map[string]time.Time{}
	for _, ac := range acts {
		if ac.Action == "evidence_added" {
			if ts, perr := time.Parse(time.RFC3339, ac.TS); perr == nil && ts.After(lastEvid[ac.WorkItem]) {
				lastEvid[ac.WorkItem] = ts
			}
		}
	}
	stallCutoff := time.Now().Add(-time.Duration(*stallHours) * time.Hour)
	for _, w := range snap.WorkItems {
		if w.Status != model.StatusDoing && w.Status != model.StatusTesting {
			continue
		}
		le, ok := lastEvid[w.ID]
		if !ok || le.Before(stallCutoff) {
			at := "从未有证据"
			if ok {
				at = le.Format("01-02 15:04")
			}
			stalled = append(stalled, fmt.Sprintf("%s（%s，assignee=%s）", w.ID, at, w.Assignee))
		}
	}
	news := ""
	if n := createdBy["feature"] + createdBy["requirement"]; n > 0 {
		news = fmt.Sprintf("（⚠ feature/requirement %d —— 巡航红线，晨间核查）", n)
	}
	// §56 晨检入口自检（F1：读数必须自带板龄；merge = 双写源指纹）。零网络。
	fresh := boardFreshnessWarn(fs.Dir())
	merges := boardMergeFingerprint(fs.Dir(), *hours)
	if *asJSON {
		printJSON(stdout, map[string]any{
			"window_hours": *hours, "delivered": delivered, "created": created,
			"created_by_type": createdBy, "redline_note": news, "presence": presenceLine(repo),
			"waiting_decide": waitDecide, "waiting_ack": waitAck, "waiting_arbitration": waitArb,
			"stalled": stalled, "board_freshness": fresh, "merge_commits": merges,
		})
		return 0
	}
	fmt.Fprintf(stdout, "== 巡航摘要（近 %dh）== %s\n", *hours, presenceLine(repo))
	if fresh != "" {
		fmt.Fprintf(stdout, "⚠ %s\n", fresh)
	}
	if len(merges) > 0 {
		fmt.Fprintf(stdout, "⚠ 近 %dh 板仓 merge 提交 %d 次——双写源指纹（单写源下 main 恒线性；写只从一个板仓 clone 进，§48/§50-A）：\n", *hours, len(merges))
		for _, m := range merges {
			fmt.Fprintf(stdout, "  · %s\n", m)
		}
	}
	// 宪章状态（启用链第一环——没宪章 = 巡航未启用，status 必须说）。
	charter := "未立（.ousheng/designs/cruise/）——授权范围/投票规则/时窗未定义，巡航未启用"
	for _, d := range snap.Designs {
		if d.Topic == "cruise" {
			charter = "已立：" + string(d.Design.Status)
		}
	}
	fmt.Fprintf(stdout, "宪章：%s\n", charter)
	for sys, repoDir := range machine.LoadRepoMap(fs.Dir()) {
		out, err := gitOutput(repoDir, "log", "--oneline", gitSinceHours(*hours))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		n := len(lines)
		if n == 1 && lines[0] == "" {
			n = 0
		}
		sample := ""
		if n > 0 {
			sample = "｜如 " + strings.TrimSpace(lines[0])
		}
		fmt.Fprintf(stdout, "  %s 仓夜间提交 %d%s\n", sys, n, sample)
	}
	fmt.Fprintf(stdout, "交付 %d 单｜新增 %d 单%s\n", delivered, created, news)
	if len(stalled) > 0 {
		fmt.Fprintf(stdout, "空转嫌疑 %d 项（doing/testing 超 %dh 无新证据——查是卡住还是忘了记账）：\n", len(stalled), *stallHours)
		for _, x := range stalled {
			fmt.Fprintf(stdout, "  ⚠ %s\n", x)
		}
	}
	if len(waitDecide)+len(waitAck)+len(waitArb) == 0 {
		fmt.Fprintf(stdout, "挂起等 human：无\n")
	} else {
		fmt.Fprintf(stdout, "挂起等 human %d 项（门在正确工作——夜里拦下的都在这）：\n", len(waitDecide)+len(waitAck)+len(waitArb))
		if l := strings.Join(waitDecide, ", "); l != "" {
			fmt.Fprintf(stdout, "  [拍板] %s\n", l)
		}
		if l := strings.Join(waitAck, ", "); l != "" {
			fmt.Fprintf(stdout, "  [C2]   %s\n", l)
		}
		if l := strings.Join(waitArb, ", "); l != "" {
			fmt.Fprintf(stdout, "  [仲裁] %s\n", l)
		}
	}
	return 0
}

// typeOf 从 snapshot 找 work item 类型（activity 只记 id；找不到计 "gone"）。
func typeOf(snap index.Snapshot, id string) string {
	for _, w := range snap.WorkItems {
		if w.ID == id {
			return string(w.Type)
		}
	}
	return "gone"
}

// cruiseReport：晨间简报——比 status 更结构化的终态汇总。可 cron 触发
// （5 7 * * * ousheng cruise report）——机制保证触发，不依赖 agent 存活
// （P1，档案 §35）。
//
// 评审修正（档案 §36）：
//   - 聚合口径 = 需求/缺陷粒度（FEAT-/REQ-/BUG-）——TASK 是执行叶不进晨报；
//     原 BUG-0 前缀启发式漏掉 BUG-1xx+（BUG-123 实锤）。
//   - 标题截断按 rune（原 40 字节切坏汉字 → 报表出 �）。
//   - 输出排序确定（原 map 遍历序随机，6 次跑出 3 种顺序）。
//   - 座位战果只计 agent 座位（human 是驾驶者，其待办在"待你处理"节）。
func cruiseReport(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise report")
	hours := fs.Int("hours", 12, "summary window in hours")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	_, snap, err := loadSnapshot(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	since := time.Now().Add(-time.Duration(*hours) * time.Hour)
	acts, aerr := repo.ListActivity()
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "[warn] 审计流读取失败——基于审计流的读数（交付/新增/空转/战果/轮摘要）降级，不是真相: %v\n", aerr)
	}
	// closedIn：窗口内有 done/cancelled 动作的收官单（晨报逐列）；更早的收官单
	// 折叠成计数——真板 164 行「✅ 收官」墙把在途单淹了（§56）：晨报是窗口报表，
	// 不是全量档案。
	closedIn := map[string]bool{}
	for _, a := range acts {
		if a.Action != "status_changed" || (!strings.Contains(a.Detail, "-> done") && !strings.Contains(a.Detail, "-> cancelled")) {
			continue
		}
		if ts, perr := time.Parse(time.RFC3339, a.TS); perr == nil && !ts.Before(since) {
			closedIn[a.WorkItem] = true
		}
	}

	// 需求/缺陷清单（诚实形态：无父子字段——聚合判定已记档延后，此处是清单非归并）。
	type itemRow struct {
		ID, Title, Status string
	}
	var items []itemRow
	for _, w := range snap.WorkItems {
		if !strings.HasPrefix(w.ID, "FEAT-") && !strings.HasPrefix(w.ID, "REQ-") && !strings.HasPrefix(w.ID, "BUG-") {
			continue
		}
		st := "进行中"
		if w.Status == model.StatusDone || w.Status == model.StatusCancelled {
			st = "✅ 收官"
		}
		items = append(items, itemRow{ID: w.ID, Title: truncRunes(w.Title, 40), Status: st})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	// 每座位战果（agent 座位；human 不计——座位=agent 执行席）。
	humanSeat := map[string]bool{}
	if afs, aerr := repo.ListActors(); aerr == nil {
		for _, af := range afs {
			if af.Actor.Type == model.ActorHuman {
				humanSeat[af.Actor.ID] = true
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "[warn] 注册表读取失败——human 座位过滤降级（战果表可能混入 human）: %v\n", aerr)
	}
	type seat struct {
		Actor               string
		Evid, Done, Created int
	}
	seats := map[string]*seat{}
	for _, a := range acts {
		if humanSeat[a.Actor] {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, a.TS)
		if perr != nil || ts.Before(since) {
			continue
		}
		if _, ok := seats[a.Actor]; !ok {
			seats[a.Actor] = &seat{Actor: a.Actor}
		}
		switch a.Action {
		case "evidence_added":
			seats[a.Actor].Evid++
		case "status_changed":
			if strings.Contains(a.Detail, "-> done") {
				seats[a.Actor].Done++
			}
		case "created":
			seats[a.Actor].Created++
		}
	}
	seatOrder := make([]string, 0, len(seats))
	for a := range seats {
		seatOrder = append(seatOrder, a)
	}
	sort.Strings(seatOrder)

	// 座位轮摘要（2026-11-12 档案 §37 F2）：activity 按座位聚簇成轮（间隔 >15min
	// 分轮），纯记账轮 = 该轮动作全为 evidence_added——零 delta 维持轮的直接曝光
	// （A 机复盘：233 轮强模型、过半纯记账，token 烧钱轮次第一次在绳上有投影）。
	const roundGap = 15 * time.Minute
	seatActs := map[string][]model.Activity{}
	for _, a := range acts {
		if humanSeat[a.Actor] {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, a.TS)
		if perr != nil || ts.Before(since) {
			continue
		}
		seatActs[a.Actor] = append(seatActs[a.Actor], a)
	}
	rounds := map[string]int{}
	pureRounds := map[string]int{}
	touched := map[string][]string{}
	for actor, list := range seatActs {
		// 按时刻排序（非字符串——RFC3339 混合时区偏移 +08:00/Z 词序≠时序，
		// 轮边界会被错切；坏 TS 行跳过，与 §31 读面 best-effort 对齐）。
		type timedAct struct {
			t time.Time
			a model.Activity
		}
		parsed := make([]timedAct, 0, len(list))
		for _, a := range list {
			ts, perr := time.Parse(time.RFC3339, a.TS)
			if perr != nil {
				continue
			}
			parsed = append(parsed, timedAct{t: ts, a: a})
		}
		sort.Slice(parsed, func(i, j int) bool { return parsed[i].t.Before(parsed[j].t) })
		n, pure := 0, 0
		var items map[string]bool
		var evid, changes int
		var last time.Time
		seen := map[string]bool{}
		start := func() {
			items, evid, changes = map[string]bool{}, 0, 0
		}
		flush := func() {
			if items == nil {
				return
			}
			n++
			if evid > 0 && changes == 0 {
				pure++
			}
			for it := range items {
				if !seen[it] {
					seen[it] = true
					touched[actor] = append(touched[actor], it)
				}
			}
		}
		for _, ta := range parsed {
			if items == nil || ta.t.Sub(last) > roundGap {
				flush()
				start()
			}
			last = ta.t
			a := ta.a
			if a.WorkItem != "" {
				items[a.WorkItem] = true
			}
			if a.Action == "evidence_added" {
				evid++
			} else {
				changes++
			}
		}
		flush()
		rounds[actor], pureRounds[actor] = n, pure
		sort.Strings(touched[actor])
	}

	// 挂起清单
	var waits []string
	for _, d := range snap.Designs {
		if d.Design.Status == model.DesignDraft && d.RoundExists {
			waits = append(waits, "[拍板] "+d.Topic)
		}
	}
	// Q(pm) 攒批计数（§37 复盘 #6，2026-11-13）：卡面 evidence 里 Q(pm) 条目
	// 是 pm 的真回复通道（会话里回 OK 走错通道）——晨报显积压量。
	// §44 自动清理规则（2026-11-14）：①卡已 done/cancelled → Q 随卡失效不计；
	// ②已落「pm 代答」标记的卡不计——积压数只反映真正等 pm 决策的量。
	qIDs := []string{}
	for _, w := range snap.WorkItems {
		if !model.WorkItemOpen(w.Status) {
			continue
		}
		hasQ, answered := false, false
		for _, ev := range w.Evidence {
			if strings.Contains(ev.Note, "Q(pm)") || strings.Contains(ev.Note, "Q（pm）") {
				hasQ = true
			}
			if strings.Contains(ev.Note, "【pm 代答】") || strings.Contains(ev.Note, "Q(pm) 已关闭") {
				answered = true
			}
		}
		if hasQ && !answered {
			qIDs = append(qIDs, w.ID)
		}
	}
	for _, w := range snap.WorkItems {
		if !model.WorkItemOpen(w.Status) {
			continue
		}
		if w.Contract != nil && w.Contract.Breaking && w.HumanAck == nil {
			waits = append(waits, "[C2] "+w.ID)
		}
	}

	if *asJSON {
		type seatJSON struct {
			Actor      string   `json:"actor"`
			Done       int      `json:"delivered"`
			Created    int      `json:"created"`
			Evid       int      `json:"evidence"`
			Rounds     int      `json:"rounds,omitempty"`
			PureRounds int      `json:"pure_rounds,omitempty"`
			Touched    []string `json:"items_touched,omitempty"`
		}
		type itemJSON struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		sj := make([]seatJSON, 0, len(seatOrder))
		for _, a := range seatOrder {
			s := seats[a]
			sj = append(sj, seatJSON{Actor: a, Done: s.Done, Created: s.Created, Evid: s.Evid,
				Rounds: rounds[a], PureRounds: pureRounds[a], Touched: touched[a]})
		}
		ij := make([]itemJSON, 0, len(items))
		for _, it := range items {
			ij = append(ij, itemJSON{ID: it.ID, Title: it.Title, Status: it.Status})
		}
		printJSON(stdout, map[string]any{
			"window_hours": *hours, "seats": sj, "items": ij, "waiting": waits,
			"q_pm_backlog": len(qIDs), "board_freshness": boardFreshnessWarn(fs.Dir()),
		})
		return 0
	}

	fmt.Fprintf(stdout, "# 巡航简报（%s，近 %dh）\n\n", time.Now().Format("2006-01-02 15:04"), *hours)
	// §56：晨报自带板龄（pm 机只读 clone 停在 9 天前曾把旧板当现况读完整个晨检）。
	if fresh := boardFreshnessWarn(fs.Dir()); fresh != "" {
		fmt.Fprintf(stdout, "> ⚠ %s\n\n", fresh)
	}
	fmt.Fprintf(stdout, "## 每座位战果\n")
	if len(seatOrder) == 0 {
		fmt.Fprintf(stdout, "（窗口内无 agent 座位活动）\n")
	} else {
		fmt.Fprintf(stdout, "| 座位 | 交付 | 新增 | evidence |\n|---|---|---|---|\n")
		for _, a := range seatOrder {
			s := seats[a]
			fmt.Fprintf(stdout, "| %s | %d | %d | %d |\n", s.Actor, s.Done, s.Created, s.Evid)
		}
	}
	// 座位轮摘要（§37 F2）：纯记账轮占比高 = 维持空转信号（驱动器退避判据的
	// 曝光面——判据触发见档案 §37 遗留）。
	fmt.Fprintf(stdout, "\n## 座位轮摘要（纯记账轮 = 只落 evidence 的零 delta 维持轮）\n")
	if len(seatOrder) == 0 {
		fmt.Fprintf(stdout, "（无）\n")
	} else {
		fmt.Fprintf(stdout, "| 座位 | 轮数 | 纯记账轮 | 触单 |\n|---|---|---|---|\n")
		for _, a := range seatOrder {
			tp := strings.Join(touched[a], " ")
			if tp == "" {
				tp = "—"
			}
			fmt.Fprintf(stdout, "| %s | %d | %d | %s |\n", a, rounds[a], pureRounds[a], truncRunes(tp, 40))
		}
	}
	fmt.Fprintf(stdout, "\n## 需求/缺陷清单（FEAT/REQ/BUG）\n")
	var openRows, recentRows, histRows []itemRow
	for _, it := range items {
		switch {
		case it.Status == "进行中":
			openRows = append(openRows, it)
		case closedIn[it.ID]:
			recentRows = append(recentRows, it)
		default:
			histRows = append(histRows, it)
		}
	}
	if len(items) == 0 {
		fmt.Fprintf(stdout, "（无）\n")
	} else {
		table := func(rows []itemRow) {
			fmt.Fprintf(stdout, "| ID | 状态 | 标题 |\n|---|---|---|\n")
			for _, it := range rows {
				fmt.Fprintf(stdout, "| %s | %s | %s |\n", it.ID, it.Status, it.Title)
			}
		}
		if len(openRows) > 0 {
			fmt.Fprintf(stdout, "进行中 %d：\n", len(openRows))
			table(openRows)
		}
		if len(recentRows) > 0 {
			fmt.Fprintf(stdout, "\n窗口内收官 %d：\n", len(recentRows))
			table(recentRows)
		}
		if len(histRows) > 0 {
			byPrefix := map[string]int{}
			for _, it := range histRows {
				byPrefix[strings.SplitN(it.ID, "-", 2)[0]]++
			}
			var parts []string
			for _, p := range []string{"BUG", "FEAT", "REQ"} {
				if byPrefix[p] > 0 {
					parts = append(parts, fmt.Sprintf("%s %d", p, byPrefix[p]))
				}
			}
			fmt.Fprintf(stdout, "\n历史收官 %d 单不逐列（%s）——`work list --status done` 可查\n", len(histRows), strings.Join(parts, " · "))
		}
	}
	if len(qIDs) > 0 {
		fmt.Fprintf(stdout, "\n## Q(pm) 攒批（卡面 Q(pm) evidence——web 驾驶舱/板上批量回，勿在会话里回 OK）\n")
		shown := qIDs
		if len(shown) > 8 {
			shown = shown[:8]
		}
		fmt.Fprintf(stdout, "- 积压 %d 张：%s%s\n", len(qIDs), strings.Join(shown, " "),
			map[bool]string{true: " …", false: ""}[len(qIDs) > 8])
	}
	if len(waits) > 0 {
		fmt.Fprintf(stdout, "\n## 待你处理\n")
		for _, w := range waits {
			fmt.Fprintf(stdout, "- %s\n", w)
		}
	}
	return 0
}

// truncRunes 按 rune 截断（中文标题 40 字节切分会产出非法 UTF-8——报表 mojibake 实锤）。
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// boardContactStale：远端接触痕迹超过此时长 → 观测面点名（一天没碰过远端，
// tracking ref 自身可能陈旧——pm 机只读 clone 实况：clone 停 11-15 达 9 天）。
const boardContactStale = 24 * time.Hour

// gitSinceHours：git --since 的合法写法（单一出口）。`--since=12h` 不是 git
// 日期语法——实测被当「本月 12 日」（12h → 全量 3364 条）/ 非法日（240h → 0 条）：
// 「仓夜间提交 N」行自 §34 起一直在报错数，无锁覆盖（§56 复核抓回）。
func gitSinceHours(hours int) string {
	return fmt.Sprintf("--since=%d hours ago", hours)
}

// boardFreshnessWarn（档案 §56；F1「ok 必须含新鲜度」在晨检入口的落地）：
// 只读观测面的板龄——零网络：①本地 HEAD 是否落后上游 tracking ref（上次 fetch
// 已知有新）；②远端接触痕迹（FETCH_HEAD / 远端跟踪 reflog）距今多久（判断
// tracking ref 自身新不新）。返回空 = 新鲜（健康态零输出，不添噪音）。
//
// 观测面不 fetch：驾驶舱每次刷新都调 status，网络半开会把页面拖死（F3/F4）；
// 全检归 doctor/ignite。新鲜度是「读者自己知道读的是不是旧板」，不是替他拉。
func boardFreshnessWarn(dir string) string {
	// HEAD...@{u}：左 = 本地独有（未推），右 = 上游独有（本地缺新 = 落后）。
	if out, err := gitOutErr(dir, "rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
		if c := strings.Fields(strings.TrimSpace(out)); len(c) == 2 && c[1] != "0" {
			return fmt.Sprintf("本地板落后上游 %s 提交——以上读数是旧板（观测面不自动拉取）：先 ousheng sync / git pull", c[1])
		}
	}
	if t, ok := lastRemoteContact(dir); ok && time.Since(t) > boardContactStale {
		return fmt.Sprintf("板仓上次 fetch 在 %.0f 小时前——远端可能有新（tracking ref 自身陈旧；git fetch 后再看）", time.Since(t).Hours())
	}
	return ""
}

// lastRemoteContact：最近一次远端接触——fetch 写 FETCH_HEAD；clone / 带更新的
// fetch / push -u 写远端跟踪 reflog。取两者最晚；全无 = 从未接触（单机形态不判）。
func lastRemoteContact(dir string) (time.Time, bool) {
	var latest time.Time
	found := false
	if fi, err := os.Stat(filepath.Join(dir, ".git", "FETCH_HEAD")); err == nil {
		latest, found = fi.ModTime(), true
	}
	_ = filepath.Walk(filepath.Join(dir, ".git", "logs", "refs", "remotes"), func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.ModTime().After(latest) {
			latest, found = fi.ModTime(), true
		}
		return nil
	})
	return latest, found
}

// boardMergeFingerprint（档案 §56）：窗口内板仓 merge 提交——双写源指纹。
// 单写源下 sync 只 ff-only、ousheng 写路径只提交不合并 → main 恒线性；出现
// merge = 有人从第二个 clone 写板后手工合并（§42 拍平 5 卡 / §48 全队停摆 /
// 11-22 冲突标记入库的公共前因；真板 8 天 11 次）。只曝光不拦：合并是人的合法
// 收口动作，但它的存在证明「单写源」纪律破了，pm 必须看见。
func boardMergeFingerprint(dir string, hours int) []string {
	res := []string{}
	out, err := gitOutErr(dir, "log", "--merges", gitSinceHours(hours), "--format=%h %s", "HEAD")
	if err != nil {
		return res
	}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, truncRunes(l, 70))
		}
	}
	return res
}

// cruiseHeartbeat：各座位心跳投影（P2，档案 §35）——最后 activity距今多久。
// 纯投影零新存储；>N 分钟无心跳=⚠ 疑似停止。
// 评审修正（档案 §36）：只看 agent 座位（human PM 非值班席）+ 输出排序确定。
func cruiseHeartbeat(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise heartbeat")
	threshold := fs.Int("min", 30, "alert threshold in minutes")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	acts, err := repo.ListActivity()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// 座位 = 注册表里的 agent（评审修正 §36：human 非值班席）。§56 收紧：只认
	// 注册座位——审计流里的未注册名/已撤座位残留（真板：ousheng、data-local）
	// 曾被当成「疑似停止」的座位永久 ⚠，警告面被幽灵占据。
	agentSeat := map[string]bool{}
	afs, aerr := repo.ListActors()
	if aerr != nil {
		fmt.Fprintln(stderr, aerr)
		return 1
	}
	for _, af := range afs {
		if af.Actor.Type == model.ActorAgent {
			agentSeat[af.Actor.ID] = true
		}
	}
	last := map[string]time.Time{}
	for i := len(acts) - 1; i >= 0; i-- {
		a := acts[i]
		if !agentSeat[a.Actor] {
			continue
		}
		if _, ok := last[a.Actor]; !ok {
			if ts, perr := time.Parse(time.RFC3339, a.TS); perr == nil {
				last[a.Actor] = ts
			}
		}
	}
	order := make([]string, 0, len(last))
	for a := range last {
		order = append(order, a)
	}
	sort.Strings(order)
	fmt.Fprintf(stdout, "座位心跳（阈值 %d 分钟）:\n", *threshold)
	for _, actor := range order {
		t := last[actor]
		ago := time.Since(t)
		warn := ""
		if ago > time.Duration(*threshold)*time.Minute {
			warn = " ⚠ 疑似停止"
		}
		fmt.Fprintf(stdout, "  %s: %s 前（%s）%s\n", actor, ago.Round(time.Minute), t.Format("15:04"), warn)
	}
	return 0
}

// cruiseDispatch：单机调度器（P4，档案 §35）——一个进程管理本机全部座位。
// 每轮：一次 pull → 全座位活况 → 有活输出派单 → 等外部 runner 消费窗口 → 下一轮；
// 全 idle 共享等待。多机形态：每机各跑一个 dispatch（天然分布式）。
// token 节约：单次 git pull + 快照装载（vs 每座位独立 runner 各自拉取）。
// 边界（绳不代牛跑）：dispatch 是指挥官不是执行者——spawn opencode 归外层
// runner/cron；本命令只产出各座位 plan（消费方拿去喂 opencode）。
func cruiseDispatch(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise dispatch")
	seats := fs.String("seats", "", "comma-separated actor list (default: all registered agents with ousheng.json on this machine)")
	maxRounds := fs.Int("rounds", 20, "max dispatch rounds")
	waitSec := fs.Int("wait", 600, "wait seconds per round（有活=给外部 runner 的消费窗口；idle=共享等待）")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	// 解析座位列表（注册表读失败 = fail-closed 拒绝，不误报"无座位"）。
	var seatList []string
	if *seats != "" {
		seatList = strings.Split(*seats, ",")
	} else {
		afs, aerr := repo.ListActors()
		if aerr != nil {
			fmt.Fprintln(stderr, aerr)
			return 1
		}
		for _, af := range afs {
			if af.Actor.Type == model.ActorAgent {
				seatList = append(seatList, af.Actor.ID)
			}
		}
	}
	if len(seatList) == 0 {
		fmt.Fprintln(stderr, "无 agent 座位")
		return 1
	}
	fmt.Fprintf(stdout, "cruise dispatch · %d 座位: %s\n", len(seatList), strings.Join(seatList, ","))
	fmt.Fprintf(stdout, "%s\n", presenceLine(repo))

	for round := 1; round <= *maxRounds; round++ {
		fmt.Fprintf(stdout, "\n[dispatch] round %d/%d\n", round, *maxRounds)
		// 一次 pull，全座位共享（失败容忍：离线时仍用本地快照调度，大声提示）。
		if _, perr := gitOutTimeout(fs.Dir(), 30*time.Second, "pull", "--ff-only"); perr != nil {
			fmt.Fprintf(stderr, "[dispatch] pull 失败（离线容忍，按本地快照调度）: %v\n", perr)
		}

		totalWork := 0
		for _, seat := range seatList {
			pd, err := buildPlan(fs.Dir(), seat)
			if err != nil {
				fmt.Fprintf(stderr, "[dispatch] %s plan 失败: %v\n", seat, err)
				continue
			}
			if pd.Idle {
				fmt.Fprintf(stdout, "  %s: idle\n", seat)
				continue
			}
			totalWork++
			work := len(pd.Resume) + len(pd.Claim) + len(pd.React)
			fmt.Fprintf(stdout, "  %s: %d 项活（resume %d / claim %d / react %d）\n",
				seat, work, len(pd.Resume), len(pd.Claim), len(pd.React))
			// 生成该座位的 plan 输出并输出（驱动器拿去喂 opencode）
			fmt.Fprintf(stdout, "--- %s plan ---\n", seat)
			for _, it := range pd.Resume {
				fmt.Fprintf(stdout, "  resume %s %s\n", it.ID, it.Title)
			}
			for _, it := range pd.Claim {
				fmt.Fprintf(stdout, "  claim %s %s\n", it.ID, it.Title)
			}
			for _, it := range pd.React {
				fmt.Fprintf(stdout, "  react %s %s\n", it.ID, it.Title)
			}
		}
		if totalWork == 0 {
			fmt.Fprintf(stdout, "[dispatch] 全座位 idle，等 %ds\n", *waitSec)
			time.Sleep(time.Duration(*waitSec) * time.Second)
		} else {
			// 评审修正（档案 §36 Y5）：有活轮原实现立即进下一轮——20 轮瞬间烧完
			// （自旋），派单输出无人消费即弃。改为等消费窗口：外部 runner 在此
			// 窗口内领活执行，下一轮 pull 后看到新状态。
			fmt.Fprintf(stdout, "[dispatch] %d 座位有活——已输出派单，等 %ds 给外部 runner 消费\n", totalWork, *waitSec)
			time.Sleep(time.Duration(*waitSec) * time.Second)
		}
	}
	return 0
}
