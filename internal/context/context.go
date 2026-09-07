// Package context 实现 Agent Context（v0.3 §25/§26/§39，Phase 2 最高价值能力）。
//
// get_my_context 不是 SQL wrapper，而是"面向 Agent 下一步决策的最小上下文投影"：
// 少、相关、可追溯、不总结成幻觉、需要时可展开（Progressive Disclosure）。
//
// 第一层：GetMyContext —— actor/role/systems/version/active_work/blockers
// 第二层：GetWorkItem —— 单个 work item 全量 + 直接依赖
// 第三层：GetEvidence —— 证据明细
package context

import (
	"fmt"
	"sort"

	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/model"
	"ousheng/internal/state"
)

type Service struct {
	Repo    state.Repository
	Idx     index.Index
	Project model.Project
}

// New 装载 snapshot 并构建内存索引（小规模零基础设施，§30）。
func New(repo state.Repository) (*Service, error) {
	snap, err := index.Load(repo)
	if err != nil {
		return nil, err
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		return nil, err
	}
	return &Service{Repo: repo, Idx: idx, Project: snap.Project}, nil
}

// --- 第一层：最小上下文 ---

// MyContext 是 get_my_context 的返回（§26 示例形状）。
type MyContext struct {
	Actor            string        `json:"actor"`
	ActorType        string        `json:"actor_type"`
	DisplayName      string        `json:"display_name,omitempty"`
	ResponsibleHuman string        `json:"responsible_human,omitempty"`
	Roles            []string      `json:"roles,omitempty"`
	Systems          []string      `json:"systems,omitempty"`
	TargetVersion    string        `json:"target_version,omitempty"`
	ActiveWork       []WorkBrief   `json:"active_work,omitempty"`
	Blockers         []string      `json:"blockers,omitempty"`
}

// WorkBrief 是 active work 的最小条目；progress 标注 reported（§19/§21）。
type WorkBrief struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Status        string  `json:"status"`
	System        string  `json:"system,omitempty"`
	ProgressValue float64 `json:"progress_reported,omitempty"`
}

func (s *Service) GetMyContext(actorID string) (*MyContext, error) {
	af, ok := s.Idx.Actor(actorID)
	if !ok {
		return nil, fmt.Errorf("unknown actor %q (try: ousheng actor list)", actorID)
	}
	a := af.Actor

	ctx := &MyContext{
		Actor:            a.ID,
		ActorType:        string(a.Type),
		DisplayName:      a.DisplayName,
		ResponsibleHuman: a.ResponsibleHuman,
	}

	// 作用域：manifest（Agent 启动默认）∪ assignments（registry 事实）
	scope := map[string]bool{}
	roleSet := map[string]bool{}
	if af.Manifest != nil {
		if af.Manifest.DefaultRole != "" {
			roleSet[af.Manifest.DefaultRole] = true
		}
		for _, sys := range af.Manifest.Systems {
			scope[sys] = true
		}
		ctx.TargetVersion = af.Manifest.TargetVersion
	}
	for _, as := range s.Idx.AssignmentsByActor(actorID) {
		if !as.Active {
			continue
		}
		roleSet[as.Role] = true
		scope[as.System] = true
	}
	ctx.Roles = sortedKeys(roleSet)
	ctx.Systems = sortedKeys(scope)

	// active work：assignee 视角（agent/human 执行中的工作）
	blockerSet := map[string]bool{}
	for _, w := range s.Idx.ActiveByActor(actorID) {
		brief := WorkBrief{ID: w.ID, Title: w.Title, Status: string(w.Status), System: w.System}
		if w.Progress != nil {
			brief.ProgressValue = w.Progress.Value
		}
		ctx.ActiveWork = append(ctx.ActiveWork, brief)
		for _, b := range s.Idx.BlockersOf(w.ID) {
			blockerSet[b.DepID] = true
		}
	}
	if ctx.ActiveWork == nil {
		ctx.ActiveWork = []WorkBrief{}
	}
	ctx.Blockers = sortedKeys(blockerSet)
	return ctx, nil
}

// --- Actor View（§22）---

type ActorView struct {
	MyContext
	Agents            []string    `json:"agents,omitempty"`             // human：负责的 agents
	ResponsibleSystems []string   `json:"responsible_systems,omitempty"` // human：accountable 的 systems
	AccountableFor    []WorkBrief `json:"accountable_for,omitempty"`    // 问责中的工作
	BlockedWork       []WorkBrief `json:"blocked_work,omitempty"`       // 状态=blocked 的工作
}

