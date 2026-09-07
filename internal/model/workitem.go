package model

// WorkItemType 是 v0.3 第一版 WorkItem 类型封闭集合。
// progress 不是类型：进度是状态报告，不是工作本身（v0.3 §11）。
type WorkItemType string

const (
	TypeRequirement WorkItemType = "requirement"
	TypeFeature     WorkItemType = "feature"
	TypeBug         WorkItemType = "bug"
	TypeTask        WorkItemType = "task"
	TypeTest        WorkItemType = "test"
	TypeDeployment  WorkItemType = "deployment"
	TypeRelease     WorkItemType = "release"
)

// WorkStatus 是工程执行生命周期，与 Contract 生命周期（5 态）彻底分开（v0.3 §13）。
type WorkStatus string

const (
	StatusBacklog   WorkStatus = "backlog"
	StatusReady     WorkStatus = "ready"
	StatusDoing     WorkStatus = "doing"
	StatusBlocked   WorkStatus = "blocked"
	StatusTesting   WorkStatus = "testing"
	StatusDone      WorkStatus = "done"
	StatusCancelled WorkStatus = "cancelled"
)

// WorkItemActive 报告该 WorkItem 是否处于"正在进行"（doing/testing/blocked）。
// blocked 也算 active：它是被挂起的进行中工作，Projection 需要展示。
func WorkItemActive(s WorkStatus) bool {
	return s == StatusDoing || s == StatusTesting || s == StatusBlocked
}

// WorkItemOpen 报告该 WorkItem 是否尚未终结（非 done/cancelled）。
func WorkItemOpen(s WorkStatus) bool {
	return s != StatusDone && s != StatusCancelled
}

// WorkSummary 是 WorkItem 的最小投影，用于 Board / Context 第一层（Progressive Disclosure）。
type WorkSummary struct {
	ID            string     `json:"id"`
	Type          string     `json:"type"`
	Title         string     `json:"title"`
	System        string     `json:"system,omitempty"`
	TargetVersion string     `json:"target_version,omitempty"`
	Status        string     `json:"status"`
	Assignee      string     `json:"assignee,omitempty"`
	Revision      int        `json:"revision"`
}

// WorkItem 是 v0.3 canonical 工程工作概念（schema_version=2）。
// Card 保留为兼容投影；二者不得混写（v0.3 §11）。
type WorkItem struct {
	SchemaVersion int    `yaml:"schema_version"`
	ID            string `yaml:"id"`
	Type          WorkItemType `yaml:"type"`
	Title         string `yaml:"title"`
	System        string `yaml:"system,omitempty"`
	TargetVersion string `yaml:"target_version,omitempty"`
	TargetRelease string `yaml:"target_release,omitempty"`

	Assignee         string `yaml:"assignee,omitempty"`
	ActingRole       string `yaml:"acting_role,omitempty"`
	AccountableHuman string `yaml:"accountable_human,omitempty"`
	DetectedBy       string `yaml:"detected_by,omitempty"`

	Status   WorkStatus `yaml:"status"`
	Revision int        `yaml:"revision"`

	DependsOn []string `yaml:"depends_on,omitempty"`
	RelatedTo []string `yaml:"related_to,omitempty"`

	Contract *Contract        `yaml:"contract,omitempty"`
	Progress *ProgressReport `yaml:"progress,omitempty"`
	Evidence []Evidence       `yaml:"evidence,omitempty"`
	HumanAck *HumanAck        `yaml:"human_ack,omitempty"`

	// 迁移辅助字段（v0.3 §36）：owner 无法解析时保留原值，等人工 resolve。
	LegacyOwner    string `yaml:"legacy_owner,omitempty"`
	MigrationStatus string `yaml:"migration_status,omitempty"`
}

func (w WorkItem) Summarize() WorkSummary {
	return WorkSummary{
		ID:            w.ID,
		Type:          string(w.Type),
		Title:         w.Title,
		System:        w.System,
		TargetVersion: w.TargetVersion,
		Status:        string(w.Status),
		Assignee:      w.Assignee,
		Revision:      w.Revision,
	}
}
