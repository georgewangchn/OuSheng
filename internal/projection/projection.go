// Package projection 实现 Board / 视图投影（v0.3 §20–§24）。
//
// Board 是 Projection，不是事实源：删除输出不影响 canonical state，
// 重新投影即可完整恢复（成功标准 S4）。
package projection

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"ousheng/internal/context"
	"ousheng/internal/model"
)

type Service struct {
	Ctx *context.Service
}

func New(c *context.Service) *Service { return &Service{Ctx: c} }

// --- Kanban（§21）---

type KanbanCard struct {
	ID            string
	Title         string
	System        string
	SystemName    string
	TargetVersion string
	Priority      string
	DueOn         string
	Contract      string
	Role          string
	RoleName      string
	Actor         string
	Human         string
	HumanName     string
	Progress      string // "70% reported (implementation-checklist)"；空=未报告
	Status        string
	Blockers      []string
}

type KanbanColumn struct {
	Status model.WorkStatus
	Cards  []KanbanCard
}

var kanbanOrder = []model.WorkStatus{
	model.StatusBacklog, model.StatusReady, model.StatusDoing,
	model.StatusBlocked, model.StatusTesting, model.StatusDone, model.StatusCancelled,
}

func (s *Service) Kanban() ([]KanbanColumn, error) {
	sysName := func(id string) string {
		if sys, ok, err := s.Ctx.Idx.System(id); err == nil && ok {
			return sys.Name
		}
		return ""
	}
	roleName := func(id string) string {
		roles, err := s.Ctx.Idx.Roles()
		if err != nil {
			return ""
		}
		for _, r := range roles {
			if r.ID == id {
				return r.Name
			}
		}
		return ""
	}
	humanName := func(id string) string {
		if a, ok, err := s.Ctx.Idx.Actor(id); err == nil && ok && a.Actor.DisplayName != "" {
			return a.Actor.DisplayName
		}
		return ""
	}
	cols := map[model.WorkStatus][]KanbanCard{}
	all, err := s.Ctx.Idx.All()
	if err != nil {
		return nil, err
	}
	for _, w := range all {
		var blockers []string
		bs, err := s.Ctx.Idx.BlockersOf(w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			blockers = append(blockers, b.DepID)
		}
		card := KanbanCard{
			ID: w.ID, Title: w.Title,
			System: w.System, SystemName: sysName(w.System),
			TargetVersion: w.TargetVersion,
			Priority:      w.Priority,
			DueOn:         w.DueOn,
			Role:          w.ActingRole, RoleName: roleName(w.ActingRole),
			Actor: w.Assignee,
			Human: w.AccountableHuman, HumanName: humanName(w.AccountableHuman),
			Status:   string(w.Status),
			Blockers: blockers,
		}
		if w.Progress != nil {
			card.Progress = fmt.Sprintf("%d%% reported (%s)", int(w.Progress.Value*100), w.Progress.Basis)
		}
		if w.Contract != nil {
			card.Contract = w.Contract.Kind + "/" + string(w.Contract.Status)
			if w.Contract.Breaking {
				card.Contract += " (breaking)"
			}
		}
		cols[w.Status] = append(cols[w.Status], card)
	}
	// 列内按优先级排序（P0 在前，未定级殿后，再按 ID 稳定）。
	// 这是呈现人的排序决策（拍板的刻度），不是调度器——AI 拿到什么单仍由人指派。
	rank := func(p string) int {
		switch p {
		case "P0":
			return 0
		case "P1":
			return 1
		case "P2":
			return 2
		case "P3":
			return 3
		}
		return 4
	}
	for st := range cols {
		c := cols[st]
		sort.SliceStable(c, func(i, j int) bool {
			ri, rj := rank(c[i].Priority), rank(c[j].Priority)
			if ri != rj {
				return ri < rj
			}
			return c[i].ID < c[j].ID
		})
	}
	var out []KanbanColumn
	for _, st := range kanbanOrder {
		if cards := cols[st]; len(cards) > 0 {
			out = append(out, KanbanColumn{Status: st, Cards: cards})
		}
	}
	return out, nil
}

// RenderKanban 输出文本看板。Progress 明确标注 reported（§21）。
func RenderKanban(w io.Writer, cols []KanbanColumn) {
	if len(cols) == 0 {
		fmt.Fprintln(w, "(no work items)")
		return
	}
	for _, col := range cols {
		fmt.Fprintf(w, "== %s (%d) ==\n", strings.ToUpper(string(col.Status)), len(col.Cards))
		for _, c := range col.Cards {
			fmt.Fprintf(w, "[%s] %s\n", c.ID, c.Title)
			if c.System != "" {
				line := "  System    " + c.System
				if c.SystemName != "" {
					line += " (" + c.SystemName + ")"
				}
				fmt.Fprintln(w, line)
			}
			if c.TargetVersion != "" {
				fmt.Fprintf(w, "  Version   %s\n", c.TargetVersion)
			}
			if c.Priority != "" {
				fmt.Fprintf(w, "  Priority  %s\n", c.Priority)
			}
		if c.DueOn != "" {
			fmt.Fprintf(w, "  Due       %s\n", c.DueOn)
		}
		if c.Contract != "" {
			fmt.Fprintf(w, "  Contract  %s\n", c.Contract)
		}
			if c.Role != "" {
				line := "  Role      " + c.Role
				if c.RoleName != "" {
					line += " (" + c.RoleName + ")"
				}
				fmt.Fprintln(w, line)
			}
			if c.Actor != "" {
				fmt.Fprintf(w, "  Actor     %s\n", c.Actor)
			}
			if c.Human != "" {
				line := "  Human     " + c.Human
				if c.HumanName != "" {
					line += " (" + c.HumanName + ")"
				}
				fmt.Fprintln(w, line)
			}
			if c.Progress != "" {
				fmt.Fprintf(w, "  Progress  %s\n", c.Progress)
			}
			if len(c.Blockers) > 0 {
				fmt.Fprintf(w, "  Blocker   %s\n", strings.Join(c.Blockers, ", "))
			}
		}
		fmt.Fprintln(w)
	}
}

