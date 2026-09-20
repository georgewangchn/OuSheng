package main

import (
	"fmt"
	"io"
	"strings"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

// cmdDesign 共识层 CLI（v0.4 §4.6：绳只做发现、注入、生命周期；
// 内容生成不经绳——design.md/round 由人和 agent 直接写）。
func cmdDesign(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng design <list|show|decide|supersede|withdraw>")
		return 2
	}
	switch args[0] {
	case "list":
		return designList(args[1:], stdout, stderr)
	case "show":
		return designShow(args[1:], stdout, stderr)
	case "decide":
		return designDecide(args[1:], stdout, stderr)
	case "supersede":
		return designSupersede(args[1:], stdout, stderr)
	case "withdraw":
		return designWithdraw(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: ousheng design <list|show|decide|supersede|withdraw>")
		return 2
	}
}

func designList(args []string, stdout, stderr io.Writer) int {
	fs := newFS("design list")
	status := fs.String("status", "", "filter: draft|agreed|superseded|withdrawn")
	waitingFor := fs.String("waiting-for", "", "filter: draft designs awaiting this actor's round input")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	repo := gityaml.Open(fs.Dir())
	designs, err := repo.ListDesigns()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, d := range designs {
		if *status != "" && string(d.Design.Status) != *status {
			continue
		}
		if *waitingFor != "" {
			if d.Design.Status != model.DesignDraft || d.Design.Owner == *waitingFor {
				continue
			}
			spoken := false
			for _, a := range d.LatestSpeakers {
				if a == *waitingFor {
					spoken = true
				}
			}
			if spoken {
				continue
			}
		}
		fmt.Fprintf(stdout, "%-24s %-11s %-16s %-24s %s\n",
			d.Topic, d.Design.Status, d.Design.Owner,
			strings.Join(d.Design.Systems, ","), strings.Join(d.Design.RelatedItems, ","))
	}
	return 0
}

func designShow(args []string, stdout, stderr io.Writer) int {
	fs := newFS("design show")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng design show <topic>")
		return 2
	}
	repo := gityaml.Open(fs.Dir())
	topic := fs.Arg(0)
	d, body, err := repo.GetDesignRaw(topic)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	out, err := model.EncodeDesignDoc(d, body)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stdout.Write(out)
	return 0
}

// humanActor 校验 actor 已注册且为 human 型（decide/supersede 门，C2 血统）。
// 注册表为空时拒绝（与 bootstrap 弱介入不同：拍板门没有弱形态）。
func humanActor(repo *gityaml.Repo, id string) error {
	actors, err := repo.ListActors()
	if err != nil {
		return err
	}
	for _, af := range actors {
		if af.Actor.ID == id {
			if af.Actor.Type != model.ActorHuman {
				return fmt.Errorf("actor %q is %s, must be human (design decide/supersede = human 门)", id, af.Actor.Type)
			}
			return nil
		}
	}
	return fmt.Errorf("unknown actor %q", id)
}

func designDecide(args []string, stdout, stderr io.Writer) int {
	fs := newFS("design decide")
	actor := fs.String("actor", "", "deciding human")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 || *actor == "" {
		fmt.Fprintln(stderr, "usage: ousheng design decide <topic> --actor <human>")
		return 2
	}
	repo := gityaml.Open(fs.Dir())
	if err := humanActor(repo, *actor); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	topic := fs.Arg(0)
	d, body, err := repo.GetDesignRaw(topic)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if d.Status != model.DesignDraft {
		fmt.Fprintf(stderr, "design %q is %s, only draft can be decided\n", topic, d.Status)
		return 1
	}
	d.Status = model.DesignAgreed
	d.DecidedBy = *actor
	d.DecidedAt = model.Now()
	acts := []model.Activity{{
		TS: model.Now(), Actor: *actor, Action: "design_decided", WorkItem: topic,
		Detail: "draft → agreed",
	}}
	if err := repo.UpdateDesign(topic, d, body, acts, "design: decide "+topic); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "design %s decided by %s\n", topic, *actor)
	return 0
}

