package model

// System 是工程上下文的核心锚点（v0.3 §9）。
// System ≠ Repository：可映射多 repo / service / deployment / data domain。
// 层级通过 parent 表达，不引入独立 Component 实体（第一版）。
type System struct {
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name,omitempty"`
	Parent       string   `yaml:"parent,omitempty"`
	Repositories []string `yaml:"repositories,omitempty"`

	// ContractDoc 是该系统契约正本（INFO-CARD 类）在**其映射代码仓内**的
	// 相对路径（档案 §30，TASK-039）。机器本地经 repos.yaml 解析到仓；
	// 未映射/文件缺失 = 该机不可见，静默跳过（best-effort 读面）。
	//
	// 语义：投影在 ousheng，SSOT 在实栈——architecture/<sys>.md 是绳上投影，
	// contract_doc 指向真本。认领层据此把契约正本纳为 RoleContract（bloat 判，
	// 陈旧不判——契约版本机制另有 human_ack）。空 = 未声明，合法。
	ContractDoc string `yaml:"contract_doc,omitempty"`

	// Envs 是该系统环境存在性的登记面（H3，档案 §38：环境是数据维度不是
	// 拓扑分叉）。仅登记，不做配置分发（死法记录：配置中心复辟）。空 = 未登记。
	Envs []string `yaml:"envs,omitempty"`
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
