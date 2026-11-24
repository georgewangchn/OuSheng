package main

import (
	"fmt"
	"io"
	"time"

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
		state, stale := effectivePresence(p)
		fmt.Fprintf(stdout, "PM %s", p.State)
		if p.Since != "" {
			fmt.Fprintf(stdout, "（自 %s，声明者 %s）", model.PresenceSinceShort(p), orDash(p.Actor))
		} else {
			fmt.Fprintf(stdout, "（未声明——悲观默认）")
		}
		if p.State == model.PresencePresent && state == model.PresenceAway {
			fmt.Fprintf(stdout, " ——⚠ 声明已失效（%s）→ 按 away 行事（在场请重新 ousheng pm present）", presenceExpiryNote(stale))
		}
		fmt.Fprintf(stdout, " ——%s\n", presenceGuidance(state))
		return 0
	default:
		fmt.Fprintf(stderr, "unknown pm subcommand: %s\n", args[0])
		return 2
	}
}

// presenceValidHours：present 声明的有效期（小时）。在场度是「此刻可期待分钟级
// 响应」的新鲜度敏感事实（档案 §34：决策响应预期）——过期声明回到悲观默认
// away，与「未声明=away」同一原理（F1：陈旧的「ok」不算 ok）。
//
// 判据（档案 §56）：「present 夜离场必 away」纯纪律 + ignite 提醒两道都没拦住
// ——11-13→16 挂三天（§50-③）、11-21→24 再挂三天四小时，同型复发。过期即
// 悲观：错判成本不对称（过期当 present = 全队问完等烧 token 数日；过期当
// away = agent 把 Q: 攒批晚几分钟拿到答复）。
const presenceValidHours = 12

// effectivePresence 是在场度生效态的唯一判定点（pm status / plan·status 头行 /
// ignite 提醒全走这里——一语义一实现）：present 超过有效期 → away。
// 声明缺有效时间戳同样按 away——无法证明新鲜的「present」不是 present（C1 不确定即拒）。
func effectivePresence(p model.Presence) (state string, staleHours float64) {
	staleHours = presenceStaleHours(p.Since)
	if p.State != model.PresencePresent {
		return p.State, staleHours
	}
	if _, err := time.Parse(time.RFC3339, p.Since); err != nil {
		return model.PresenceAway, 0
	}
	if staleHours > presenceValidHours {
		return model.PresenceAway, staleHours
	}
	return p.State, staleHours
}

// presenceExpiryNote：present 声明为何不生效（人话，三处消费同文案）。
func presenceExpiryNote(staleHours float64) string {
	if staleHours <= 0 {
		return "声明缺有效时间戳"
	}
	return fmt.Sprintf("已挂 %.0f 小时，超过 %d 小时有效期", staleHours, presenceValidHours)
}

// presenceGuidance 返回该在场度下 agent 的门行为一句话（plan/status 复用）。
func presenceGuidance(state string) string {
	if state == model.PresencePresent {
		return "present：单内 Q: 可期待分钟级回复（问完等）；门响应快，可适度即时交互"
	}
	return "away：遇 human 门挂起转下一活；问题攒批（单内 evidence Q:，晨间批量）；不空等不空转"
}

// presenceLine 是 cruise plan/status 头部的在场度行（数据事实+纪律提示）。
// present 过期时显式说明「声明过期按 away」——pm 一眼知道该重新声明。
func presenceLine(repo *gityaml.Repo) string {
	p := repo.LoadPresence()
	state, stale := effectivePresence(p)
	since := "未声明"
	if p.Since != "" {
		since = "自 " + model.PresenceSinceShort(p)
	}
	line := fmt.Sprintf("PM %s（%s）", p.State, since)
	if p.State == model.PresencePresent && state == model.PresenceAway {
		line += fmt.Sprintf("——⚠ %s，声明过期按 away 行事（在场请重新声明）", presenceExpiryNote(stale))
	}
	return line + "——" + presenceGuidance(state)
}