func designSupersede(args []string, stdout, stderr io.Writer) int {
	fs := newFS("design supersede")
	actor := fs.String("actor", "", "superseding human")
	by := fs.String("by", "", "successor topic")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 || *actor == "" || *by == "" {
		fmt.Fprintln(stderr, "usage: ousheng design supersede <topic> --by <new-topic> --actor <human>")
		return 2
	}
	repo := gityaml.Open(fs.Dir())
	if err := humanActor(repo, *actor); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	topic := fs.Arg(0)
	if topic == *by {
		fmt.Fprintln(stderr, "design cannot supersede itself")
		return 1
	}
	// 继任者必须存在（其是否已拍板由 converge 链检查曝光——「未生效」warning）
	if _, _, err := repo.GetDesignRaw(*by); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	d, body, err := repo.GetDesignRaw(topic)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if d.Status != model.DesignAgreed {
		fmt.Fprintf(stderr, "design %q is %s, only agreed can be superseded\n", topic, d.Status)
		return 1
	}
	d.Status = model.DesignSuperseded
	d.SupersededBy = *by
	acts := []model.Activity{{
		TS: model.Now(), Actor: *actor, Action: "design_superseded", WorkItem: topic,
		Detail: "agreed → superseded by " + *by,
	}}
	if err := repo.UpdateDesign(topic, d, body, acts, "design: supersede "+topic+" by "+*by); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "design %s superseded by %s\n", topic, *by)
	return 0
}

// designWithdraw：draft → withdrawn（2026-09-20 PM 撤回事故补的终态）。
//
// 门（镜像 F4 哲学）：human 手不卡（可撤任意 draft）；agent 只能撤自己的
// （收回自己的提案非跨主体伤害）；非 owner 的 agent 撤别人的 draft = 拒。
// 与 decide/supersede 不同——这不是拍板门：draft 从未获得权威，无共识可转移，
// 故 owner 即可操作。agreed 的关闭仍走 supersede（继任者 + human 门）。
func designWithdraw(args []string, stdout, stderr io.Writer) int {
	fs := newFS("design withdraw")
	actor := fs.String("actor", "", "withdrawing actor (owner or any human)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 || *actor == "" {
		fmt.Fprintln(stderr, "usage: ousheng design withdraw <topic> --actor <owner-or-human>")
		return 2
	}
	repo := gityaml.Open(fs.Dir())
	actors, err := repo.ListActors()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var typ model.ActorType
	found := false
	for _, af := range actors {
		if af.Actor.ID == *actor {
			typ, found = af.Actor.Type, true
			break
		}
	}
	if !found {
		fmt.Fprintf(stderr, "unknown actor %q\n", *actor)
		return 1
	}
	topic := fs.Arg(0)
	d, body, err := repo.GetDesignRaw(topic)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if d.Status != model.DesignDraft {
		fmt.Fprintf(stderr, "design %q is %s, only draft can be withdrawn (agreed 走 supersede)\n", topic, d.Status)
		return 1
	}
	if typ != model.ActorHuman && *actor != d.Owner {
		fmt.Fprintf(stderr, "agent %q can only withdraw own draft (owner is %q) — human 可撤任意\n", *actor, d.Owner)
		return 1
	}
	d.Status = model.DesignWithdrawn
	d.WithdrawnBy = *actor
	d.WithdrawnAt = model.Now()
	acts := []model.Activity{{
		TS: model.Now(), Actor: *actor, Action: "design_withdrawn", WorkItem: topic,
		Detail: "draft → withdrawn",
	}}
	if err := repo.UpdateDesign(topic, d, body, acts, "design: withdraw "+topic); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "design %s withdrawn (by %s) — 档案保留，不再注入评审面\n", topic, *actor)
	return 0
}
