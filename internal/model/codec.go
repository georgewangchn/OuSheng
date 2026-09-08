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

// LoadActivityFile 读取一个 jsonl 文件为 Activity 列表（坏行报错，不静默丢弃）。
func LoadActivityFile(path string) ([]Activity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Activity
	for i, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var a Activity
		if err := jsonUnmarshal(line, &a); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		out = append(out, a)
	}
	return out, nil
}

// Now 返回 RFC3339 时间戳（单一出口，测试可替换）。
var Now = func() string { return time.Now().Format(time.RFC3339) }
