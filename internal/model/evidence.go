package model

// EvidenceType 是 typed evidence 封闭集合（v0.3 §17）。
type EvidenceType string

const (
	EvidenceGitCommit   EvidenceType = "git_commit"
	EvidencePullRequest EvidenceType = "pull_request"
	EvidenceTestResult  EvidenceType = "test_result"
	EvidenceCIRun       EvidenceType = "ci_run"
	EvidenceDeployment  EvidenceType = "deployment"
	EvidenceLog         EvidenceType = "log"
	EvidenceManualCheck EvidenceType = "manual_check"
	EvidenceDocument    EvidenceType = "document"
)

// Evidence 是 append-only 类型化证据条目。
// State 回答"现在是什么状态"，Evidence 回答"为什么相信这个状态"（v0.3 §16）。
type Evidence struct {
	Type       EvidenceType `yaml:"type"`
	Source     string       `yaml:"source,omitempty"`     // 观察来源：github、pytest、git…
	Locator    string       `yaml:"locator,omitempty"`    // 外部定位符：commit hash、run id…
	Result     string       `yaml:"result,omitempty"`     // passed|failed|…（可选）
	ObservedAt string       `yaml:"observed_at,omitempty"` // RFC3339
	Note       string       `yaml:"note,omitempty"`
}

// ProgressBasis 是 progress 计算依据（v0.3 §19）。
// Progress 是 Reported State，不是 Fact；必须有明示 basis，不许凭空百分比。
type ProgressBasis string

const (
	BasisManual                  ProgressBasis = "manual"
	BasisImplementationChecklist ProgressBasis = "implementation-checklist"
	BasisTestCases               ProgressBasis = "test-cases"
	BasisSubtasks                ProgressBasis = "subtasks"
	BasisStoryPoints             ProgressBasis = "story-points"
	BasisMilestone               ProgressBasis = "milestone"
)

// ProgressReport 报告者署名 + 依据 + 时间戳。
type ProgressReport struct {
	Value      float64       `yaml:"value"` // 0..1
	Actor      string        `yaml:"actor"`
	ReportedAt string        `yaml:"reported_at"` // RFC3339
	Basis      ProgressBasis `yaml:"basis"`
}
