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

// done 项最后上报进度 < 1.0：警告但不阻塞（信号分辨力）。
func TestDoneWithPartialProgressWarns(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Progress = &model.ProgressReport{Value: 0.5, Actor: "a", ReportedAt: "2026-09-09T10:00:00+08:00", Basis: model.BasisManual}
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("partial-progress done must not block, got %s", r.Status)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "W-1") || !strings.Contains(r.Warnings[0], "50%") {
		t.Fatalf("want 1 warning for W-1 50%%, got %v", r.Warnings)
	}
}

func TestDoneWithFullProgressNoWarning(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Progress = &model.ProgressReport{Value: 1.0, Actor: "a", ReportedAt: "2026-09-09T10:00:00+08:00", Basis: model.BasisManual}
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("full-progress done must not warn, got %v", r.Warnings)
	}
}

// 无上报的 done 不算矛盾：progress 是可选的诚实汇报，不强制。
func TestDoneWithoutProgressNoWarning(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("done without progress report must not warn, got %v", r.Warnings)
	}
}

// doing 项部分进度是常态，不警告。
func TestDoingPartialProgressNoWarning(t *testing.T) {
	w := wi("W-1", model.StatusDoing)
	w.Progress = &model.ProgressReport{Value: 0.5, Actor: "a", ReportedAt: "2026-09-09T10:00:00+08:00", Basis: model.BasisManual}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("doing at 50%% must not warn, got %v", r.Warnings)
	}
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

// done 零证据：基石 ASR 意图（防过早喊 done 污染下游）的可见化，只警告不阻塞。
func TestDoneWithoutEvidenceWarns(t *testing.T) {
	r, err := Check(idxFrom(t, []model.WorkItem{wi("W-1", model.StatusDone)}))
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("zero-evidence done must not block, got %s", r.Status)
	}
	found := false
	for _, warn := range r.Warnings {
		if strings.Contains(warn, "done without evidence") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want done-without-evidence warning, got %v", r.Warnings)
	}
}

func TestDoneWithEvidenceNoEvidenceWarning(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	for _, warn := range r.Warnings {
		if strings.Contains(warn, "without evidence") {
			t.Fatalf("evidence present, must not warn: %v", r.Warnings)
		}
	}
}

// 跨状态机漂移：关闭的工作携带 proposed 契约（done=实施超前于共识；cancelled=悬空提案）。
func TestClosedWorkWithProposedContractWarns(t *testing.T) {
	for _, st := range []model.WorkStatus{model.StatusDone, model.StatusCancelled} {
		w := wi("W-1", st)
		w.Contract = &model.Contract{Kind: "http", Status: model.ContractProposed}
		r, err := Check(idxFrom(t, []model.WorkItem{w}))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, warn := range r.Warnings {
			if strings.Contains(warn, "contract still proposed on closed work") {
				found = true
			}
		}
		if !found {
			t.Fatalf("status %s: want proposed-on-closed warning, got %v", st, r.Warnings)
		}
	}
}

// work done + contract live 是合法终态（契约生命周期长于工作），不警告。
func TestDoneWorkWithLiveContractNoWarning(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Contract = &model.Contract{Kind: "http", Status: model.ContractLive}
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := Check(idxFrom(t, []model.WorkItem{w}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("done + live contract is a legal terminal state, got warnings %v", r.Warnings)
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
