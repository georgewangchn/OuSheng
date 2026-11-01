package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"ousheng/internal/context"
	"ousheng/internal/index"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

// cmdCruise 是巡航模式的只读驱动面（巡航模式设计方案 v0.2 §三）。
//
//	ousheng cruise plan   [--actor A] [--allow-new]   下一活（优先序：恢复>领活>反应>新提）
//	ousheng cruise status [--hours N]                 巡航态 + 晨间简报
//
// 铁律：绳不代牛跑——plan 只产出指令源，执行归 harness/驱动器；status 纯投影
// （activity/converge 语义的派生视图），零新存储。human 门不豁免：巡航遇到
// C2/拍板 = 挂起等天亮（status 列出），不是绕过。
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

// cruisePlan 输出某座位的下一活清单（驱动器把它拼进 opencode/claude 的 prompt）。
func cruisePlan(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise plan")
	actor := fs.String("actor", "", "seat actor (default: me)")
	allowNew := fs.Bool("allow-new", false, "include the self-proposed new-item section (default off —— 拍板 2：先稳主链路)")
	asJSON := fs.Bool("json", false, "output JSON")
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
	// 单源装载（复查修正）：context.New 自带 Idx——不再叠加 loadSnapshot 双份快照。
	svcCtx, err := context.New(gityaml.Open(fs.Dir()))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	mc, err := svcCtx.GetMyContext(a)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	idx := svcCtx.Idx

	type planItem struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Note  string `json:"note,omitempty"`
	}
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
	// awaiting_test（复查补）：tester 视角的待测池（§26 机制）——巡航时反应节
	// 必须含它，否则 tester 座位整夜失明。事实投影非派活指令，tester 自行 show 判断。
	for _, w := range mc.AwaitingTest {
		react = append(react, planItem{ID: w.ID, Title: w.Title, Note: "待测池（testing 态，dev 未移交）——work show 判断后接手"})
	}
	// 待领池：我的执行系统 × ready 无主（backlog 不列——巡航只领能直接开工的）。
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

	idle := len(resume) == 0 && len(claim) == 0 && len(react) == 0
	if *asJSON {
		out := map[string]any{
			"actor":  a,
			"resume": resume, "claim": claim, "react": react,
			"idle":    idle,
			"new":     *allowNew,
			"charter": "见 .ousheng/designs/cruise/（若立）——授权范围/投票规则/红线",
		}
		printJSON(stdout, out)
		return 0
	}
	fmt.Fprintf(stdout, "cruise plan · actor=%s（优先序：恢复>领活>反应>新提）\n", a)
	section := func(name string, items []planItem) {
		fmt.Fprintf(stdout, "\n%s (%d)\n", name, len(items))
		for _, it := range items {
			fmt.Fprintf(stdout, "  %s  %s\n      %s\n", it.ID, it.Title, it.Note)
		}
	}
	section("1·resume 恢复", resume)
	section("2·claim 领活", claim)
	section("3·react 反应", react)
	if *allowNew {
		fmt.Fprintf(stdout, "\n4·new 自提（--allow-new）\n")
		fmt.Fprintf(stdout, "  自提优化单规则：只许 task/bug/test/notice（feature 结构性不可能）；\n")
		fmt.Fprintf(stdout, "  正文必须含第一性推演 + 反事实推演过程；只动自系统仓；C1 证据照常。\n")
	} else {
		fmt.Fprintf(stdout, "\n4·new 自提：关闭（--allow-new 开启；默认关——先稳主链路）\n")
	}
	if idle {
		fmt.Fprintf(stdout, "\ncruise: idle —— 无可做之事，驱动器应停机/退避（造活是病）\n")
	}
	return 0
}

// cruiseStatus 巡航态 + 晨间简报（纯投影）。挂起三类都是 human 门在正确工作：
// [拍板] draft 齐轮次等 decide；[C2] breaking 等 ack；[仲裁] 依赖全关仍 blocked。
func cruiseStatus(args []string, stdout, stderr io.Writer) int {
	fs := newFS("cruise status")
	hours := fs.Int("hours", 12, "summary window in hours")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	_, snap, err := loadSnapshot(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	since := time.Now().Add(-time.Duration(*hours) * time.Hour)

	// 夜间摘要：activity 时间窗投影。
	var delivered, created int
	createdBy := map[string]int{}
	if acts, err := gityaml.Open(fs.Dir()).ListActivity(); err == nil {
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
	}

	// 等 human 三类（无人值守的正确终局 = 可见的等待）。
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
	news := ""
	if n := createdBy["feature"] + createdBy["requirement"]; n > 0 {
		news = fmt.Sprintf("（⚠ feature/requirement %d —— 巡航红线，晨间核查）", n)
	}
	if *asJSON {
		printJSON(stdout, map[string]any{
			"window_hours": *hours, "delivered": delivered, "created": created,
			"created_by_type": createdBy, "redline_note": news,
			"waiting_decide": waitDecide, "waiting_ack": waitAck, "waiting_arbitration": waitArb,
		})
		return 0
	}
	fmt.Fprintf(stdout, "== 巡航摘要（近 %dh）==\n", *hours)
	fmt.Fprintf(stdout, "交付 %d 单｜新增 %d 单%s\n", delivered, created, news)
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