// --- Version View（§24）---

type SystemVersionBlock struct {
	System  string
	Version string
	Counts  map[string]int // status -> count
	// Reported Progress：work -> "70% (basis)"；不合成虚假总百分比（§19）
	ReportedProgress []ProgressLine
	Blockers         []string
}

type ProgressLine struct {
	Work  string
	Value string // "70% (implementation-checklist)"
}

func (s *Service) VersionView(version string) ([]SystemVersionBlock, error) {
	bySystem := map[string]*SystemVersionBlock{}
	var order []string
	versionWork, err := s.Ctx.Idx.ByVersion(version)
	if err != nil {
		return nil, err
	}
	for _, w := range versionWork {
		key := w.System
		if key == "" {
			key = "(unassigned)"
		}
		blk, ok := bySystem[key]
		if !ok {
			blk = &SystemVersionBlock{System: key, Version: version, Counts: map[string]int{}}
			bySystem[key] = blk
			order = append(order, key)
		}
		blk.Counts[string(w.Status)]++
		if model.WorkItemActive(w.Status) && w.Progress != nil {
			blk.ReportedProgress = append(blk.ReportedProgress, ProgressLine{
				Work:  w.ID,
				Value: fmt.Sprintf("%d%% (%s)", int(w.Progress.Value*100), w.Progress.Basis),
			})
		}
		bs, err := s.Ctx.Idx.BlockersOf(w.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bs {
			blk.Blockers = appendUnique(blk.Blockers, b.DepID)
		}
	}
	sort.Strings(order)
	var out []SystemVersionBlock
	for _, k := range order {
		out = append(out, *bySystem[k])
	}
	return out, nil
}

func RenderVersionView(w io.Writer, blocks []SystemVersionBlock) {
	for _, b := range blocks {
		fmt.Fprintf(w, "%s / %s\n", b.System, b.Version)
		fmt.Fprintf(w, "  WorkItems: %s\n", formatCounts(b.Counts))
		if len(b.ReportedProgress) > 0 {
			fmt.Fprintln(w, "  Reported Progress:")
			for _, p := range b.ReportedProgress {
				fmt.Fprintf(w, "    %s  %s\n", p.Work, p.Value)
			}
		}
		if len(b.Blockers) > 0 {
			fmt.Fprintf(w, "  Blockers: %s\n", strings.Join(b.Blockers, ", "))
		}
		fmt.Fprintln(w)
	}
}

// --- Project Summary（Project View：只数数，不造假精度，§19）---

type ProjectSummary struct {
	Project  string
	Name     string
	Total    int
	Counts   map[string]int
	BySystem map[string]map[string]int
	Blockers []string
}

func (s *Service) ProjectSummary() (ProjectSummary, error) {
	sum := ProjectSummary{
		Project:  s.Ctx.Project.ID,
		Name:     s.Ctx.Project.Name,
		Counts:   map[string]int{},
		BySystem: map[string]map[string]int{},
	}
	blockerSet := map[string]bool{}
	all, err := s.Ctx.Idx.All()
	if err != nil {
		return sum, err
	}
	for _, w := range all {
		sum.Total++
		sum.Counts[string(w.Status)]++
		sys := w.System
		if sys == "" {
			sys = "(unassigned)"
		}
		if sum.BySystem[sys] == nil {
			sum.BySystem[sys] = map[string]int{}
		}
		sum.BySystem[sys][string(w.Status)]++
		bs, err := s.Ctx.Idx.BlockersOf(w.ID)
		if err != nil {
			return sum, err
		}
		for _, b := range bs {
			blockerSet[b.DepID] = true
		}
	}
	sum.Blockers = sortedStrings(blockerSet)
	return sum, nil
}

func RenderProjectSummary(w io.Writer, sum ProjectSummary) {
	title := sum.Project
	if sum.Name != "" {
		title += " (" + sum.Name + ")"
	}
	fmt.Fprintf(w, "%s\n", title)
	fmt.Fprintf(w, "  WorkItems: %d (%s)\n", sum.Total, formatCounts(sum.Counts))
	for _, sys := range sortedStringKeys(sum.BySystem) {
		fmt.Fprintf(w, "  %s: %s\n", sys, formatCounts(sum.BySystem[sys]))
	}
	if len(sum.Blockers) > 0 {
		fmt.Fprintf(w, "  Blockers: %s\n", strings.Join(sum.Blockers, ", "))
	}
}

func sortedStrings(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStringKeys(m map[string]map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func formatCounts(c map[string]int) string {
	var parts []string
	for _, st := range kanbanOrder {
		if n := c[string(st)]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", st, n))
		}
	}
	return strings.Join(parts, "  ")
}

func appendUnique(cur []string, v string) []string {
	for _, x := range cur {
		if x == v {
			return cur
		}
	}
	return append(cur, v)
}
