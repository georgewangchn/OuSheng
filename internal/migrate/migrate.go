// Package migrate 实现 v1 Card → v2 WorkItem 迁移（v0.3 §35/§36）。
//
// 映射铁律：
//   - card.id      → work_item.id（原样，不改写）
//   - card.task    → work_item.title
//   - card.status  → work_item.contract.status（契约生命周期）
//   - card.version → work_item.revision（CAS 计数）
//   - card.owner   → resolver：Actor 命中→assignee；Role 命中→acting_role；
//     都不中→legacy_owner + migration_status=needs_resolution
//
// 明确禁止：card.version → target_version（二者语义完全不同）。
package migrate

import (
	"fmt"
	"strings"

	"ousheng/internal/card"
	"ousheng/internal/model"
	"ousheng/internal/state"
)

// StatusNeedsResolution 标记 owner 无法解析、等待人工处理。
const StatusNeedsResolution = "needs_resolution"

// Result 汇总一次迁移的产物。
type Result struct {
	Migrated        []string // 成功迁移的 work id
	Skipped         []string // 已存在而跳过的 work id
	NeedsResolution []string // owner 未解析的 work id
}

// MigrateSchema 把 v1 cards/（board store）迁入 .ousheng/work/。
// cards/ 不删除——v1 API 继续可用（兼容约束）。
// 幂等：work/<id>.yaml 已存在则跳过。
func MigrateSchema(repo state.Repository, cards []card.Card) (Result, error) {
	res := Result{}
	actors := actorIndex(repo)
	roles := roleIndex(repo)

	for _, c := range cards {
		existing, err := repo.GetWorkItem(c.ID)
		if err == nil && existing.ID == c.ID {
			res.Skipped = append(res.Skipped, c.ID)
			continue
		}

		w := CardToWorkItem(c)

		if _, ok := actors[c.Owner]; ok {
			w.Assignee = c.Owner
		} else if _, ok := roles[c.Owner]; ok {
			w.ActingRole = c.Owner
			w.LegacyOwner = c.Owner
			w.MigrationStatus = StatusNeedsResolution
		} else {
			w.LegacyOwner = c.Owner
			w.MigrationStatus = StatusNeedsResolution
		}
		res.NeedsResolution = appendNeeds(res.NeedsResolution, w)

		acts := []model.Activity{{
			TS: model.Now(), Actor: "migrate", Action: "created", WorkItem: w.ID,
			Detail: "migrated from v1 card " + c.ID,
		}}
		msg := fmt.Sprintf("migrate: %s from v1 card", w.ID)
		if _, err := repo.ImportWorkItem(w, acts, msg); err != nil {
			return res, fmt.Errorf("migrate %s: %w", c.ID, err)
		}
		res.Migrated = append(res.Migrated, w.ID)
	}
	return res, nil
}

// CardToWorkItem 执行字段映射（不含 owner 解析）。
func CardToWorkItem(c card.Card) model.WorkItem {
	// WorkItem 执行状态从契约状态推导（可解释映射）：
	// proposed→backlog agreed→ready live→doing verified→done deprecated→cancelled
	workStatus := map[card.Status]model.WorkStatus{
		card.Proposed:   model.StatusBacklog,
		card.Agreed:     model.StatusReady,
		card.Live:       model.StatusDoing,
		card.Verified:   model.StatusDone,
		card.Deprecated: model.StatusCancelled,
	}[c.Status]

	w := model.WorkItem{
		SchemaVersion: 2,
		ID:            c.ID,
		Type:          model.TypeTask, // v1 无类型信息，统一 task
		Title:         c.Task,
		Status:        workStatus,
		Revision:      c.Version,
		DependsOn:     c.DependsOn,
		Contract: &model.Contract{
			Kind:      c.Contract.Kind,
			Status:    contractStatusFromCard(c.Status),
			Breaking:  c.Contract.Breaking,
			Interface: c.Contract.Interface,
		},
	}
	if c.Evidence != nil {
		w.Evidence = append(w.Evidence, model.Evidence{
			Type:    model.EvidenceTestResult,
			Source:  c.Evidence.Probe,
			Locator: c.Evidence.PassedAtCommit,
			Note:    "by: " + c.Evidence.By,
		})
	}
	if c.HumanAck != nil {
		w.HumanAck = &model.HumanAck{
			Approver:   c.HumanAck.Approver,
			AtRevision: c.HumanAck.AtVersion,
			Note:       "migrated from v1 human_ack",
		}
	}
	return w
}

