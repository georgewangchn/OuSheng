package converge

import (
	"strings"
	"testing"

	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

func idxFromFixtures(t *testing.T) index.Index {
	t.Helper()
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	snap, err := index.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	return idx
}

func idxFrom(t *testing.T, items []model.WorkItem) index.Index {
	t.Helper()
	snap := index.Snapshot{WorkItems: items}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	return idx
}

func wi(id string, status model.WorkStatus) model.WorkItem {
	return model.WorkItem{SchemaVersion: 2, ID: id, Type: model.TypeTask, Title: id, Status: status, Revision: 1}
}

func TestFixturesInProgress(t *testing.T) {
	r, err := Check(idxFromFixtures(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != InProgress {
		t.Fatalf("fixtures should be IN_PROGRESS, got %s (%v)", r.Status, r.Blockers)
	}
	// K8S-003 backlog：BUG-017/FEAT-CDC-001 在等它 → 属于 waiting，未显式 blocked
	// §40 语义：waiting 是 IN_PROGRESS 内的正常依赖，不是 BLOCKED？
	// 不——"不存在 unresolved blocker" 是 CONVERGED 条件；waiting 不阻止 IN_PROGRESS。
	if len(r.Blockers) != 0 {
		t.Fatalf("waiting deps should not make status BLOCKED: %v", r.Blockers)
	}
}

func TestEmptyConverged(t *testing.T) {
	r, err := Check(idxFrom(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("empty board should be CONVERGED, got %s", r.Status)
	}
}

func TestAllDoneConverged(t *testing.T) {
	r, err := Check(idxFrom(t, []model.WorkItem{wi("A-1", model.StatusDone), wi("B-1", model.StatusCancelled)}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("all done should be CONVERGED, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestExplicitBlocked(t *testing.T) {
	r, err := Check(idxFrom(t, []model.WorkItem{wi("A-1", model.StatusBlocked)}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("explicit blocked should be BLOCKED, got %s", r.Status)
	}
	if len(r.Blockers) == 0 || !strings.Contains(r.Blockers[0], "A-1: blocked") {
		t.Fatalf("blockers wrong: %v", r.Blockers)
	}
}

func TestDanglingDep(t *testing.T) {
	a := wi("A-1", model.StatusDoing)
	a.DependsOn = []string{"GHOST"}
	r, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("dangling dep should be BLOCKED, got %s", r.Status)
	}
	if !strings.Contains(strings.Join(r.Blockers, ";"), "dangling dependency GHOST") {
		t.Fatalf("blockers wrong: %v", r.Blockers)
	}
}

func TestDoneItemWithStaleDepNotBlocking(t *testing.T) {
	// done 项的依赖不再阻塞收敛（历史依赖）
	a := wi("A-1", model.StatusDone)
	a.DependsOn = []string{"B-1"}
	r, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("done item's stale dep must not block, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestCycle(t *testing.T) {
	a := wi("A-1", model.StatusDoing)
	a.DependsOn = []string{"B-1"}
	b := wi("B-1", model.StatusDoing)
	b.DependsOn = []string{"A-1"}
	r, err := Check(idxFrom(t, []model.WorkItem{a, b}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked || len(r.Cycle) < 2 {
		t.Fatalf("cycle should be BLOCKED with cycle, got %+v", r)
	}
}

func TestVerifiedWithoutEvidence(t *testing.T) {
	a := wi("A-1", model.StatusDone)
	a.Contract = &model.Contract{Kind: "http", Status: model.ContractVerified}
	r, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("verified without evidence should be BLOCKED, got %s", r.Status)
	}
	a.Evidence = []model.Evidence{{Type: model.EvidenceTestResult, Source: "t"}}
	r2, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != Converged {
		t.Fatalf("with evidence should be CONVERGED, got %s (%v)", r2.Status, r2.Blockers)
	}
}

func TestBreakingWithoutAck(t *testing.T) {
	a := wi("A-1", model.StatusDoing)
	a.Contract = &model.Contract{Kind: "http", Breaking: true}
	r, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("breaking without ack should be BLOCKED, got %s", r.Status)
	}
	a.HumanAck = &model.HumanAck{Approver: "zhangsan"}
	r2, err := Check(idxFrom(t, []model.WorkItem{a}))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != InProgress {
		t.Fatalf("with ack should be IN_PROGRESS, got %s", r2.Status)
	}
}
