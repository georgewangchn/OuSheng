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

	// Machine 是该座位常驻机器的登记面（档案 §40，2026-11-12 pm session 冷启动
	// 实栈发现：七座位六台机的拓扑绳上无处查——跨机协作的执行位寻址靠它：
	// pm 派活/杀进程前 `team list` 即知座位在哪。声明性不验证（机器名跨机
	// 不可验证），空=未登记（多机游走/已下线）。与 §38 envs 同构：登记≠分发。
	Machine string `yaml:"machine,omitempty"`
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
