package model

// ContractStatus 沿用 v1 五态生命周期（v0.3 §4.3），
// 与 WorkStatus（7 态执行生命周期）分离（v0.3 §13）。
type ContractStatus string

const (
	ContractProposed   ContractStatus = "proposed"
	ContractAgreed     ContractStatus = "agreed"
	ContractLive       ContractStatus = "live"
	ContractVerified   ContractStatus = "verified"
	ContractDeprecated ContractStatus = "deprecated"
)

// Contract 是 WorkItem 的重要子对象，不再是顶层核心（v0.3 §4.3）。
type Contract struct {
	Kind      string         `yaml:"kind"` // http|cli|lib|event
	Status    ContractStatus `yaml:"status,omitempty"`
	Breaking  bool           `yaml:"breaking,omitempty"`
	Interface any            `yaml:"interface,omitempty"`
}

// HumanAck 是 human accountability 载体（v0.3 §42）。
// 关键动作（breaking 等）必须由 human 确认，防止 AI 生成→AI 验收闭环幻觉。
type HumanAck struct {
	Approver   string `yaml:"approver"`
	At         string `yaml:"at,omitempty"`          // RFC3339
	AtRevision int    `yaml:"at_revision,omitempty"` // v1 迁移携带：ack 时的 CAS revision
	Note       string `yaml:"note,omitempty"`
}

// CanContractTransition 报告 Contract 状态迁移合法性。
// 与 v1 card lifecycle 相同的边：proposed→agreed→live→verified，任意态→deprecated，
// verified→live 允许（回归）。
func CanContractTransition(from, to ContractStatus) bool {
	if from == to {
		return true
	}
	edges := map[ContractStatus][]ContractStatus{
		ContractProposed:   {ContractAgreed, ContractDeprecated},
		ContractAgreed:     {ContractLive, ContractDeprecated},
		ContractLive:       {ContractVerified, ContractDeprecated},
		ContractVerified:   {ContractLive, ContractDeprecated},
		ContractDeprecated: {},
	}
	for _, t := range edges[from] {
		if t == to {
			return true
		}
	}
	return false
}
