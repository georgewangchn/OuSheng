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

	byID        map[string]model.WorkItem
	byAssignee  map[string][]model.WorkItem
	byAccount   map[string][]model.WorkItem
	bySystem    map[string][]model.WorkItem
	byVersion   map[string][]model.WorkItem
	byStatus    map[model.WorkStatus][]model.WorkItem
	byType      map[model.WorkItemType][]model.WorkItem
	active      map[string][]model.WorkItem
	blockers    map[string][]index.Blocker
	actorsByID  map[string]model.ActorFile
	systemsByID map[string]model.System
	assignByActor   map[string][]model.Assignment
	assignBySystem  map[string][]model.Assignment
}

var _ index.Index = (*MemIndex)(nil)

func New() *MemIndex {
	return &MemIndex{
		byID: map[string]model.WorkItem{},
		byAssignee: map[string][]model.WorkItem{},
		byAccount: map[string][]model.WorkItem{},
		bySystem: map[string][]model.WorkItem{},
		byVersion: map[string][]model.WorkItem{},
		byStatus: map[model.WorkStatus][]model.WorkItem{},
		byType: map[model.WorkItemType][]model.WorkItem{},
		active: map[string][]model.WorkItem{},
		blockers: map[string][]index.Blocker{},
		actorsByID: map[string]model.ActorFile{},
		systemsByID: map[string]model.System{},
		assignByActor: map[string][]model.Assignment{},
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

func (m *MemIndex) Get(id string) (model.WorkItem, bool) {
	w, ok := m.byID[id]
	return w, ok
}

func (m *MemIndex) All() []model.WorkItem { return m.snap.WorkItems }

func (m *MemIndex) ByAssignee(actor string) []model.WorkItem { return m.byAssignee[actor] }
func (m *MemIndex) ByAccountable(human string) []model.WorkItem { return m.byAccount[human] }
func (m *MemIndex) BySystem(system string) []model.WorkItem { return m.bySystem[system] }
func (m *MemIndex) ByVersion(version string) []model.WorkItem { return m.byVersion[version] }
func (m *MemIndex) ByStatus(status model.WorkStatus) []model.WorkItem { return m.byStatus[status] }
func (m *MemIndex) ByType(t model.WorkItemType) []model.WorkItem { return m.byType[t] }
func (m *MemIndex) ActiveByActor(actor string) []model.WorkItem { return m.active[actor] }
func (m *MemIndex) BlockersOf(id string) []index.Blocker { return m.blockers[id] }

func (m *MemIndex) Actor(id string) (model.ActorFile, bool) {
	a, ok := m.actorsByID[id]
	return a, ok
}

func (m *MemIndex) Actors() []model.ActorFile { return m.snap.Actors }

func (m *MemIndex) System(id string) (model.System, bool) {
	s, ok := m.systemsByID[id]
	return s, ok
}

func (m *MemIndex) Systems() []model.System { return m.snap.Systems }

func (m *MemIndex) AssignmentsByActor(actor string) []model.Assignment {
	return m.assignByActor[actor]
}

func (m *MemIndex) AssignmentsBySystem(system string) []model.Assignment {
	return m.assignBySystem[system]
}

func (m *MemIndex) Roles() []model.Role { return m.snap.Roles }

// SortedWorkIDs 返回有序 work id 列表（调试 / 一致性比对用）。
func (m *MemIndex) SortedWorkIDs() []string {
	out := make([]string, 0, len(m.byID))
	for id := range m.byID {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
