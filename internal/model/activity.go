package model

// Activity 是 audit / observation record，不是权威事实源（v0.3 §18）。
// 权威字段始终是 WorkItem 本身；Event Sourcing 需单独 ADR，此处不隐式引入。
type Activity struct {
	TS       string `json:"ts"`     // RFC3339
	Actor    string `json:"actor"`  // 谁
	Action   string `json:"action"` // created|status_changed|assigned|progress_reported|evidence_added|acked|deprecated|…
	WorkItem string `json:"work_item,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ActionDesignDecided：design 拍板的 activity 动词（converge 据此交叉验证
// frontmatter decided_by——缺失 = 疑手改绕过拍板门，档案 §28）。
const ActionDesignDecided = "design_decided"

// Project 第一版只是 workspace/config 顶层，不建重量级实体（v0.3 §14）。
type Project struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type ProjectFile struct {
	SchemaVersion int     `yaml:"schema_version"`
	Project       Project `yaml:"project"`
}
