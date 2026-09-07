package main

import (
	"fmt"
	"io"
	"strings"

	"ousheng/internal/context"
	"ousheng/internal/model"
	"ousheng/internal/projection"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/workspace"
)

func ctxService(dir string) (*context.Service, error) {
	return context.New(gityaml.Open(dir))
}

func cmdContext(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng context <me|actor|system> ...")
		return 2
	}
	switch args[0] {
	case "me":
		fs := newFS("context me")
		actor := fs.String("actor", "", "actor id")
		asJSON := fs.Bool("json", true, "output JSON (agent-friendly)")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if *actor == "" {
			fmt.Fprintln(stderr, "--actor required (or set OUSHENG_ACTOR)")
			return 2
		}
		c, err := ctxService(fs.Dir())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		mc, err := c.GetMyContext(*actor)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *asJSON {
			printJSON(stdout, mc)
		} else {
			renderMyContext(stdout, mc)
		}
		return 0

	case "actor":
		fs := newFS("context actor")
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng context actor <id>")
			return 2
		}
		c, err := ctxService(fs.Dir())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		v, err := c.GetActorContext(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *asJSON {
			printJSON(stdout, v)
		} else {
			renderActorView(stdout, v)
		}
		return 0

	case "system":
		fs := newFS("context system")
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng context system <id>")
			return 2
		}
		c, err := ctxService(fs.Dir())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		v, err := c.GetSystemContext(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *asJSON {
			printJSON(stdout, v)
		} else {
			renderSystemView(stdout, v)
		}
		return 0
	}
	fmt.Fprintln(stderr, "unknown context subcommand:", args[0])
	return 2
}

func renderMyContext(w io.Writer, c *context.MyContext) {
	fmt.Fprintf(w, "actor: %s (%s)\n", c.Actor, c.ActorType)
	if c.DisplayName != "" {
		printKV(w, "display_name", c.DisplayName)
	}
	if c.ResponsibleHuman != "" {
		printKV(w, "responsible_human", c.ResponsibleHuman)
	}
	if len(c.Roles) > 0 {
		printKV(w, "roles", strings.Join(c.Roles, ", "))
	}
	if len(c.Systems) > 0 {
		printKV(w, "systems", strings.Join(c.Systems, ", "))
	}
	if c.TargetVersion != "" {
		printKV(w, "target_version", c.TargetVersion)
	}
	if len(c.ActiveWork) > 0 {
		fmt.Fprintln(w, "active_work:")
		for _, wv := range c.ActiveWork {
			line := fmt.Sprintf("  - %s  %s  [%s]", wv.ID, wv.Status, wv.Title)
			if wv.ProgressValue > 0 {
				line += fmt.Sprintf("  %d%% reported", int(wv.ProgressValue*100))
			}
			fmt.Fprintln(w, line)
		}
	}
	if len(c.Blockers) > 0 {
		fmt.Fprintln(w, "blockers:")
		for _, b := range c.Blockers {
			fmt.Fprintf(w, "  - %s\n", b)
		}
	}
}

func renderActorView(w io.Writer, v *context.ActorView) {
	renderMyContext(w, &v.MyContext)
	if len(v.Agents) > 0 {
		printKV(w, "agents", strings.Join(v.Agents, ", "))
	}
	if len(v.ResponsibleSystems) > 0 {
		printKV(w, "responsible_systems", strings.Join(v.ResponsibleSystems, ", "))
	}
	if len(v.AccountableFor) > 0 {
		fmt.Fprintln(w, "accountable_for:")
		for _, wv := range v.AccountableFor {
			fmt.Fprintf(w, "  - %s  %s  [%s]\n", wv.ID, wv.Status, wv.Title)
		}
	}
	if len(v.BlockedWork) > 0 {
		fmt.Fprintln(w, "blocked_work:")
		for _, wv := range v.BlockedWork {
			fmt.Fprintf(w, "  - %s  [%s]\n", wv.ID, wv.Title)
		}
	}
}

