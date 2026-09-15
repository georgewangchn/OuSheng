package model

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DesignStatus 是共识层方案的生命周期（v0.4 §4.2）：
// draft →（PM decide，human 门）→ agreed →（supersede，human 门）→ superseded。
type DesignStatus string

const (
	DesignDraft      DesignStatus = "draft"
	DesignAgreed     DesignStatus = "agreed"
	DesignSuperseded DesignStatus = "superseded"
)

// DesignDoc 是 designs/<topic>/design.md 的 frontmatter（v0.4 §4.1，移除轴后最小集）。
//
// strict decode：未知键拒绝；agreed 必须带 decided_by、superseded 必须带
// superseded_by（存在性在此层校验，decided_by 的 human 型由写入路径 + converge
// 双层校验——与 C2 的分工同构）。
type DesignDoc struct {
	Status       DesignStatus `yaml:"status"`
	Owner        string       `yaml:"owner"`
	Systems      []string     `yaml:"systems,omitempty"`
	RelatedItems []string     `yaml:"related_items,omitempty"`
	DecidedBy    string       `yaml:"decided_by,omitempty"`
	DecidedAt    string       `yaml:"decided_at,omitempty"`
	SupersededBy string       `yaml:"superseded_by,omitempty"`
}

// DesignInfo 是共识层只读模型（gityaml.ListDesigns 产出，Snapshot 携带，
// 不进 Index 查询面——S5 判决 v0.4 §4.7）。
type DesignInfo struct {
	Topic          string     // 目录名（主题）
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
	case DesignDraft, DesignAgreed, DesignSuperseded:
	case "":
		return fmt.Errorf("design status is required (draft|agreed|superseded)")
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
