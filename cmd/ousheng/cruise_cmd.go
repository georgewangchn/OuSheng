package main

import (
	"fmt"
	"io"
	"os"
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
}

// cruisePlan 输出某座位的下一活清单（驱动器把它拼进 opencode 的 prompt）。
func cruisePlan(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise plan")
	actor := fs.String("actor", "", "seat actor (default: me)")
	allowNew := fs.Bool("allow-new", false, "include the self-proposed new-item section (default off)")
	asJSON := fs.Bool("json", false, "output JSON")
	waitSec := fs.Int("wait", 0, "无活时阻塞等待（秒）：每 20s pull+重查，有活或超时返回——活到即醒")
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
			if !pd.Idle {
				break
			}
			gitOutput(fs.Dir(), "pull", "--ff-only")
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
			"idle": pd.Idle, "new": *allowNew, "attach_session": pd.AttachSess,
			"presence": presenceLine(repo),
			"charter":  "见 .ousheng/designs/cruise/（若立）——授权范围/投票规则/红线",
		})
		return 0
	}
	fmt.Fprintf(stdout, "cruise plan · actor=%s（优先序：恢复>领活>反应>新提）\n", a)
	fmt.Fprintf(stdout, "%s\n", presenceLine(repo))
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
		if w.Status == string(model.StatusDoing) || w.Status == string(model.StatusTesting) {
			resume = append(resume, planItem{ID: w.ID, Title: w.Title, Note: "上轮 handover 指针续跑（ousheng handover show）"})
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
	return &planData{MC: mc, Resume: resume, Claim: claim, React: react,
		Idle: len(resume) == 0 && len(claim) == 0 && len(react) == 0, AttachSess: attach}, nil
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
	acts, _ := repo.ListActivity()

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
	if *asJSON {
		printJSON(stdout, map[string]any{
			"window_hours": *hours, "delivered": delivered, "created": created,
			"created_by_type": createdBy, "redline_note": news, "presence": presenceLine(repo),
			"waiting_decide": waitDecide, "waiting_ack": waitAck, "waiting_arbitration": waitArb,
			"stalled": stalled,
		})
		return 0
	}
	fmt.Fprintf(stdout, "== 巡航摘要（近 %dh）== %s\n", *hours, presenceLine(repo))
	// 宪章状态（启用链第一环——没宪章 = 巡航未启用，status 必须说）。
	charter := "未立（.ousheng/designs/cruise/）——授权范围/投票规则/时窗未定义，巡航未启用"
	for _, d := range snap.Designs {
		if d.Topic == "cruise" {
			charter = "已立：" + string(d.Design.Status)
		}
	}
	fmt.Fprintf(stdout, "宪章：%s\n", charter)
	for sys, repoDir := range machine.LoadRepoMap(fs.Dir()) {
		out, err := gitOutput(repoDir, "log", "--oneline", fmt.Sprintf("--since=%dh", *hours))
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
	acts, _ := repo.ListActivity()

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
			"q_pm_backlog": len(qIDs),
		})
		return 0
	}

	fmt.Fprintf(stdout, "# 巡航简报（%s，近 %dh）\n\n", time.Now().Format("2006-01-02 15:04"), *hours)
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
	if len(items) == 0 {
		fmt.Fprintf(stdout, "（无）\n")
	} else {
		fmt.Fprintf(stdout, "| ID | 状态 | 标题 |\n|---|---|---|\n")
		for _, it := range items {
			fmt.Fprintf(stdout, "| %s | %s | %s |\n", it.ID, it.Status, it.Title)
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
	humanSeat := map[string]bool{}
	if afs, aerr := repo.ListActors(); aerr == nil {
		for _, af := range afs {
			if af.Actor.Type == model.ActorHuman {
				humanSeat[af.Actor.ID] = true
			}
		}
	}
	last := map[string]time.Time{}
	for i := len(acts) - 1; i >= 0; i-- {
		a := acts[i]
		if humanSeat[a.Actor] {
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
		if _, perr := gitOutput(fs.Dir(), "pull", "--ff-only"); perr != nil {
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