func renderSystemView(w io.Writer, v *context.SystemView) {
	fmt.Fprintf(w, "system: %s", v.System)
	if v.Name != "" {
		fmt.Fprintf(w, " (%s)", v.Name)
	}
	fmt.Fprintln(w)
	if v.Parent != "" {
		printKV(w, "parent", v.Parent)
	}
	for _, r := range v.Responsible {
		fmt.Fprintf(w, "  responsible: %s / %s\n", r.Actor, r.Role)
	}
	for _, e := range v.Executors {
		fmt.Fprintf(w, "  executor:    %s / %s\n", e.Actor, e.Role)
	}
	if len(v.ActiveWork) > 0 {
		fmt.Fprintln(w, "active_work:")
		for _, wv := range v.ActiveWork {
			fmt.Fprintf(w, "  - %s  %s  [%s]\n", wv.ID, wv.Status, wv.Title)
		}
	}
	fmt.Fprintf(w, "open_bugs: %d\n", v.OpenBugs)
	if len(v.Blockers) > 0 {
		printKV(w, "blockers", strings.Join(v.Blockers, ", "))
	}
}

// --- registry list/show ---

func cmdActor(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng actor <list|show>")
		return 2
	}
	fs := newFS("actor " + args[0])
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch args[0] {
	case "list":
		for _, af := range c.Idx.Actors() {
			line := fmt.Sprintf("%-16s %-6s %s", af.Actor.ID, af.Actor.Type, af.Actor.DisplayName)
			if af.Actor.ResponsibleHuman != "" {
				line += "  → " + af.Actor.ResponsibleHuman
			}
			fmt.Fprintln(stdout, line)
		}
		return 0
	case "show":
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng actor show <id>")
			return 2
		}
		v, err := c.GetActorContext(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		renderActorView(stdout, v)
		return 0
	}
	fmt.Fprintln(stderr, "unknown actor subcommand:", args[0])
	return 2
}

func cmdSystem(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng system <list|show>")
		return 2
	}
	fs := newFS("system " + args[0])
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch args[0] {
	case "list":
		for _, s := range c.Idx.Systems() {
			line := fmt.Sprintf("%-18s %s", s.ID, s.Name)
			if s.Parent != "" {
				line += "  (parent: " + s.Parent + ")"
			}
			fmt.Fprintln(stdout, line)
		}
		return 0
	case "show":
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng system show <id>")
			return 2
		}
		v, err := c.GetSystemContext(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		renderSystemView(stdout, v)
		return 0
	}
	fmt.Fprintln(stderr, "unknown system subcommand:", args[0])
	return 2
}

func cmdAssignment(args []string, stdout, stderr io.Writer) int {
	fs := newFS("assignment list")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, a := range c.Idx.AssignmentsByActor("") {
		_ = a
	}
	// AssignmentsByActor("") 为空映射；直接从快照遍历
	for _, a := range assignmentsOf(c) {
		mark := " "
		if a.Active {
			mark = "*"
		}
		fmt.Fprintf(stdout, "%s %-16s %-8s %-18s %s\n", mark, a.Actor, a.Role, a.System, a.Responsibility)
	}
	return 0
}

func assignmentsOf(c *context.Service) []model.Assignment {
	var out []model.Assignment
	for _, actor := range c.Idx.Actors() {
		out = append(out, c.Idx.AssignmentsByActor(actor.Actor.ID)...)
	}
	return out
}

// --- view ---

func cmdView(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng view <kanban|system|version|project>")
		return 2
	}
	fs := newFS("view " + args[0])
	version := fs.String("version", "", "target version filter")
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	p := projection.New(c)
	switch args[0] {
	case "kanban":
		projection.RenderKanban(stdout, p.Kanban())
		return 0
	case "project":
		projection.RenderProjectSummary(stdout, p.ProjectSummary())
		return 0
	case "version":
		if *version == "" {
			fmt.Fprintln(stderr, "--version required")
			return 2
		}
		projection.RenderVersionView(stdout, p.VersionView(*version))
		return 0
	case "system":
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng view system <id>")
			return 2
		}
		v, err := c.GetSystemContext(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		renderSystemView(stdout, v)
		return 0
	}
	fmt.Fprintln(stderr, "unknown view:", args[0])
	return 2
}

// --- activity ---

