// Package index 定义派生索引抽象（v0.3 §29/§30）。
//
// 索引永远可从 canonical Git+YAML 完整重建（硬约束 S5）：
//
//	rm .ousheng/cache/index.db && ousheng index rebuild
//
// 小规模必须可不依赖 SQLite 运行：memory 实现直接从 YAML 构建。
package index

import (
	"ousheng/internal/model"
	"ousheng/internal/state"
)

// Snapshot 是一次完整 canonical state 装载，索引的唯一输入。
type Snapshot struct {
	Project     model.Project
	Systems     []model.System
	Roles       []model.Role
	Actors      []model.ActorFile
	Assignments []model.Assignment
	WorkItems   []model.WorkItem
}

// Load 从 repository 装载完整 snapshot。
func Load(repo state.Repository) (Snapshot, error) {
	var s Snapshot
	var err error
	if s.Project, err = repo.GetProject(); err != nil {
		return Snapshot{}, err
	}
	if s.Systems, err = repo.ListSystems(); err != nil {
		return Snapshot{}, err
	}
	if s.Roles, err = repo.ListRoles(); err != nil {
		return Snapshot{}, err
	}
	if s.Actors, err = repo.ListActors(); err != nil {
		return Snapshot{}, err
	}
	if s.Assignments, err = repo.ListAssignments(); err != nil {
		return Snapshot{}, err
	}
	if s.WorkItems, err = repo.ListWorkItems(); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// Blocker 描述一个未解除的依赖阻塞。
type Blocker struct {
	WorkID string // 被阻塞的 work item
	DepID  string // 依赖对象
	Reason string // not-done | missing
}

// Index 是查询接口。实现必须可从 Snapshot 无损重建。
// 查询带 error：SQLite 实现可能失败；memory 实现恒返回 nil。
type Index interface {
	Rebuild(s Snapshot) error

	// WorkItem 查询
	Get(id string) (model.WorkItem, bool, error)
	All() ([]model.WorkItem, error)
	ByAssignee(actor string) ([]model.WorkItem, error)
	ByAccountable(human string) ([]model.WorkItem, error)
	BySystem(system string) ([]model.WorkItem, error)
	ByVersion(version string) ([]model.WorkItem, error)
	ByStatus(status model.WorkStatus) ([]model.WorkItem, error)
	ByType(t model.WorkItemType) ([]model.WorkItem, error)
	// ActiveByActor：actor 执行中（doing/testing/blocked）的 work。
	ActiveByActor(actor string) ([]model.WorkItem, error)
	// BlockersOf：直接依赖中未完成（非 done/cancelled）或缺失的项。
	BlockersOf(id string) ([]Blocker, error)

	// Registry 查询
	Actor(id string) (model.ActorFile, bool, error)
	Actors() ([]model.ActorFile, error)
	System(id string) (model.System, bool, error)
	Systems() ([]model.System, error)
	AssignmentsByActor(actor string) ([]model.Assignment, error)
	AssignmentsBySystem(system string) ([]model.Assignment, error)
	Roles() ([]model.Role, error)
}
