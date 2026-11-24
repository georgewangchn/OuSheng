package model

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// decodeStrict 使用 KnownFields(true)：拒绝未知顶层键（仓库既定约束）。
func decodeStrict(b []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	return nil
}

func DecodeWorkItem(b []byte) (WorkItem, error) {
	var w WorkItem
	if err := decodeStrict(b, &w); err != nil {
		return WorkItem{}, err
	}
	return w, nil
}

func EncodeWorkItem(w WorkItem) ([]byte, error) {
	return yaml.Marshal(w)
}

func DecodeActorFile(b []byte) (ActorFile, error) {
	var f ActorFile
	if err := decodeStrict(b, &f); err != nil {
		return ActorFile{}, err
	}
	if err := ValidateActor(f.Actor); err != nil {
		return ActorFile{}, err
	}
	if f.SchemaVersion != 1 {
		return ActorFile{}, fmt.Errorf("actor file schema_version must be 1, got %d", f.SchemaVersion)
	}
	return f, nil
}

func EncodeActorFile(f ActorFile) ([]byte, error) {
	return yaml.Marshal(f)
}

func EncodeSystemsFile(f SystemsFile) ([]byte, error) {
	f.SchemaVersion = 1
	return yaml.Marshal(f)
}

func EncodeRolesFile(f RolesFile) ([]byte, error) {
	f.SchemaVersion = 1
	return yaml.Marshal(f)
}

func EncodeAssignmentsFile(f AssignmentsFile) ([]byte, error) {
	f.SchemaVersion = 1
	return yaml.Marshal(f)
}

func DecodeSystemsFile(b []byte) (SystemsFile, error) {
	var f SystemsFile
	if err := decodeStrict(b, &f); err != nil {
		return SystemsFile{}, err
	}
	if f.SchemaVersion != 1 {
		return SystemsFile{}, fmt.Errorf("systems schema_version must be 1, got %d", f.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, s := range f.Systems {
		if !lowerIDRe.MatchString(s.ID) {
			return SystemsFile{}, fmt.Errorf("invalid system id %q", s.ID)
		}
		if seen[s.ID] {
			return SystemsFile{}, fmt.Errorf("duplicate system id %q", s.ID)
		}
		seen[s.ID] = true
		if s.Parent != "" && s.Parent == s.ID {
			return SystemsFile{}, fmt.Errorf("system %s parents itself", s.ID)
		}
	}
	// parent 必须指向已声明 system
	for _, s := range f.Systems {
		if s.Parent != "" && !seen[s.Parent] {
			return SystemsFile{}, fmt.Errorf("system %s references unknown parent %s", s.ID, s.Parent)
		}
	}
	return f, nil
}

func DecodeRolesFile(b []byte) (RolesFile, error) {
	var f RolesFile
	if err := decodeStrict(b, &f); err != nil {
		return RolesFile{}, err
	}
	if f.SchemaVersion != 1 {
		return RolesFile{}, fmt.Errorf("roles schema_version must be 1, got %d", f.SchemaVersion)
	}
	seen := map[string]bool{}
	for _, r := range f.Roles {
		if !lowerIDRe.MatchString(r.ID) {
			return RolesFile{}, fmt.Errorf("invalid role id %q", r.ID)
		}
		if seen[r.ID] {
			return RolesFile{}, fmt.Errorf("duplicate role id %q", r.ID)
		}
		seen[r.ID] = true
	}
	return f, nil
}

func DecodeAssignmentsFile(b []byte) (AssignmentsFile, error) {
	var f AssignmentsFile
	if err := decodeStrict(b, &f); err != nil {
		return AssignmentsFile{}, err
	}
	if f.SchemaVersion != 1 {
		return AssignmentsFile{}, fmt.Errorf("assignments schema_version must be 1, got %d", f.SchemaVersion)
	}
	seen := map[string]bool{}
	for i, a := range f.Assignments {
		if err := ValidateAssignment(a); err != nil {
			return AssignmentsFile{}, fmt.Errorf("assignments[%d]: %w", i, err)
		}
		// 完全重复行拒绝：memory 与 sqlite 对重复行的处理不同（双记 vs 忽略），
		// canonical 层拒绝保证两实现一致（S5）
		key := a.Actor + "\x00" + a.Role + "\x00" + a.System + "\x00" + a.Responsibility
		if seen[key] {
			return AssignmentsFile{}, fmt.Errorf("assignments[%d]: duplicate assignment %s×%s×%s×%s", i, a.Actor, a.Role, a.System, a.Responsibility)
		}
		seen[key] = true
	}
	return f, nil
}

func DecodeProjectFile(b []byte) (ProjectFile, error) {
	var f ProjectFile
	if err := decodeStrict(b, &f); err != nil {
		return ProjectFile{}, err
	}
	if f.SchemaVersion != 1 {
		return ProjectFile{}, fmt.Errorf("project schema_version must be 1, got %d", f.SchemaVersion)
	}
	if !lowerIDRe.MatchString(f.Project.ID) {
		return ProjectFile{}, fmt.Errorf("invalid project id %q", f.Project.ID)
	}
	return f, nil
}

// WriteActivityJSONL 以 append-only 方式追加一条 Activity 记录。
func WriteActivityJSONL(w io.Writer, a Activity) error {
	b, err := jsonMarshal(a)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// BadLine 是读面跳过的一条坏行（审计流 best-effort 的告警原料，档案 §56）。
type BadLine struct {
	Line   int    // 1-based
	Reason string // 解码失败原因
	Raw    string // 坏行原文（冲突标记识别用；截断防巨行）
}

// ScanActivityFile 读取一个 jsonl 文件为 Activity 列表——坏行跳过并返回
// （file:行号），不静默丢弃：调用方负责响亮警告。
//
// best-effort 的理由（2026-11-24 实栈实锤）：审计流是投影输入不是事实源
// （D2），而 ListActivity 的调用点（status/report/plan/converge 审计输入）
// 一律把 activity 当非致命输入吞错——「坏行报错」的设计意图在调用点退化成
// 「整体静默归零」：一行冲突标记让全舰队 cruise status 报「交付 0 单」。
// 跳坏 + 响亮警告 = 与 work/actors/designs 三读面同一纪律（D4）。
func ScanActivityFile(path string) ([]Activity, []BadLine, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var out []Activity
	var bad []BadLine
	for i, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var a Activity
		if err := jsonUnmarshal(line, &a); err != nil {
			raw := []rune(string(line))
			if len(raw) > 40 {
				raw = append(raw[:40], '…') // 按 rune 截断（字节截断会切碎汉字）
			}
			bad = append(bad, BadLine{Line: i + 1, Reason: err.Error(), Raw: string(raw)})
			continue
		}
		out = append(out, a)
	}
	return out, bad, nil
}

// LoadActivityFile 读取一个 jsonl 文件为 Activity 列表（严格形态：遇坏行报错）。
// 读面走 ScanActivityFile（best-effort）；本函数留给需要「全有或全无」语义的调用方。
func LoadActivityFile(path string) ([]Activity, error) {
	acts, bad, err := ScanActivityFile(path)
	if err != nil {
		return nil, err
	}
	if len(bad) > 0 {
		return nil, fmt.Errorf("%s:%d: %s", path, bad[0].Line, bad[0].Reason)
	}
	return acts, nil
}

// Now 返回 RFC3339 时间戳（单一出口，测试可替换）。
// Now 用 RFC3339Nano：秒精度下同秒并列会破坏跨文件审计因果序
// （activity 按 月×actor 分文件后，跨文件同秒无 in-file append 序可依）。
var Now = func() string { return time.Now().Format(time.RFC3339Nano) }