func (s *Service) GetActorContext(actorID string) (*ActorView, error) {
	base, err := s.GetMyContext(actorID)
	if err != nil {
		return nil, err
	}
	v := &ActorView{MyContext: *base}

	if base.ActorType == string(model.ActorHuman) {
		agentSet := map[string]bool{}
		sysSet := map[string]bool{}
		for _, af := range s.Idx.Actors() {
			if af.Actor.Type == model.ActorAgent && af.Actor.ResponsibleHuman == actorID {
				agentSet[af.Actor.ID] = true
			}
		}
		for _, as := range s.Idx.AssignmentsByActor(actorID) {
			if as.Active && as.Responsibility == model.ResponsibilityAccountable {
				sysSet[as.System] = true
			}
		}
		v.Agents = sortedKeys(agentSet)
		v.ResponsibleSystems = sortedKeys(sysSet)
	}

	for _, w := range s.Idx.ByAccountable(actorID) {
		if !model.WorkItemActive(w.Status) {
			continue
		}
		brief := WorkBrief{ID: w.ID, Title: w.Title, Status: string(w.Status), System: w.System}
		if w.Progress != nil {
			brief.ProgressValue = w.Progress.Value
		}
		v.AccountableFor = append(v.AccountableFor, brief)
		if w.Status == model.StatusBlocked {
			v.BlockedWork = append(v.BlockedWork, brief)
		}
	}
	return v, nil
}

// --- System View（§23）---

type SystemView struct {
	System        string           `json:"system"`
	Name          string           `json:"name,omitempty"`
	Parent        string           `json:"parent,omitempty"`
	Responsible   []ActorInRole    `json:"responsible,omitempty"`  // accountable humans
	Executors     []ActorInRole    `json:"executors,omitempty"`    // 执行者（多为 agent）
	ActiveWork    []WorkBrief      `json:"active_work,omitempty"`
	OpenBugs      int              `json:"open_bugs"`
	Blockers      []string         `json:"blockers,omitempty"`     // 系统内未解除阻塞（去重）
}

type ActorInRole struct {
	Actor string `json:"actor"`
	Role  string `json:"role"`
}

func (s *Service) GetSystemContext(systemID string) (*SystemView, error) {
	sys, ok := s.Idx.System(systemID)
	if !ok {
		return nil, fmt.Errorf("unknown system %q (try: ousheng system list)", systemID)
	}
	v := &SystemView{System: sys.ID, Name: sys.Name, Parent: sys.Parent}

	for _, as := range s.Idx.AssignmentsBySystem(systemID) {
		if !as.Active {
			continue
		}
		pair := ActorInRole{Actor: as.Actor, Role: as.Role}
		if as.Responsibility == model.ResponsibilityAccountable {
			v.Responsible = append(v.Responsible, pair)
		} else {
			v.Executors = append(v.Executors, pair)
		}
	}

	blockerSet := map[string]bool{}
	for _, w := range s.Idx.BySystem(systemID) {
		if !model.WorkItemActive(w.Status) {
			continue
		}
		brief := WorkBrief{ID: w.ID, Title: w.Title, Status: string(w.Status), System: w.System}
		if w.Progress != nil {
			brief.ProgressValue = w.Progress.Value
		}
		v.ActiveWork = append(v.ActiveWork, brief)
		if w.Type == model.TypeBug && model.WorkItemOpen(w.Status) {
			v.OpenBugs++
		}
		for _, b := range s.Idx.BlockersOf(w.ID) {
			blockerSet[b.DepID] = true
		}
	}
	v.Blockers = sortedKeys(blockerSet)
	return v, nil
}

// --- 第二层：单个 WorkItem 全量 ---

type WorkItemDetail struct {
	model.WorkItem
	Deps []WorkBrief `json:"deps,omitempty"` // 直接依赖摘要
}

func (s *Service) GetWorkItem(id string) (*WorkItemDetail, error) {
	w, ok := s.Idx.Get(id)
	if !ok {
		return nil, fmt.Errorf("work item %q: %w", id, state.ErrNotFound)
	}
	d := &WorkItemDetail{WorkItem: w}
	for _, dep := range w.DependsOn {
		dw, ok := s.Idx.Get(dep)
		if !ok {
			d.Deps = append(d.Deps, WorkBrief{ID: dep, Status: "missing"})
			continue
		}
		d.Deps = append(d.Deps, WorkBrief{ID: dw.ID, Title: dw.Title, Status: string(dw.Status), System: dw.System})
	}
	return d, nil
}

// --- 第三层：Evidence ---

func (s *Service) GetEvidence(workID string) ([]model.Evidence, error) {
	w, ok := s.Idx.Get(workID)
	if !ok {
		return nil, fmt.Errorf("work item %q: %w", workID, state.ErrNotFound)
	}
	return w.Evidence, nil
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
