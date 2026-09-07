package model

import "testing"

func TestCanWorkTransition(t *testing.T) {
	ok := []struct{ from, to WorkStatus }{
		{StatusBacklog, StatusReady},
		{StatusBacklog, StatusCancelled},
		{StatusReady, StatusDoing},
		{StatusDoing, StatusBlocked},
		{StatusDoing, StatusTesting},
		{StatusDoing, StatusDone},
		{StatusBlocked, StatusDoing},
		{StatusTesting, StatusDone},
		{StatusTesting, StatusDoing},
		{StatusDone, StatusDoing},
		{StatusCancelled, StatusBacklog},
		{StatusDoing, StatusDoing},
	}
	for _, c := range ok {
		if !CanWorkTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s allowed", c.from, c.to)
		}
	}
	bad := []struct{ from, to WorkStatus }{
		{StatusBacklog, StatusDoing},   // 必须先 ready
		{StatusBacklog, StatusDone},    // 不许跳级完成
		{StatusReady, StatusDone},      // 必须经过 doing
		{StatusReady, StatusBlocked},   // 未开始无从 blocked
		{StatusDone, StatusReady},      // reopen 只回 doing
		{StatusDone, StatusCancelled},  // 终态只能 reopen
		{StatusCancelled, StatusDoing}, // reopen 回 backlog
		{StatusTesting, StatusBlocked}, // testing 只回 doing 或进 done
	}
	for _, c := range bad {
		if CanWorkTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s rejected", c.from, c.to)
		}
	}
}

func TestCanContractTransition(t *testing.T) {
	ok := []struct{ from, to ContractStatus }{
		{ContractProposed, ContractAgreed},
		{ContractAgreed, ContractLive},
		{ContractLive, ContractVerified},
		{ContractVerified, ContractLive}, // 回归
		{ContractProposed, ContractDeprecated},
		{ContractLive, ContractDeprecated},
		{ContractLive, ContractLive},
	}
	for _, c := range ok {
		if !CanContractTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s allowed", c.from, c.to)
		}
	}
	bad := []struct{ from, to ContractStatus }{
		{ContractProposed, ContractLive},     // 必须先 agreed
		{ContractProposed, ContractVerified}, // 不许跳级
		{ContractDeprecated, ContractProposed},
	}
	for _, c := range bad {
		if CanContractTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s rejected", c.from, c.to)
		}
	}
}

func TestActorValidation(t *testing.T) {
	human := Actor{ID: "zhangsan", Type: ActorHuman, DisplayName: "张三"}
	if err := ValidateActor(human); err != nil {
		t.Fatal(err)
	}
	agent := Actor{ID: "backend-agent", Type: ActorAgent, ResponsibleHuman: "zhangsan"}
	if err := ValidateActor(agent); err != nil {
		t.Fatal(err)
	}
	noOwner := Actor{ID: "backend-agent", Type: ActorAgent}
	if err := ValidateActor(noOwner); err == nil {
		t.Fatal("agent without responsible_human must be rejected")
	}
	badType := Actor{ID: "x", Type: ActorType("robot")}
	if err := ValidateActor(badType); err == nil {
		t.Fatal("unknown actor type must be rejected")
	}
}

func TestDecodeActorFileStrict(t *testing.T) {
	raw := `schema_version: 1
actor:
  id: backend-agent
  type: agent
  display_name: Backend Agent
  responsible_human: zhangsan
manifest:
  default_role: backend
  systems:
    - datax-backend
  target_version: v2.0
`
	f, err := DecodeActorFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Actor.ID != "backend-agent" || f.Manifest == nil || f.Manifest.DefaultRole != "backend" {
		t.Fatalf("bad actor file: %+v", f)
	}
	bad := `schema_version: 1
actor:
  id: backend-agent
  type: agent
  responsible_human: zhangsan
  bogus: 1
`
	if _, err := DecodeActorFile([]byte(bad)); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestDecodeSystemsFile(t *testing.T) {
	raw := `schema_version: 1
systems:
  - id: data-platform
    name: 数据平台
  - id: datax-backend
    name: DataX Backend
    parent: data-platform
    repositories:
      - github.com/example/datax-backend
`
	f, err := DecodeSystemsFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Systems) != 2 || f.Systems[1].Parent != "data-platform" {
		t.Fatalf("bad systems: %+v", f)
	}
	dup := `schema_version: 1
systems:
  - id: a
  - id: a
`
	if _, err := DecodeSystemsFile([]byte(dup)); err == nil {
		t.Fatal("duplicate system id must be rejected")
	}
	orphan := `schema_version: 1
systems:
  - id: a
    parent: ghost
`
	if _, err := DecodeSystemsFile([]byte(orphan)); err == nil {
		t.Fatal("unknown parent must be rejected")
	}
	self := `schema_version: 1
systems:
  - id: a
    parent: a
`
	if _, err := DecodeSystemsFile([]byte(self)); err == nil {
		t.Fatal("self parent must be rejected")
	}
}

func TestDecodeAssignmentsFile(t *testing.T) {
	raw := `schema_version: 1
assignments:
  - actor: backend-agent
    role: backend
    system: datax-backend
    responsibility: executor
    active: true
  - actor: zhangsan
    role: backend
    system: datax-backend
    responsibility: accountable
    active: true
`
	f, err := DecodeAssignmentsFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Assignments) != 2 || f.Assignments[0].Responsibility != ResponsibilityExecutor {
		t.Fatalf("bad assignments: %+v", f)
	}
	bad := `schema_version: 1
assignments:
  - actor: backend-agent
    role: backend
    system: datax-backend
`
	if _, err := DecodeAssignmentsFile([]byte(bad)); err == nil {
		t.Fatal("missing responsibility must be rejected")
	}
}

func TestDecodeProjectFile(t *testing.T) {
	raw := `schema_version: 1
project:
  id: smart-lakehouse
  name: 智能湖仓
`
	f, err := DecodeProjectFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if f.Project.ID != "smart-lakehouse" || f.Project.Name != "智能湖仓" {
		t.Fatalf("bad project: %+v", f)
	}
}

func TestActivityJSONLRoundTrip(t *testing.T) {
	var buf []byte
	w := &sliceWriter{&buf}
	entries := []Activity{
		{TS: "2026-09-07T09:00:00+08:00", Actor: "backend-agent", Action: "started", WorkItem: "FEAT-CDC-001"},
		{TS: "2026-09-07T11:00:00+08:00", Actor: "backend-agent", Action: "progress_reported", WorkItem: "FEAT-CDC-001", Detail: "0.6"},
	}
	for _, a := range entries {
		if err := WriteActivityJSONL(w, a); err != nil {
			t.Fatal(err)
		}
	}
	// 写临时文件再读回
	tmp := t.TempDir() + "/act.jsonl"
	if err := writeFile(tmp, buf); err != nil {
		t.Fatal(err)
	}
	got, err := LoadActivityFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Detail != "0.6" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