// ResolveOwner 人工解决 needs_resolution 的 work item（§36）。
func ResolveOwner(repo state.Repository, id, assignee, actingRole, accountableHuman string, actor string) (model.WorkItem, error) {
	w, err := repo.GetWorkItem(id)
	if err != nil {
		return model.WorkItem{}, err
	}
	if w.MigrationStatus != StatusNeedsResolution {
		return model.WorkItem{}, fmt.Errorf("work item %s is not needs_resolution (status=%q)", id, w.MigrationStatus)
	}
	if assignee == "" && actingRole == "" {
		return model.WorkItem{}, fmt.Errorf("provide --assignee and/or --role to resolve owner")
	}
	if assignee != "" {
		w.Assignee = assignee
	}
	if actingRole != "" {
		w.ActingRole = actingRole
	}
	if accountableHuman != "" {
		w.AccountableHuman = accountableHuman
	}
	// 与 Service.Update 同门（active 必有主）：role-only 解析会清掉 needs_resolution
	// 却留下无主 doing/testing/blocked——resolve-owner 是 migrate 无主 active 的
	// 指定解阻路径，不允许它造出写路径已杜绝的状态。
	if model.WorkItemActive(w.Status) && w.Assignee == "" {
		return model.WorkItem{}, fmt.Errorf("%s requires assignee (role-only resolution would leave active work unowned; pass --assignee)", w.Status)
	}
	w.LegacyOwner = ""
	w.MigrationStatus = ""
	acts := []model.Activity{{
		TS: model.Now(), Actor: actor, Action: "owner_resolved", WorkItem: id,
		Detail: fmt.Sprintf("assignee=%s role=%s", assignee, actingRole),
	}}
	return repo.UpdateWorkItem(w, w.Revision, acts, "migrate: resolve owner "+id)
}

func contractStatusFromCard(s card.Status) model.ContractStatus {
	return map[card.Status]model.ContractStatus{
		card.Proposed:   model.ContractProposed,
		card.Agreed:     model.ContractAgreed,
		card.Live:       model.ContractLive,
		card.Verified:   model.ContractVerified,
		card.Deprecated: model.ContractDeprecated,
	}[s]
}

func actorIndex(repo state.Repository) map[string]model.Actor {
	out := map[string]model.Actor{}
	if actors, err := repo.ListActors(); err == nil {
		for _, f := range actors {
			out[f.Actor.ID] = f.Actor
		}
	}
	return out
}

func roleIndex(repo state.Repository) map[string]model.Role {
	out := map[string]model.Role{}
	if roles, err := repo.ListRoles(); err == nil {
		for _, r := range roles {
			out[r.ID] = r
		}
	}
	return out
}

func appendNeeds(cur []string, w model.WorkItem) []string {
	if w.MigrationStatus == StatusNeedsResolution {
		return append(cur, w.ID)
	}
	return cur
}

// ReadCards 从 v1 board store 目录读取全部卡（cards/*.yaml）。
func ReadCards(dir string) ([]card.Card, error) {
	// 借用 store.List 逻辑但不引入写路径依赖：直接列目录解码
	var out []card.Card
	entries, err := listYAML(join(dir, "cards"))
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		b, err := readFile(join(dir, "cards", name))
		if err != nil {
			return nil, err
		}
		c, err := card.Decode(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// SummaryText 渲染迁移结果（CLI 输出）。
func (r Result) SummaryText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrated: %d\n", len(r.Migrated))
	for _, id := range r.Migrated {
		fmt.Fprintf(&b, "  + %s\n", id)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "skipped (already migrated): %d\n", len(r.Skipped))
		for _, id := range r.Skipped {
			fmt.Fprintf(&b, "  = %s\n", id)
		}
	}
	if len(r.NeedsResolution) > 0 {
		fmt.Fprintf(&b, "needs owner resolution: %d\n", len(r.NeedsResolution))
		for _, id := range r.NeedsResolution {
			fmt.Fprintf(&b, "  ? %s (ousheng migrate resolve-owner %s --assignee <actor> --role <role>)\n", id, id)
		}
	}
	return b.String()
}
