package model

// ActorType 统一 Human 与 Agent（v0.3 §7）。
// 不引入 Person/Human/Agent 四套身份概念。
type ActorType string

const (
	ActorHuman ActorType = "human"
	ActorAgent ActorType = "agent"
)

// Actor 是工程身份实体。
// agent 必须绑定 responsible_human（最终责任人，v0.3 §42）。
type Actor struct {
	ID               string    `yaml:"id"`
	Type             ActorType `yaml:"type"`
	DisplayName      string    `yaml:"display_name,omitempty"`
	ResponsibleHuman string    `yaml:"responsible_human,omitempty"`
}

// Manifest 是 Agent Context Manifest（v0.3 §25）：
// Agent 启动时 get_my_context() 的默认作用域。
type Manifest struct {
	DefaultRole   string   `yaml:"default_role,omitempty"`
	Systems       []string `yaml:"systems,omitempty"`
	TargetVersion string   `yaml:"target_version,omitempty"`
}

// ActorFile 是 .ousheng/actors/<id>.yaml 的文件格式。
type ActorFile struct {
	SchemaVersion int       `yaml:"schema_version"`
	Actor         Actor     `yaml:"actor"`
	Manifest      *Manifest `yaml:"manifest,omitempty"`
}
