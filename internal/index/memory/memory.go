// Package memory 是 Index 的纯内存实现（v0.3 §30）：
// YAML → Go in-memory index 直接工作，小项目零基础设施。
package memory

import (
	"sort"

	"ousheng/internal/index"
	"ousheng/internal/model"
)

type MemIndex struct {
	snap index.Snapshot

	byID           map[string]model.WorkItem
	byAssignee     map[string][]model.WorkItem
	byAccount      map[string][]model.WorkItem
	bySystem       map[string][]model.WorkItem
	byVersion      map[string][]model.WorkItem
	byStatus       map[model.WorkStatus][]model.WorkItem
	byType         map[model.WorkItemType][]model.WorkItem
	active         map[string][]model.WorkItem
	blockers       map[string][]index.Blocker
	actorsByID     map[string]model.ActorFile
	systemsByID    map[string]model.System
	assignByActor  map[string][]model.Assignment
	assignBySystem map[string][]model.Assignment
}

var _ index.Index = (*MemIndex)(nil)

func New() *MemIndex {
	return &MemIndex{
		byID:           map[string]model.WorkItem{},
		byAssignee:     map[string][]model.WorkItem{},
		byAccount:      map[string][]model.WorkItem{},
		bySystem:       map[string][]model.WorkItem{},
		byVersion:      map[string][]model.WorkItem{},
		byStatus:       map[model.WorkStatus][]model.WorkItem{},
		byType:         map[model.WorkItemType][]model.WorkItem{},
		active:         map[string][]model.WorkItem{},
		blockers:       map[string][]index.Blocker{},
		actorsByID:     map[string]model.ActorFile{},
		systemsByID:    map[string]model.System{},
		assignByActor:  map[string][]model.Assignment{},
		assignBySystem: map[string][]model.Assignment{},
	}
}

func (m *MemIndex) Rebuild(s index.Snapshot) error {
	*m = *New()
	m.snap = s

	// 第一遍：填充全部查找表（依赖可能在列表后部，blockers 必须第二遍算）
	for _, w := range s.WorkItems {
		m.byID[w.ID] = w
		m.byAssignee[w.Assignee] = append(m.byAssignee[w.Assignee], w)
		m.byAccount[w.AccountableHuman] = append(m.byAccount[w.AccountableHuman], w)
		if w.System != "" {
			m.bySystem[w.System] = append(m.bySystem[w.System], w)
		}
		if w.TargetVersion != "" {
			m.byVersion[w.TargetVersion] = append(m.byVersion[w.TargetVersion], w)
		}
		m.byStatus[w.Status] = append(m.byStatus[w.Status], w)
		m.byType[w.Type] = append(m.byType[w.Type], w)
		if model.WorkItemActive(w.Status) {
			m.active[w.Assignee] = append(m.active[w.Assignee], w)
		}
	}
	// 第二遍：blockers（依赖未完成或缺失）
	for _, w := range s.WorkItems {
		for _, dep := range w.DependsOn {
			d, ok := m.byID[dep]
			switch {
			case !ok:
				m.blockers[w.ID] = append(m.blockers[w.ID], index.Blocker{WorkID: w.ID, DepID: dep, Reason: "missing"})
			case !isDone(d):
				m.blockers[w.ID] = append(m.blockers[w.ID], index.Blocker{WorkID: w.ID, DepID: dep, Reason: "not-done"})
			}
		}
	}
	// Registry 输出统一按 id 排序（与 sqlite ORDER BY 一致，S5 等价性）
	sort.Slice(s.Systems, func(i, j int) bool { return s.Systems[i].ID < s.Systems[j].ID })
	sort.Slice(s.Roles, func(i, j int) bool { return s.Roles[i].ID < s.Roles[j].ID })
	sort.Slice(s.Actors, func(i, j int) bool { return s.Actors[i].Actor.ID < s.Actors[j].Actor.ID })
	sort.Slice(s.Assignments, func(i, j int) bool {
		if s.Assignments[i].Actor != s.Assignments[j].Actor {
			return s.Assignments[i].Actor < s.Assignments[j].Actor
		}
		if s.Assignments[i].System != s.Assignments[j].System {
			return s.Assignments[i].System < s.Assignments[j].System
		}
		return s.Assignments[i].Role < s.Assignments[j].Role
	})

	for _, a := range s.Actors {
		m.actorsByID[a.Actor.ID] = a
	}
	for _, sys := range s.Systems {
		m.systemsByID[sys.ID] = sys
	}
	for _, a := range s.Assignments {
		m.assignByActor[a.Actor] = append(m.assignByActor[a.Actor], a)
		m.assignBySystem[a.System] = append(m.assignBySystem[a.System], a)
	}
	return nil
}

func isDone(w model.WorkItem) bool {
	return w.Status == model.StatusDone || w.Status == model.StatusCancelled
}

func (m *MemIndex) Get(id string) (model.WorkItem, bool, error) {
	w, ok := m.byID[id]
	return w, ok, nil
}

func (m *MemIndex) All() ([]model.WorkItem, error) { return m.snap.WorkItems, nil }

func (m *MemIndex) ByAssignee(actor string) ([]model.WorkItem, error) {
	return m.byAssignee[actor], nil
}

func (m *MemIndex) ByAccountable(human string) ([]model.WorkItem, error) {
	return m.byAccount[human], nil
}

func (m *MemIndex) BySystem(system string) ([]model.WorkItem, error) {
	return m.bySystem[system], nil
}

func (m *MemIndex) ByVersion(version string) ([]model.WorkItem, error) {
	return m.byVersion[version], nil
}

func (m *MemIndex) ByStatus(status model.WorkStatus) ([]model.WorkItem, error) {
	return m.byStatus[status], nil
}

func (m *MemIndex) ByType(t model.WorkItemType) ([]model.WorkItem, error) {
	return m.byType[t], nil
}

func (m *MemIndex) ActiveByActor(actor string) ([]model.WorkItem, error) {
	return m.active[actor], nil
}

func (m *MemIndex) BlockersOf(id string) ([]index.Blocker, error) {
	return m.blockers[id], nil
}

func (m *MemIndex) Actor(id string) (model.ActorFile, bool, error) {
	a, ok := m.actorsByID[id]
	return a, ok, nil
}

func (m *MemIndex) Actors() ([]model.ActorFile, error) { return m.snap.Actors, nil }

func (m *MemIndex) System(id string) (model.System, bool, error) {
	s, ok := m.systemsByID[id]
	return s, ok, nil
}

func (m *MemIndex) Systems() ([]model.System, error) { return m.snap.Systems, nil }

func (m *MemIndex) AssignmentsByActor(actor string) ([]model.Assignment, error) {
	return m.assignByActor[actor], nil
}

func (m *MemIndex) AssignmentsBySystem(system string) ([]model.Assignment, error) {
	return m.assignBySystem[system], nil
}

func (m *MemIndex) Roles() ([]model.Role, error) { return m.snap.Roles, nil }

// SortedWorkIDs 返回有序 work id 列表（调试 / 一致性比对用）。
func (m *MemIndex) SortedWorkIDs() []string {
	out := make([]string, 0, len(m.byID))
	for id := range m.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
