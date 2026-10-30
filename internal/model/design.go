package model

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// DesignStatus 是共识层方案的生命周期（v0.4 §4.2 + 2026-09-20 撤回裁决）：
// draft →（PM decide，human 门）→ agreed →（supersede，human 门）→ superseded；
// draft →（withdraw，owner 或 human）→ withdrawn（撤回终态——无权威可转移，
// 与 supersede 无关；PM 撤回事故前只能手改 frontmatter 贴横幅绕绳）。
type DesignStatus string

const (
	DesignDraft      DesignStatus = "draft"
	DesignAgreed     DesignStatus = "agreed"
	DesignSuperseded DesignStatus = "superseded"
	DesignWithdrawn  DesignStatus = "withdrawn"
)

// DesignDoc 是 designs/<topic>/design.md 的 frontmatter（v0.4 §4.1，移除轴后最小集）。
//
// strict decode：未知键拒绝；agreed 必须带 decided_by、superseded 必须带
// superseded_by、withdrawn 必须带 withdrawn_by 且禁 superseded_by/decided_by
// （存在性在此层校验，decided_by/withdrawn_by 的身份由写入路径 + converge
// 双层校验——与 C2 的分工同构）。
type DesignDoc struct {
	Status       DesignStatus `yaml:"status"`
	Owner        string       `yaml:"owner"`
	Systems      []string     `yaml:"systems,omitempty"`
	RelatedItems []string     `yaml:"related_items,omitempty"`
	DecidedBy    string       `yaml:"decided_by,omitempty"`
	DecidedAt    string       `yaml:"decided_at,omitempty"`
	SupersededBy string       `yaml:"superseded_by,omitempty"`
	WithdrawnBy  string       `yaml:"withdrawn_by,omitempty"`
	WithdrawnAt  string       `yaml:"withdrawn_at,omitempty"`

	// Doc 是本 topic 现行方案文档的**相对路径**（相对工作区根）。
	//
	// 认领语义（文档认领层设计 §4.1）：一个 topic 至多一篇现行方案文档；
	// 空 = 卡片自身即全文，不需要外部正本，合法。
	//
	// 约束三条：
	//   - 必须是相对路径（禁绝对路径）——绝对路径跨机器不可移植
	//   - 允许 `../` 越界（指向同级其他仓），但越界者不参与陈旧判定（拿不到 git 时间）
	//   - 不进注入面（T10）：查询走 `ousheng doc map`，与 Knowledge 同列按需自取
	Doc string `yaml:"doc,omitempty"`
}

// DesignInfo 是共识层只读模型（gityaml.ListDesigns 产出，Snapshot 携带，
// 不进 Index 查询面——S5 判决 v0.4 §4.7）。
type DesignInfo struct {
	Topic          string // 目录名（主题）
	Design         DesignDoc
	RoundExists    bool     // 是否已有轮次文件
	LatestSpeakers []string // 最新 round 节头出现的 actor（发言事实，pending 派生用）
}

// DecodeDesignDoc 解析 design.md 原文（--- 包围的 frontmatter + 自由正文）。
// 正文原样返回（重写时零损伤）。
func DecodeDesignDoc(raw []byte) (DesignDoc, []byte, error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return DesignDoc{}, nil, fmt.Errorf("design.md must start with '---' frontmatter")
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return DesignDoc{}, nil, fmt.Errorf("design.md frontmatter not closed")
	}
	fm := s[4 : 4+end]
	// 闭合围栏占 "\n---\n" 5 字符；文件截断在 "---" 时正文为空。
	start := 4 + end + 5
	if start > len(s) {
		start = len(s)
	}
	body := s[start:]

	var d DesignDoc
	dec := yaml.NewDecoder(strings.NewReader(fm))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return DesignDoc{}, nil, err
	}
	if err := validateDesignDoc(d); err != nil {
		return DesignDoc{}, nil, err
	}
	return d, []byte(body), nil
}

// EncodeDesignDoc 重组 design.md（frontmatter + 正文）。
func EncodeDesignDoc(d DesignDoc, body []byte) ([]byte, error) {
	if err := validateDesignDoc(d); err != nil {
		return nil, err
	}
	fm, err := yaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	out := "---\n" + string(fm) + "---\n" + string(body)
	return []byte(out), nil
}

