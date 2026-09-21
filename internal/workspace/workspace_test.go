package workspace

import (
	"errors"
	"strings"
	"testing"

	"ousheng/internal/gityamltest"
	"ousheng/internal/model"
	"ousheng/internal/state"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

func openFixtures(t *testing.T) *Service {
	t.Helper()
	dir := testfix.Setup(t)
	return New(gityaml.Open(dir))
}

func newBug() model.WorkItem {
	return model.WorkItem{
		SchemaVersion:    2,
		ID:               "BUG-100",
		Type:             model.TypeBug,
		Title:            "新 bug",
		System:           "datax-backend",
		TargetVersion:    "v2.0",
		Assignee:         "backend-agent",
		ActingRole:       "backend",
		AccountableHuman: "zhangsan",
		DetectedBy:       "test-agent",
	}
}

func TestCreateStartsAtBacklog(t *testing.T) {
	s := openFixtures(t)
	w := newBug()
	w.Status = model.StatusDoing
	if _, err := s.Create(w, "test-agent"); err == nil {
		t.Fatal("non-backlog create must be rejected")
	}
	w.Status = ""
	got, err := s.Create(w, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusBacklog || got.Revision != 1 {
		t.Fatalf("bad create: %+v", got)
	}
}

func TestCreateRejectsBadRefs(t *testing.T) {
	s := openFixtures(t)

	w := newBug()
	w.System = "ghost-system"
	if _, err := s.Create(w, "test-agent"); err == nil || !strings.Contains(err.Error(), "unknown system") {
		t.Fatalf("expected unknown system error, got %v", err)
	}

	w = newBug()
	w.Assignee = "ghost-agent"
	if _, err := s.Create(w, "test-agent"); err == nil || !strings.Contains(err.Error(), "unknown assignee") {
		t.Fatalf("expected unknown assignee error, got %v", err)
	}

	w = newBug()
	w.AccountableHuman = "backend-agent" // agent 不能问责
	if _, err := s.Create(w, "test-agent"); err == nil || !strings.Contains(err.Error(), "type=human") {
		t.Fatalf("expected accountable-human-type error, got %v", err)
	}

	w = newBug()
	w.ActingRole = "wizard"
	if _, err := s.Create(w, "test-agent"); err == nil || !strings.Contains(err.Error(), "unknown acting_role") {
		t.Fatalf("expected unknown role error, got %v", err)
	}
}

func TestCASConflict(t *testing.T) {
	s := openFixtures(t)
	w, err := s.Create(newBug(), "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	// 第二个写者基于旧 revision
	stale := w
	stale.Title = "更新标题"
	if _, err := s.Update(stale, w.Revision, "backend-agent"); err != nil {
		t.Fatalf("first update at rev %d should succeed: %v", w.Revision, err)
	}
	if _, err := s.Update(stale, w.Revision, "backend-agent"); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("expected CAS conflict, got %v", err)
	}
}

func TestDuplicateCreate(t *testing.T) {
	s := openFixtures(t)
	if _, err := s.Create(newBug(), "test-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(newBug(), "test-agent"); !errors.Is(err, state.ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

func TestWorkTransitionEnforced(t *testing.T) {
	s := openFixtures(t)
	w, _ := s.Create(newBug(), "test-agent")

	// backlog -> doing 非法（必须先 ready）
	jump := w
	jump.Status = model.StatusDoing
	if _, err := s.Update(jump, w.Revision, "backend-agent"); err == nil {
		t.Fatal("backlog->doing must be rejected")
	}

	// backlog -> ready -> doing 合法
	ready := w
	ready.Status = model.StatusReady
	w2, err := s.Update(ready, w.Revision, "backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	doing := w2
	doing.Status = model.StatusDoing
	if _, err := s.Update(doing, w2.Revision, "backend-agent"); err != nil {
		t.Fatal(err)
	}
}

func TestContractTransitionEnforced(t *testing.T) {
	s := openFixtures(t)
	w, _ := s.Repo.GetWorkItem("FEAT-CDC-001") // contract: http live
	w.Contract.Status = model.ContractVerified
	// live -> verified 合法，但 verified 要求 evidence（已有）
	updated, err := s.Update(w, w.Revision, "backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	// verified -> proposed 非法
	bad := updated
	bad.Contract.Status = model.ContractProposed
	if _, err := s.Update(bad, updated.Revision, "backend-agent"); err == nil {
		t.Fatal("verified->proposed must be rejected")
	}
}

func TestProgressAndEvidenceAndAck(t *testing.T) {
	s := openFixtures(t)
	w, _ := s.Create(newBug(), "test-agent")
	w.Status = model.StatusReady
	w, _ = s.Update(w, w.Revision, "backend-agent")
	w.Status = model.StatusDoing
	w, _ = s.Update(w, w.Revision, "backend-agent")

	p := model.ProgressReport{
		Value: 0.5, Actor: "backend-agent",
		ReportedAt: model.Now(), Basis: model.BasisSubtasks,
	}
	w, err := s.ReportProgress(w.ID, p, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if w.Progress == nil || w.Progress.Value != 0.5 {
		t.Fatalf("progress not written: %+v", w.Progress)
	}

	ev := model.Evidence{Type: model.EvidenceTestResult, Source: "pytest", Locator: "BUG-100", Result: "failed"}
	w, err = s.AddEvidence(w.ID, ev, w.Revision, "backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Evidence) != 1 {
		t.Fatalf("evidence not appended: %+v", w.Evidence)
	}

	w, err = s.Ack(w.ID, "zhangsan", "确认修复方案", w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if w.HumanAck == nil || w.HumanAck.Approver != "zhangsan" {
		t.Fatalf("ack not written: %+v", w.HumanAck)
	}

	acts, err := s.Repo.ListActivity()
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, a := range acts {
		if a.WorkItem == "BUG-100" {
			actions = append(actions, a.Action)
		}
	}
	want := []string{"created", "status_changed", "status_changed", "progress_reported", "evidence_added", "human_acked"}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Fatalf("activity sequence mismatch:\n got %v\nwant %v", actions, want)
	}
}

func TestBootstrapWithoutRegistries(t *testing.T) {
	dir := t.TempDir()
	repo := gityaml.Open(dir)
	if err := repo.InitWorkspace(model.Project{ID: "solo", Name: "Solo"}); err != nil {
		t.Fatal(err)
	}
	s := New(repo)
	// 注册表为空：refs 校验跳过（bootstrap 弱介入）
	w := model.WorkItem{
		SchemaVersion: 2, ID: "TASK-1", Type: model.TypeTask, Title: "第一次工作",
		System: "anything", Assignee: "anyone",
	}
	if _, err := s.Create(w, "anyone"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Repo.GetWorkItem("TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.StatusBacklog {
		t.Fatalf("expected backlog, got %s", got.Status)
	}
}

func TestGitHistoryPerChange(t *testing.T) {
	dir := testfix.Setup(t)
	s := New(gityaml.Open(dir))
	if _, err := s.Create(newBug(), "test-agent"); err != nil {
		t.Fatal(err)
	}
	log := gityamltest.GitLog(dir, "--oneline")
	if !strings.Contains(log, "work: create BUG-100") {
		t.Fatalf("expected create commit in log:\n%s", log)
	}
}

// 发布条件（2026-09-18）：进行中（doing/testing/blocked）必有主——
// 统一规则，不为 testing/blocked 开特例（换人走 work assign，从不清空主）。
func TestActiveRequiresAssignee(t *testing.T) {
	s := openFixtures(t)
	base, err := s.Repo.GetWorkItem("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if base.Assignee == "" {
		t.Fatal("fixture BUG-017 应有主")
	}
	for _, st := range []model.WorkStatus{model.StatusDoing, model.StatusBlocked, model.StatusTesting} {
		w := base
		w.Status = st
		w.Assignee = ""
		if _, err := s.Update(w, base.Revision, "test-agent"); err == nil || !strings.Contains(err.Error(), "requires assignee") {
			t.Fatalf("%s 无主必须拒写，err=%v", st, err)
		}
	}
	w := base
	w.Status = model.StatusTesting
	if _, err := s.Update(w, base.Revision, "test-agent"); err != nil {
		t.Fatalf("有主 testing 应可写: %v", err)
	}
}

// 2026-09-21 审计事故锁（档案 §14）：activity 是审计线索，写路径身份硬门——
// 空 actor 曾由 MCP 入口静默写入 unknown 桶；未注册身份同样拒（审计与
// context 对不上）。CLI 与 MCP 共用此咽喉，故锁在这一层。
func TestWriteRequiresActor(t *testing.T) {
	s := openFixtures(t)

	// 空 actor → 拒（fail-closed，不许静默落 unknown 桶）
	if _, err := s.Create(newBug(), ""); err == nil || !strings.Contains(err.Error(), "acting actor required") {
		t.Fatalf("空 actor 必须拒，got err=%v", err)
	}
	// 未注册 actor → 拒
	if _, err := s.Create(newBug(), "ghost"); err == nil || !strings.Contains(err.Error(), "unknown actor") {
		t.Fatalf("未注册 actor 必须拒，got err=%v", err)
	}
	// 已注册 actor → 通过
	if _, err := s.Create(newBug(), "backend-agent"); err != nil {
		t.Fatalf("已注册 actor 应通过：%v", err)
	}

	// evidence 路径同门
	if _, err := s.AddEvidence("BUG-100", model.Evidence{Type: model.EvidenceManualCheck, Locator: "x"}, -1, ""); err == nil {
		t.Fatal("evidence 空 actor 必须拒")
	}
	// progress 路径（p.Actor）同门
	if _, err := s.ReportProgress("BUG-100", model.ProgressReport{Value: 0.5, Actor: "", Basis: model.BasisManual}, -1); err == nil {
		t.Fatal("progress 空 actor 必须拒")
	}
}
