package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// MaxWorkItemBytes 与 v1 card 同尺寸门（投影成本约束）。
const MaxWorkItemBytes = 8192

var (
	// workIDRe 兼容大小写：v1 Card id 原样迁入 v2 WorkItem（§35 card.id → work_item.id，
	// 不做大小写改写）。新建 WorkItem 推荐 UPPER-xxx 风格（BUG-017 / FEAT-CDC-001）。
	workIDRe  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)
	lowerIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

var workTypes = map[WorkItemType]bool{
	TypeRequirement: true, TypeFeature: true, TypeBug: true, TypeTask: true,
	TypeTest: true, TypeDeployment: true, TypeRelease: true,
}

var workStatuses = map[WorkStatus]bool{
	StatusBacklog: true, StatusReady: true, StatusDoing: true, StatusBlocked: true,
	StatusTesting: true, StatusDone: true, StatusCancelled: true,
}

var contractStatuses = map[ContractStatus]bool{
	ContractProposed: true, ContractAgreed: true, ContractLive: true,
	ContractVerified: true, ContractDeprecated: true,
}

var contractKinds = map[string]bool{"http": true, "cli": true, "lib": true, "event": true}

var evidenceTypes = map[EvidenceType]bool{
	EvidenceGitCommit: true, EvidencePullRequest: true, EvidenceTestResult: true,
	EvidenceCIRun: true, EvidenceDeployment: true, EvidenceLog: true,
	EvidenceManualCheck: true, EvidenceDocument: true,
}

var priorities = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}

var progressBases = map[ProgressBasis]bool{
	BasisManual: true, BasisImplementationChecklist: true, BasisTestCases: true,
	BasisSubtasks: true, BasisStoryPoints: true, BasisMilestone: true,
}

var forbiddenContent = []string{"```", "Traceback (most recent call last)"}

