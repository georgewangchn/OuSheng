package main

import (
	"fmt"
	"io"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

// cmdPm：`ousheng pm <present|away|status>`——在场度声明（档案 §34）。
//
// 模式坍缩裁决：巡航/普通不是两种模式，是一个连续变量——决策响应预期。
// present=agent 遇 human 门可期待分钟级回复（问完等）；away=挂起转下一活，
// 问题攒批（单内 evidence Q:），不空等不空转不占 PM 通道。缺声明=away
// （悲观默认）。声明者必须 human（在场度是人的事实，agent 无权代declare）。
func cmdPm(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ousheng pm <present|away|status>")
		return 2
	}
	fs := newFS("pm " + args[0])
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	switch args[0] {
	case "present", "away":
		me := defaultActor(fs.Dir())
		if me == "" {
			fmt.Fprintln(stderr, "本机未声明身份（ousheng me <id>）——在场度由 human 座位声明")
			return 2
		}
		af, err := repo.GetActor(me)
		if err != nil {
			fmt.Fprintf(stderr, "unknown actor %q — 先注册\n", me)
			return 2
		}
		if af.Actor.Type != model.ActorHuman {
			fmt.Fprintf(stderr, "在场度只能由 human 声明（me=%s 是 %s）——agent 不代替人宣布自己在场\n", me, af.Actor.Type)
			return 2
		}
		state := model.PresencePresent
		if args[0] == "away" {
			state = model.PresenceAway
		}
		p := model.Presence{Actor: me, State: state, Since: model.Now()}
		if err := repo.SavePresence(p, "pm: presence "+state); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "PM %s（自 %s）——%s\n", state, model.PresenceSinceShort(p),
			presenceGuidance(state))
		return 0
	case "status":
		p := repo.LoadPresence()
		fmt.Fprintf(stdout, "PM %s", p.State)
		if p.Since != "" {
			fmt.Fprintf(stdout, "（自 %s，声明者 %s）", model.PresenceSinceShort(p), orDash(p.Actor))
		} else {
			fmt.Fprintf(stdout, "（未声明——悲观默认）")
		}
		fmt.Fprintf(stdout, " ——%s\n", presenceGuidance(p.State))
		return 0
	default:
		fmt.Fprintf(stderr, "unknown pm subcommand: %s\n", args[0])
		return 2
	}
}

// presenceGuidance 返回该在场度下 agent 的门行为一句话（plan/status 复用）。
func presenceGuidance(state string) string {
	if state == model.PresencePresent {
		return "present：单内 Q: 可期待分钟级回复（问完等）；门响应快，可适度即时交互"
	}
	return "away：遇 human 门挂起转下一活；问题攒批（单内 evidence Q:，晨间批量）；不空等不空转"
}

// presenceLine 是 cruise plan/status 头部的在场度行（数据事实+纪律提示）。
func presenceLine(repo *gityaml.Repo) string {
	p := repo.LoadPresence()
	since := ""
	if p.Since != "" {
		since = "自 " + model.PresenceSinceShort(p) + " "
	}
	return fmt.Sprintf("PM %s（%s）——%s", p.State, since, presenceGuidance(p.State))
}
