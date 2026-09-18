package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/workspace"
)

func workService(dir string) *workspace.Service {
	return workspace.New(gityaml.Open(dir))
}

func cmdWork(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: ousheng work <list|show|create|update|assign>")
		return 2
	}
	switch args[0] {
	case "list":
		return workList(args[1:], stdout, stderr)
	case "show":
		return workShow(args[1:], stdout, stderr)
	case "create":
		return workCreate(args[1:], stdout, stderr)
	case "update":
		return workUpdate(args[1:], stdout, stderr)
	case "assign":
		return workAssign(args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, "unknown work subcommand:", args[0])
	return 2
}

func workList(args []string, stdout, stderr io.Writer) int {
	fs := newFS("work list")
	system := fs.String("system", "", "filter system")
	status := fs.String("status", "", "filter status")
	assignee := fs.String("assignee", "", "filter assignee")
	version := fs.String("version", "", "filter target_version")
	wtype := fs.String("type", "", "filter type")
	open := fs.Bool("open", false, "only open (not done/cancelled)")
	unassigned := fs.Bool("unassigned", false, "only items without assignee（PM 巡检：待认领池）")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	items, err := c.Idx.All()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, "ID               STATUS     SYSTEM             ASSIGNEE         PRI    DUE         CT           TITLE")
	for _, w := range items {
		if *system != "" && w.System != *system {
			continue
		}
		if *status != "" && string(w.Status) != *status {
			continue
		}
		if *assignee != "" && w.Assignee != *assignee {
			continue
		}
		if *version != "" && w.TargetVersion != *version {
			continue
		}
		if *wtype != "" && string(w.Type) != *wtype {
			continue
		}
		if *open && !model.WorkItemOpen(w.Status) {
			continue
		}
		if *unassigned && (w.Assignee != "" || !model.WorkItemOpen(w.Status)) {
			continue // 待认领池只装 open 的无主单（关闭态不是可认领对象）
		}
		ct := ""
		if w.Contract != nil {
			ct = w.Contract.Kind + "/" + string(w.Contract.Status)
		}
		fmt.Fprintf(stdout, "%-16s %-10s %-18s %-16s %-6s %-11s %-12s %s\n",
			w.ID, w.Status, w.System, w.Assignee, w.Priority, w.DueOn, ct, w.Title)
	}
	return 0
}

