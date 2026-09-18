package context

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

// reloadRebuilt 重建 Service 索引（New 只在构造时装载一次快照）。
func reloadRebuilt(t *testing.T, repo *gityaml.Repo) *Service {
	t.Helper()
	s, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func svc(t *testing.T) *Service {
	t.Helper()
	dir := testfix.Setup(t)
	s, err := New(gityaml.Open(dir))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// §47 验收：backend-agent 无需扫描整个项目即可知道：
// 我是谁 / 最终负责人 / 角色 / 系统 / 版本 / 正在做什么 / 阻塞来自哪里。
func TestGetMyContext_AgentScenario(t *testing.T) {
	s := svc(t)
	c, err := s.GetMyContext("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	if c.Actor != "backend-agent" || c.ActorType != "agent" {
		t.Fatalf("identity wrong: %+v", c)
	}
	if c.ResponsibleHuman != "zhangsan" {
		t.Fatalf("responsible human wrong: %q", c.ResponsibleHuman)
	}
	if len(c.Roles) != 1 || c.Roles[0] != "backend" {
		t.Fatalf("roles wrong: %v", c.Roles)
	}
	if len(c.Systems) != 1 || c.Systems[0] != "datax-backend" {
		t.Fatalf("systems wrong: %v", c.Systems)
	}
	if c.TargetVersion != "v2.0" {
		t.Fatalf("target version wrong: %q", c.TargetVersion)
	}
	ids := map[string]bool{}
	for _, w := range c.ActiveWork {
		ids[w.ID] = true
	}
	if !ids["FEAT-CDC-001"] || !ids["BUG-017"] {
		t.Fatalf("active work wrong: %+v", c.ActiveWork)
	}
	if len(c.Blockers) != 1 || c.Blockers[0] != "K8S-003" {
		t.Fatalf("blockers wrong: %v", c.Blockers)
	}
}

// Progressive Disclosure 第一层：不含 evidence / 依赖明细 / 其他 actor 任务。
func TestGetMyContext_Layer1Minimal(t *testing.T) {
	s := svc(t)
	c, err := s.GetMyContext("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"evidence", "contract", "depends_on", "human_ack"} {
		if _, ok := m[forbidden]; ok {
			t.Fatalf("layer 1 must not contain %q: %s", forbidden, b)
		}
	}
	if len(b) > 1500 {
		t.Fatalf("layer 1 context too large: %d bytes", len(b))
	}
}

func TestGetMyContext_UnknownActor(t *testing.T) {
	s := svc(t)
	if _, err := s.GetMyContext("nobody"); err == nil {
		t.Fatal("unknown actor must error")
	}
}

// 场景测试发现：backlog/ready 任务必须出现在我的队列（session 启动第一时机），
// 否则执行者对未开始的工作失明（§26）。
func TestGetMyContext_BacklogInQueue(t *testing.T) {
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	s, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	// fixtures 中 wangwu 名下无任务；建一个 backlog 项验证入队
	w := model.WorkItem{SchemaVersion: 2, ID: "X-1", Type: model.TypeTask, Title: "排队中", Status: model.StatusBacklog, Assignee: "wangwu", Revision: 1}
	if _, err := repo.CreateWorkItem(w, nil, "test: create X-1"); err != nil {
		t.Fatal(err)
	}
	s = reloadRebuilt(t, repo)
	c, err := s.GetMyContext("wangwu")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range c.ActiveWork {
		if w.ID == "X-1" && w.Status == string(model.StatusBacklog) {
			found = true
		}
	}
	if !found {
		t.Fatalf("backlog item assigned to actor must appear in queue: %+v", c.ActiveWork)
	}
}

func TestGetActorContext_HumanScenario(t *testing.T) {
	s := svc(t)
	v, err := s.GetActorContext("zhangsan")
	if err != nil {
		t.Fatal(err)
	}
	if v.ActorType != "human" {
		t.Fatalf("type wrong: %s", v.ActorType)
	}
	if len(v.Agents) != 1 || v.Agents[0] != "backend-agent" {
		t.Fatalf("agents wrong: %v", v.Agents)
	}
	if len(v.ResponsibleSystems) != 1 || v.ResponsibleSystems[0] != "datax-backend" {
		t.Fatalf("responsible systems wrong: %v", v.ResponsibleSystems)
	}
	// zhangsan 问责中的工作（BUG-017 / FEAT-CDC-001）
	if len(v.AccountableFor) != 2 {
		t.Fatalf("accountable for wrong: %+v", v.AccountableFor)
	}
}

func TestGetSystemContext(t *testing.T) {
	s := svc(t)
	v, err := s.GetSystemContext("datax-backend")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Responsible) != 2 { // zhangsan(backend) + lisi(tester) accountable
		t.Fatalf("responsible wrong: %+v", v.Responsible)
	}
	if len(v.Executors) != 2 { // backend-agent + test-agent
		t.Fatalf("executors wrong: %+v", v.Executors)
	}
	if len(v.ActiveWork) != 2 {
		t.Fatalf("active work wrong: %+v", v.ActiveWork)
	}
	if v.OpenBugs != 1 {
		t.Fatalf("open bugs wrong: %d", v.OpenBugs)
	}
	if len(v.Blockers) != 1 || v.Blockers[0] != "K8S-003" {
		t.Fatalf("blockers wrong: %v", v.Blockers)
	}
}

func TestGetWorkItem_Layer2(t *testing.T) {
	s := svc(t)
	d, err := s.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Deps) != 1 || d.Deps[0].ID != "K8S-003" || d.Deps[0].Status != "backlog" {
		t.Fatalf("deps wrong: %+v", d.Deps)
	}
	if d.Assignee != "backend-agent" || d.DetectedBy != "test-agent" {
		t.Fatalf("detail wrong: %+v", d.WorkItem)
	}
}

func TestGetEvidence_Layer3(t *testing.T) {
	s := svc(t)
	evs, err := s.GetEvidence("FEAT-CDC-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Type != "git_commit" || evs[1].Type != "test_result" {
		t.Fatalf("evidence wrong: %+v", evs)
	}
}

func TestGetWorkItem_MissingDepVisible(t *testing.T) {
	s := svc(t)
	// K8S-003 无依赖；BUG-017 的依赖存在。构造缺失：直接查不存在 id。
	if _, err := s.GetWorkItem("GHOST-1"); err == nil {
		t.Fatal("missing work item must error")
	}
}

// work show 依赖摘要的字段名保真锁（2026-09-18 审计 P6）：WorkBrief 曾只有
// json tag，yaml.Marshal 把 DueOn/ProgressValue 渲染成 dueon/progressvalue。
// 有值字段必须以 due_on / progress_reported 渲染。
func TestWorkBriefYAMLFieldNames(t *testing.T) {
	d := WorkItemDetail{
		Deps: []WorkBrief{{ID: "K8S-003", Title: "x", Status: "doing", System: "s",
			Priority: "P1", DueOn: "2026-10-01", ProgressValue: 0.5}},
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`due_on: "2026-10-01"`, "progress_reported: 0.5", "priority: P1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("依赖摘要字段名/值错，缺 %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "dueon") || strings.Contains(s, "progressvalue") {
		t.Fatalf("字段名烂（缺 yaml tag）:\n%s", s)
	}
}