func cmdActivity(args []string, stdout, stderr io.Writer) int {
	fs := newFS("activity list")
	limit := fs.Int("limit", 50, "max entries")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	repo := gityaml.Open(fs.Dir())
	acts, err := repo.ListActivity()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	start := 0
	if len(acts) > *limit {
		start = len(acts) - *limit
	}
	for _, a := range acts[start:] {
		fmt.Fprintf(stdout, "%s  %-14s %-20s %-14s %s\n", a.TS, a.Actor, a.Action, a.WorkItem, a.Detail)
	}
	return 0
}

// --- evidence ---

func cmdEvidence(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng evidence <add|list>")
		return 2
	}
	switch args[0] {
	case "add":
		fs := newFS("evidence add")
		evType := fs.String("type", "", "evidence type (git_commit|pull_request|test_result|ci_run|deployment|log|manual_check|document)")
		locator := fs.String("locator", "", "external locator (commit hash, run id...)")
		source := fs.String("source", "", "source system")
		result := fs.String("result", "", "result (passed|failed|...)")
		note := fs.String("note", "", "note")
		expect := fs.Int("expect", -1, "expected revision (default: current)")
		actor := fs.String("actor", "", "acting actor")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if fs.NArg() < 1 || *evType == "" || *locator == "" {
			fmt.Fprintln(stderr, "usage: ousheng evidence add <work-id> --type T --locator L [...]")
			return 2
		}
		svc := workspace.New(gityaml.Open(fs.Dir()))
		ev := model.Evidence{
			Type: model.EvidenceType(*evType), Source: *source,
			Locator: *locator, Result: *result, Note: *note,
			ObservedAt: model.Now(),
		}
		// git_commit：经 adapter 验证存在性并回填 subject
		if ev.Type == model.EvidenceGitCommit {
			verified, err := gitAdapterEvidence(fs.Dir(), *locator)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if verified != nil {
				if ev.Note == "" {
					ev.Note = verified.Note
				}
				ev.Source = "git"
			}
		}
		w, err := updateWithAutoExpect(svc, fs.Arg(0), *expect, func(cur model.WorkItem) model.WorkItem {
			cur.Evidence = append(cur.Evidence, ev)
			return cur
		}, "evidence", *actor)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "evidence added to %s (revision %d)\n", w.ID, w.Revision)
		return 0

	case "list":
		fs := newFS("evidence list")
		if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
			return usageErr(stderr, err)
		}
		if fs.NArg() < 1 {
			fmt.Fprintln(stderr, "usage: ousheng evidence list <work-id>")
			return 2
		}
		c, err := ctxService(fs.Dir())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		evs, err := c.GetEvidence(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(evs) == 0 {
			fmt.Fprintln(stdout, "(no evidence)")
			return 0
		}
		for _, ev := range evs {
			fmt.Fprintf(stdout, "%-12s %-10s %s", ev.Type, ev.Result, ev.Locator)
			if ev.ObservedAt != "" {
				fmt.Fprintf(stdout, "  @%s", ev.ObservedAt)
			}
			if ev.Note != "" {
				fmt.Fprintf(stdout, "  %s", ev.Note)
			}
			fmt.Fprintln(stdout)
		}
		return 0
	}
	fmt.Fprintln(stderr, "unknown evidence subcommand:", args[0])
	return 2
}

// updateWithAutoExpect: expect<0 时自动取当前 revision（读-改-写，竞争仍触发 CAS 冲突）。
func updateWithAutoExpect(svc *workspace.Service, id string, expect int, mutate func(model.WorkItem) model.WorkItem, kind string, actor string) (model.WorkItem, error) {
	cur, err := svc.Repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	if expect < 0 {
		expect = cur.Revision
	}
	next := mutate(cur)
	switch kind {
	case "evidence":
		if len(next.Evidence) > 0 {
			last := next.Evidence[len(next.Evidence)-1]
			acts := []model.Activity{{
				TS: model.Now(), Actor: actor, Action: "evidence_added", WorkItem: id,
				Detail: fmt.Sprintf("%s %s", last.Type, last.Locator),
			}}
			return svc.Repo.UpdateWorkItem(next, expect, acts, fmt.Sprintf("work: evidence %s %s", id, last.Type))
		}
	case "assign":
		return svc.Update(next, expect, actor)
	default:
		return svc.Update(next, expect, actor)
	}
	return model.WorkItem{}, fmt.Errorf("unreachable")
}