func workShow(args []string, stdout, stderr io.Writer) int {
	fs := newFS("work show")
	asJSON := fs.Bool("json", false, "output JSON")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng work show <id>")
		return 2
	}
	c, err := ctxService(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	d, err := c.GetWorkItem(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *asJSON {
		printJSON(stdout, d)
		return 0
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, string(b))
	if len(d.Deps) > 0 {
		fmt.Fprintln(stdout, "deps:")
		for _, dep := range d.Deps {
			fmt.Fprintf(stdout, "  - %s  %s\n", dep.ID, dep.Status)
		}
	}
	return 0
}

func workCreate(args []string, stdout, stderr io.Writer) int {
	fs := newFS("work create")
	file := fs.String("file", "", "work item yaml file")
	id := fs.String("id", "", "work item id (UPPER-xxx style)")
	wtype := fs.String("type", "task", "type: requirement|feature|bug|task|test|deployment|release")
	title := fs.String("title", "", "title")
	system := fs.String("system", "", "system id")
	version := fs.String("version", "", "target version")
	assignee := fs.String("assignee", "", "assignee actor id")
	role := fs.String("role", "", "acting role id")
	accountable := fs.String("accountable", "", "accountable human id")
	detectedBy := fs.String("detected-by", "", "detected-by actor id (bug)")
	dependsOn := fs.String("depends-on", "", "comma-separated dependency ids")
	priority := fs.String("priority", "", "P0..P3")
	due := fs.String("due", "", "due date YYYY-MM-DD")
	description := fs.String("description", "", "需求正文/验收标准")
	actor := fs.String("actor", "", "acting actor (activity attribution)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	if *actor == "" {
		*actor = defaultActor(fs.Dir())
	}

	var w model.WorkItem
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		w, err = model.DecodeWorkItem(raw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		if *id == "" || *title == "" {
			fmt.Fprintln(stderr, "--id and --title required (or use --file)")
			return 2
		}
		w = model.WorkItem{
			SchemaVersion:    2,
			ID:               *id,
			Type:             model.WorkItemType(*wtype),
			Title:            *title,
			System:           *system,
			TargetVersion:    *version,
			Priority:         *priority,
			DueOn:            *due,
			Description:      *description,
			Assignee:         *assignee,
			ActingRole:       *role,
			AccountableHuman: *accountable,
			DetectedBy:       *detectedBy,
		}
		if *dependsOn != "" {
			for _, d := range strings.Split(*dependsOn, ",") {
				if d = strings.TrimSpace(d); d != "" {
					w.DependsOn = append(w.DependsOn, d)
				}
			}
		}
	}

	created, err := workService(fs.Dir()).Create(w, *actor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "created %s (revision %d, status %s)\n", created.ID, created.Revision, created.Status)
	return 0
}

func workUpdate(args []string, stdout, stderr io.Writer) int {
	fs := newFS("work update")
	file := fs.String("file", "", "full work item yaml")
	status := fs.String("status", "", "new status")
	priority := fs.String("priority", "", "set priority P0..P3")
	due := fs.String("due", "", "set due date YYYY-MM-DD")
	description := fs.String("description", "", "set description")
	expect := fs.Int("expect", -1, "expected revision (required with --file)")
	actor := fs.String("actor", "", "acting actor")
	ack := fs.Bool("ack", false, "human 拍板：本条提交即 human_ack（approver=acting actor，C2）")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if *actor == "" {
		*actor = defaultActor(fs.Dir())
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng work update <id> (--file F --expect N [--ack] | --status S | --priority P | --due D | --description X)")
		return 2
	}
	id := fs.Arg(0)
	svc := workService(fs.Dir())

	if *file != "" {
		if *expect < 0 {
			fmt.Fprintln(stderr, "--file update requires explicit --expect (CAS)")
			return 2
		}
		if *ack && *actor == "" {
			fmt.Fprintln(stderr, "--ack requires --actor or ousheng me (ack 必须落到 human 身上)")
			return 2
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		w, err := model.DecodeWorkItem(raw)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if w.ID != id {
			fmt.Fprintf(stderr, "file id %q != argument id %q\n", w.ID, id)
			return 2
		}
		if *ack {
			w.HumanAck = &model.HumanAck{Approver: *actor, At: model.Now()}
		}
		updated, err := svc.Update(w, *expect, *actor)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "updated %s (revision %d, status %s)\n", updated.ID, updated.Revision, updated.Status)
		return 0
	}

	if *status == "" && *priority == "" && *due == "" && *description == "" {
		fmt.Fprintln(stderr, "nothing to update: pass --status/--priority/--due/--description or --file")
		return 2
	}
	updated, err := updateWithAutoExpect(svc, id, *expect, func(cur model.WorkItem) model.WorkItem {
		if *status != "" {
			cur.Status = model.WorkStatus(*status)
		}
		if *priority != "" {
			cur.Priority = *priority
		}
		if *due != "" {
			cur.DueOn = *due
		}
		if *description != "" {
			cur.Description = *description
		}
		return cur
	}, *actor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "updated %s (revision %d, status %s)\n", updated.ID, updated.Revision, updated.Status)
	return 0
}

func workAssign(args []string, stdout, stderr io.Writer) int {
	fs := newFS("work assign")
	assignee := fs.String("assignee", "", "assignee actor id")
	role := fs.String("role", "", "acting role id")
	expect := fs.Int("expect", -1, "expected revision")
	actor := fs.String("actor", "", "acting actor")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if *actor == "" {
		*actor = defaultActor(fs.Dir())
	}
	if fs.NArg() != 1 || *assignee == "" {
		fmt.Fprintln(stderr, "usage: ousheng work assign <id> --assignee A [--role R]")
		return 2
	}
	svc := workService(fs.Dir())
	updated, err := updateWithAutoExpect(svc, fs.Arg(0), *expect, func(cur model.WorkItem) model.WorkItem {
		cur.Assignee = *assignee
		if *role != "" {
			cur.ActingRole = *role
		}
		return cur
	}, *actor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "assigned %s -> %s (revision %d)\n", updated.ID, updated.Assignee, updated.Revision)
	return 0
}

func cmdBug(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "report" {
		fmt.Fprintln(stderr, "usage: ousheng bug report ...")
		return 2
	}
	fs := newFS("bug report")
	file := fs.String("file", "", "work item yaml file (type will be forced to bug)")
	id := fs.String("id", "", "bug id (BUG-xxx)")
	title := fs.String("title", "", "title")
	system := fs.String("system", "", "system id")
	version := fs.String("version", "", "target version")
	assignee := fs.String("assignee", "", "assignee actor id")
	role := fs.String("role", "", "acting role id")
	accountable := fs.String("accountable", "", "accountable human id")
	detectedBy := fs.String("detected-by", "", "who detected this bug")
	actor := fs.String("actor", "", "acting actor")
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	if *actor == "" {
		*actor = defaultActor(fs.Dir())
	}

	var w model.WorkItem
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if w, err = model.DecodeWorkItem(raw); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		if *id == "" || *title == "" {
			fmt.Fprintln(stderr, "--id and --title required (or use --file)")
			return 2
		}
		w = model.WorkItem{
			SchemaVersion: 2, ID: *id, Type: model.TypeBug, Title: *title,
			System: *system, TargetVersion: *version,
			Assignee: *assignee, ActingRole: *role, AccountableHuman: *accountable,
			DetectedBy: *detectedBy,
		}
	}
	w.Type = model.TypeBug
	if *detectedBy != "" {
		w.DetectedBy = *detectedBy
	}
	created, err := workService(fs.Dir()).Create(w, *actor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "reported %s (revision %d)\n", created.ID, created.Revision)
	return 0
}

