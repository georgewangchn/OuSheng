package main

import (
	"fmt"
	"io"
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
	a := *actor
	if a == "" {
		a = defaultActor(fs.Dir())
	}
	if a == "" {
		fmt.Fprintln(stderr, "--actor required（或本机先 ousheng me）")
		return 2
	}
	// --wait（P2）：无活时阻塞轮询，替代驱动器盲 sleep。
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
			// 睡至多 20s，但不越过 deadline（修：短 wait 不被首个 20s 睡超时）。
			d := time.Until(deadline)
			if d <= 0 {
				break
			}
			if d > 20*time.Second {
				d = 20 * time.Second
			}
			time.Sleep(d)
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

	for _, w := range mc.ActiveWork {
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
		react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "待测池（testing 态，dev 未移交）——work show 判断后接手"})
	}
	if all, err := idx.All(); err == nil {
		for _, w := range all {
			if !model.WorkItemOpen(w.Status) {
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
			if w.Status != model.StatusReady || w.Assignee != "" || !mySystems[w.System] {
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