func validateDesignDoc(d DesignDoc) error {
	switch d.Status {
	case DesignDraft, DesignAgreed, DesignSuperseded, DesignWithdrawn:
	case "":
		return fmt.Errorf("design status is required (draft|agreed|superseded|withdrawn)")
	default:
		return fmt.Errorf("invalid design status %q", d.Status)
	}
	if strings.TrimSpace(d.Owner) == "" {
		return fmt.Errorf("design owner is required")
	}
	if d.Status == DesignAgreed && strings.TrimSpace(d.DecidedBy) == "" {
		return fmt.Errorf("agreed design requires decided_by")
	}
	if d.Status == DesignSuperseded && strings.TrimSpace(d.SupersededBy) == "" {
		return fmt.Errorf("superseded design requires superseded_by")
	}
	if d.Status == DesignWithdrawn {
		if strings.TrimSpace(d.WithdrawnBy) == "" {
			return fmt.Errorf("withdrawn design requires withdrawn_by")
		}
		// withdrawn = 未拍板即收回：无权威可转移（禁 superseded_by）、从未 decide（禁 decided_by）
		if strings.TrimSpace(d.SupersededBy) != "" {
			return fmt.Errorf("withdrawn design must not carry superseded_by (nothing to transfer)")
		}
		if strings.TrimSpace(d.DecidedBy) != "" {
			return fmt.Errorf("withdrawn design must not carry decided_by (never decided)")
		}
	}
	if strings.TrimSpace(d.Doc) != "" {
		if err := validateDocPath(d.Doc); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, sys := range d.Systems {
		if !lowerIDRe.MatchString(sys) {
			return fmt.Errorf("invalid system id %q in design systems", sys)
		}
		if seen[sys] {
			return fmt.Errorf("duplicate system %q in design systems", sys)
		}
		seen[sys] = true
	}
	for _, id := range d.RelatedItems {
		if !workIDRe.MatchString(id) {
			return fmt.Errorf("invalid related item id %q", id)
		}
	}
	if d.SupersededBy != "" && !designTopicRe.MatchString(d.SupersededBy) {
		return fmt.Errorf("invalid superseded_by topic %q", d.SupersededBy)
	}
	return nil
}

var designTopicRe = lowerIDRe // 主题目录名 = 小写 id（防路径穿越，同 card id 语义）

// ParseRoundSpeakers 提取 round 文件节头 actor（`## <actor> — <日期>`，v0.4 §4.1）。
// 节头缺失 → 返回空（安全默认全员待发言）。这是清单解析不是语义解析：
// validateDocPath 校验 doc 指针的形态（存在性由 design decide 的写路径校验，
// 因为 draft 阶段允许先写指针后落文件——但形态从解码起就必须合法）。
//
// 三条规则（文档认领层设计 §三 边界 2）：
//   - 禁止绝对路径：跨机器不可移植
//   - 禁止空/根（"."）：指向自己无意义
//   - 禁止反斜杠：工作区路径统一用 /，避免 Windows 分隔符混入 YAML
//
// `../` 允许（越界指向同级仓是合法场景），不做层数限制——读取层只做存在性判断，
// 不写工作区外路径，"永不写工作区外"这条铁律不因此松动。
func validateDocPath(p string) error {
	if strings.ContainsRune(p, '\x00') {
		return fmt.Errorf("design doc path must not contain NUL")
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("design doc path must use '/' separator, got %q", p)
	}
	if path.IsAbs(p) {
		return fmt.Errorf("design doc path must be relative (workspace root), got absolute %q", p)
	}
	clean := path.Clean(p)
	if clean == "." || clean == "/" || clean == "" {
		return fmt.Errorf("design doc path must point to a file, got %q", p)
	}
	return nil
}

// ParseRoundSpeakers 解析轮次文件的节头发言者（`## <actor> — <日期>`），
// 只认行首 `## ` 的第一个 token。
func ParseRoundSpeakers(raw []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		actor := strings.Fields(strings.TrimPrefix(line, "## "))
		if len(actor) == 0 {
			continue
		}
		id := actor[0]
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