func cmdProgress(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] != "report" {
		fmt.Fprintln(stderr, "usage: ousheng progress report <id> --value V --actor A --basis B")
		return 2
	}
	fs := newFS("progress report")
	value := fs.String("value", "", "progress value 0..1 (e.g. 0.7)")
	actor := fs.String("actor", "", "reporting actor")
	basis := fs.String("basis", "manual", "basis: manual|implementation-checklist|test-cases|subtasks|story-points|milestone")
	expect := fs.Int("expect", -1, "expected revision")
	if err := parseLoose(fs.FlagSet, args[1:]); err != nil {
		return usageErr(stderr, err)
	}
	if *actor == "" {
		*actor = defaultActor(fs.Dir())
	}
	if fs.NArg() != 1 || *value == "" || *actor == "" {
		fmt.Fprintln(stderr, "usage: ousheng progress report <id> --value 0.7 [--actor A] [--basis B]")
		return 2
	}
	v, err := strconv.ParseFloat(*value, 64)
	if err != nil || v < 0 || v > 1 {
		fmt.Fprintln(stderr, "--value must be within [0,1]")
		return 2
	}
	svc := workService(fs.Dir())
	cur, err := svc.Repo.GetWorkItem(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *expect < 0 {
		*expect = cur.Revision
	}
	p := model.ProgressReport{
		Value: v, Actor: *actor, ReportedAt: model.Now(), Basis: model.ProgressBasis(*basis),
	}
	updated, err := svc.ReportProgress(cur.ID, p, *expect)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "progress reported: %s %d%% (%s, revision %d)\n",
		updated.ID, int(v*100), p.Basis, updated.Revision)
	return 0
}
