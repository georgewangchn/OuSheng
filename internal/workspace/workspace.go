// Package workspace 是 v0.3 Core 业务规则层（v0.3 §4.1）：
// 模型约束、CAS、状态迁移、Evidence 规则、Activity 记录全部在此，
// CLI / MCP 不承载业务逻辑。存储通过 state.Repository 注入，可替换。
package workspace

import (
	"fmt"
	"strings"

	"ousheng/internal/model"
	"ousheng/internal/state"
)

type Service struct {
	Repo state.Repository
}

func New(r state.Repository) *Service { return &Service{Repo: r} }

// refs 是写路径上的引用完整性校验（注册表为空时跳过——bootstrap 弱介入）。
func (s *Service) validateRefs(w model.WorkItem) error {
	systems, err := s.Repo.ListSystems()
	if err != nil {
		return err
	}
	if len(systems) > 0 && w.System != "" {
		found := false
		for _, sys := range systems {
			if sys.ID == w.System {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown system %q (declared: %s)", w.System, ids(systemIDs(systems)))
		}
	}

	actors, err := s.Repo.ListActors()
	if err != nil {
		return err
	}
	if len(actors) > 0 {
		byID := map[string]model.Actor{}
		for _, f := range actors {
			byID[f.Actor.ID] = f.Actor
		}
		if w.Assignee != "" {
			if _, ok := byID[w.Assignee]; !ok {
				return fmt.Errorf("unknown assignee actor %q", w.Assignee)
			}
		}
		if w.DetectedBy != "" {
			if _, ok := byID[w.DetectedBy]; !ok {
				return fmt.Errorf("unknown detected_by actor %q", w.DetectedBy)
			}
		}
		// 问责必须落到 human（v0.3 §42：防 AI 生成→AI 验收闭环）
		if w.AccountableHuman != "" {
			a, ok := byID[w.AccountableHuman]
			if !ok {
				return fmt.Errorf("unknown accountable_human %q", w.AccountableHuman)
			}
			if a.Type != model.ActorHuman {
				return fmt.Errorf("accountable_human %q must be type=human, got %s", w.AccountableHuman, a.Type)
			}
		}
	}

	roles, err := s.Repo.ListRoles()
	if err != nil {
		return err
	}
	if len(roles) > 0 && w.ActingRole != "" {
		found := false
		for _, r := range roles {
			if r.ID == w.ActingRole {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown acting_role %q", w.ActingRole)
		}
	}
	return nil
}

// Create 创建 WorkItem。新工作必须从 backlog 起步（对应 v1 新卡必须 proposed）。
func (s *Service) Create(w model.WorkItem, actor string) (model.WorkItem, error) {
	if w.Status != "" && w.Status != model.StatusBacklog {
		return model.WorkItem{}, fmt.Errorf("new work item must start in backlog (got %s)", w.Status)
	}
	w.Status = model.StatusBacklog
	if err := s.validateRefs(w); err != nil {
		return model.WorkItem{}, err
	}
	w.SchemaVersion = 2
	acts := []model.Activity{{
		TS: model.Now(), Actor: actor, Action: "created", WorkItem: w.ID,
		Detail: fmt.Sprintf("type=%s system=%s target_version=%s", w.Type, w.System, w.TargetVersion),
	}}
	msg := fmt.Sprintf("work: create %s (%s)", w.ID, w.Title)
	return s.Repo.CreateWorkItem(w, acts, msg)
}

// Update 以 CAS 语义更新 WorkItem，并强制执行两个独立生命周期迁移规则。
func (s *Service) Update(w model.WorkItem, expectRevision int, actor string) (model.WorkItem, error) {
	cur, err := s.Repo.GetWorkItem(w.ID)
	if err != nil {
		return model.WorkItem{}, err
	}
	var acts []model.Activity
	if cur.Status != w.Status && !model.CanWorkTransition(cur.Status, w.Status) {
		return model.WorkItem{}, fmt.Errorf("illegal work transition %s -> %s", cur.Status, w.Status)
	}
	if cur.Status != w.Status {
		acts = append(acts, model.Activity{
			TS: model.Now(), Actor: actor, Action: "status_changed", WorkItem: w.ID,
			Detail: string(cur.Status) + " -> " + string(w.Status),
		})
	}
	if cur.Contract != nil && w.Contract != nil &&
		cur.Contract.Status != w.Contract.Status &&
		!model.CanContractTransition(cur.Contract.Status, w.Contract.Status) {
		return model.WorkItem{}, fmt.Errorf("illegal contract transition %s -> %s", cur.Contract.Status, w.Contract.Status)
	}
	if cur.Contract != nil && w.Contract != nil && cur.Contract.Status != w.Contract.Status {
		acts = append(acts, model.Activity{
			TS: model.Now(), Actor: actor, Action: "contract_status_changed", WorkItem: w.ID,
			Detail: string(cur.Contract.Status) + " -> " + string(w.Contract.Status),
		})
	}
	if cur.Assignee != w.Assignee || cur.ActingRole != w.ActingRole {
		acts = append(acts, model.Activity{
			TS: model.Now(), Actor: actor, Action: "assigned", WorkItem: w.ID,
			Detail: fmt.Sprintf("assignee=%s role=%s", w.Assignee, w.ActingRole),
		})
	}
	if err := s.validateRefs(w); err != nil {
		return model.WorkItem{}, err
	}
	if len(acts) == 0 {
		acts = append(acts, model.Activity{
			TS: model.Now(), Actor: actor, Action: "updated", WorkItem: w.ID,
		})
	}
	msg := fmt.Sprintf("work: update %s", w.ID)
	return s.Repo.UpdateWorkItem(w, expectRevision, acts, msg)
}

// Assign 设置 assignee / acting_role。
func (s *Service) Assign(id string, assignee, actingRole string, expectRevision int, actor string) (model.WorkItem, error) {
	w, err := s.Repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	w.Assignee = assignee
	w.ActingRole = actingRole
	return s.Update(w, expectRevision, actor)
}

// ReportProgress 更新 ProgressReport（reported state，非 fact）。
func (s *Service) ReportProgress(id string, p model.ProgressReport, expectRevision int) (model.WorkItem, error) {
	w, err := s.Repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	w.Progress = &p
	acts := []model.Activity{{
		TS: model.Now(), Actor: p.Actor, Action: "progress_reported", WorkItem: id,
		Detail: fmt.Sprintf("%.2f basis=%s", p.Value, p.Basis),
	}}
	msg := fmt.Sprintf("work: progress %s %.0f%%", id, p.Value*100)
	return s.Repo.UpdateWorkItem(w, expectRevision, acts, msg)
}

// AddEvidence 追加 typed evidence（append-only）。
func (s *Service) AddEvidence(id string, ev model.Evidence, expectRevision int, actor string) (model.WorkItem, error) {
	w, err := s.Repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	w.Evidence = append(w.Evidence, ev)
	acts := []model.Activity{{
		TS: model.Now(), Actor: actor, Action: "evidence_added", WorkItem: id,
		Detail: fmt.Sprintf("%s %s", ev.Type, ev.Locator),
	}}
	msg := fmt.Sprintf("work: evidence %s %s", id, ev.Type)
	return s.Repo.UpdateWorkItem(w, expectRevision, acts, msg)
}

// Ack 记录 human ack。approver 必须是已注册 human actor（注册表非空时）。
func (s *Service) Ack(id string, approver, note string, expectRevision int) (model.WorkItem, error) {
	w, err := s.Repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	w.HumanAck = &model.HumanAck{Approver: approver, At: model.Now(), Note: note}
	if err := s.validateRefs(w); err != nil {
		return model.WorkItem{}, err
	}
	acts := []model.Activity{{
		TS: model.Now(), Actor: approver, Action: "human_acked", WorkItem: id,
		Detail: note,
	}}
	msg := fmt.Sprintf("work: ack %s by %s", id, approver)
	return s.Repo.UpdateWorkItem(w, expectRevision, acts, msg)
}

// helper：拼 id 列表用于报错信息。
func ids(ss []string) string { return strings.Join(ss, ",") }

func systemIDs(systems []model.System) []string {
	out := make([]string, len(systems))
	for i, s := range systems {
		out[i] = s.ID
	}
	return out
}