// ValidateWorkItem 手写校验：不用 schema 库（仓库既定约束）。
// raw 参与尺寸与内容门检查；引用完整性（actor/system/role 存在性）由 state 层负责。
func ValidateWorkItem(w WorkItem, raw []byte) error {
	if w.SchemaVersion != 2 {
		return fmt.Errorf("schema_version must be 2, got %d", w.SchemaVersion)
	}
	if !workIDRe.MatchString(w.ID) {
		return fmt.Errorf("invalid work item id %q (want ^[a-zA-Z0-9][a-zA-Z0-9-]*$)", w.ID)
	}
	if !workTypes[w.Type] {
		return fmt.Errorf("invalid type %q", w.Type)
	}
	if strings.TrimSpace(w.Title) == "" {
		return fmt.Errorf("title required")
	}
	if !workStatuses[w.Status] {
		return fmt.Errorf("invalid status %q", w.Status)
	}
	if w.Revision < 1 {
		return fmt.Errorf("revision must be >= 1")
	}
	if w.System != "" && !lowerIDRe.MatchString(w.System) {
		return fmt.Errorf("invalid system ref %q", w.System)
	}
	if w.TargetVersion != "" && strings.TrimSpace(w.TargetVersion) == "" {
		return fmt.Errorf("target_version must be non-empty if present")
	}
	if w.Priority != "" && !priorities[w.Priority] {
		return fmt.Errorf("invalid priority %q (want P0..P3)", w.Priority)
	}
	if w.DueOn != "" {
		if _, err := time.Parse("2006-01-02", w.DueOn); err != nil {
			return fmt.Errorf("due_on must be YYYY-MM-DD, got %q", w.DueOn)
		}
	}
	if w.ActingRole != "" && !lowerIDRe.MatchString(w.ActingRole) {
		return fmt.Errorf("invalid acting_role %q", w.ActingRole)
	}
	for _, dep := range w.DependsOn {
		if dep == w.ID {
			return fmt.Errorf("work item %s cannot depend on itself", w.ID)
		}
		if !workIDRe.MatchString(dep) {
			return fmt.Errorf("invalid depends_on ref %q", dep)
		}
	}
	for _, rel := range w.RelatedTo {
		if !workIDRe.MatchString(rel) {
			return fmt.Errorf("invalid related_to ref %q", rel)
		}
	}
	if w.Contract != nil {
		if !contractKinds[w.Contract.Kind] {
			return fmt.Errorf("invalid contract.kind %q", w.Contract.Kind)
		}
		if w.Contract.Status != "" && !contractStatuses[w.Contract.Status] {
			return fmt.Errorf("invalid contract.status %q", w.Contract.Status)
		}
		// C1：verified 必须有 evidence（v0.3 §4.4 保留 v1 规则）
		if w.Contract.Status == ContractVerified && len(w.Evidence) == 0 {
			return fmt.Errorf("contract.status=verified requires evidence")
		}
		// C2：breaking 必须 human_ack（v0.3 §4.4）
		if w.Contract.Breaking && (w.HumanAck == nil || strings.TrimSpace(w.HumanAck.Approver) == "") {
			return fmt.Errorf("contract.breaking=true requires human_ack with non-empty approver")
		}
	}
	if w.Progress != nil {
		if w.Progress.Value < 0 || w.Progress.Value > 1 {
			return fmt.Errorf("progress.value must be within [0,1], got %v", w.Progress.Value)
		}
		if strings.TrimSpace(w.Progress.Actor) == "" {
			return fmt.Errorf("progress.actor required")
		}
		if !progressBases[w.Progress.Basis] {
			return fmt.Errorf("invalid progress.basis %q", w.Progress.Basis)
		}
		if _, err := time.Parse(time.RFC3339, w.Progress.ReportedAt); err != nil {
			return fmt.Errorf("progress.reported_at must be RFC3339: %w", err)
		}
	}
	for i, ev := range w.Evidence {
		if !evidenceTypes[ev.Type] {
			return fmt.Errorf("evidence[%d]: invalid type %q", i, ev.Type)
		}
		if ev.ObservedAt != "" {
			if _, err := time.Parse(time.RFC3339, ev.ObservedAt); err != nil {
				return fmt.Errorf("evidence[%d]: observed_at must be RFC3339: %w", i, err)
			}
		}
	}
	if w.HumanAck != nil && w.HumanAck.At != "" {
		if _, err := time.Parse(time.RFC3339, w.HumanAck.At); err != nil {
			return fmt.Errorf("human_ack.at must be RFC3339: %w", err)
		}
	}
	if len(raw) > MaxWorkItemBytes {
		return fmt.Errorf("work item exceeds %d bytes——骨架原则：证据用指针（`evidence add --type document --locator <路径/URL>`），正文与实测归外部文档；或先瘦身本条（长段描述/note 移出）", MaxWorkItemBytes)
	}
	for _, f := range forbiddenContent {
		if strings.Contains(string(raw), f) {
			return fmt.Errorf("forbidden content: %q", f)
		}
	}
	return nil
}

// ValidateActor 校验单个 Actor 记录。
func ValidateActor(a Actor) error {
	if !lowerIDRe.MatchString(a.ID) {
		return fmt.Errorf("invalid actor id %q", a.ID)
	}
	if a.Type != ActorHuman && a.Type != ActorAgent {
		return fmt.Errorf("invalid actor type %q", a.Type)
	}
	if a.ResponsibleHuman != "" && !lowerIDRe.MatchString(a.ResponsibleHuman) {
		return fmt.Errorf("invalid responsible_human %q", a.ResponsibleHuman)
	}
	// agent 必须绑定最终责任人（v0.3 §7/§42）
	if a.Type == ActorAgent && a.ResponsibleHuman == "" {
		return fmt.Errorf("agent actor %s requires responsible_human", a.ID)
	}
	return nil
}

// ValidateAssignment 校验单条 Assignment。
func ValidateAssignment(a Assignment) error {
	if !lowerIDRe.MatchString(a.Actor) {
		return fmt.Errorf("invalid assignment.actor %q", a.Actor)
	}
	if !lowerIDRe.MatchString(a.Role) {
		return fmt.Errorf("invalid assignment.role %q", a.Role)
	}
	if !lowerIDRe.MatchString(a.System) {
		return fmt.Errorf("invalid assignment.system %q", a.System)
	}
	if strings.TrimSpace(a.Responsibility) == "" {
		return fmt.Errorf("assignment.responsibility required")
	}
	return nil
}
