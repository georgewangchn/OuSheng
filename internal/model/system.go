package model

// System 是工程上下文的核心锚点（v0.3 §9）。
// System ≠ Repository：可映射多 repo / service / deployment / data domain。
// 层级通过 parent 表达，不引入独立 Component 实体（第一版）。
type System struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name,omitempty"`
	Parent       string   `yaml:"parent,omitempty"`
	Repositories []string `yaml:"repositories,omitempty"`
}

type SystemsFile struct {
	SchemaVersion int      `yaml:"schema_version"`
	Systems       []System `yaml:"systems"`
}

// Role 第一版保持轻量：project-scoped registry（v0.3 §8）。
// 只回答"Actor 以什么职责视角参与当前 System"。
type Role struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type RolesFile struct {
	SchemaVersion int    `yaml:"schema_version"`
	Roles         []Role `yaml:"roles"`
}
