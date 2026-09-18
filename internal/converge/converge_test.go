package converge

import (
	"strings"
	"testing"

	"ousheng/internal/card"
	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/migrate"
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

// checkFixtures 带固定语料的 Check（共识层输入为空：fixtures 无 designs）。
func checkFixtures(t *testing.T) (Result, error) {
	t.Helper()
	return Check(idxFromFixtures(t), Knowledge{})
}

// --- 共识层（v0.4）：design 审计 ---

func idxWithActors(t *testing.T, actors []model.ActorFile) index.Index {
	t.Helper()
	snap := index.Snapshot{
		Actors: actors,
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	return idx
}

var castActors = []model.ActorFile{
	{Actor: model.Actor{ID: "pm", Type: model.ActorHuman}},
	{Actor: model.Actor{ID: "be-agent", Type: model.ActorAgent}},
}

func di(topic, status, decidedBy, supersededBy string, systems, related []string) model.DesignInfo {
	return model.DesignInfo{Topic: topic, Design: model.DesignDoc{
		Status: model.DesignStatus(status), Owner: "be-agent", Systems: systems,
		RelatedItems: related, DecidedBy: decidedBy, SupersededBy: supersededBy,
	}}
}

// agent 自拍共识（agreed 但 decided_by 非 human）= BLOCKER（T10 §10.2，C2 血统）。
func TestDesignDecidedByNonHumanBlocks(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{di("export", "agreed", "be-agent", "", nil, nil)}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked || !contains(r.Blockers, "non-human") {
		t.Fatalf("agent-decided design must BLOCK, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestDesignDecidedByUnknownActorBlocks(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{di("export", "agreed", "ghost", "", nil, nil)}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked || !contains(r.Blockers, "unknown actor") {
		t.Fatalf("unknown decider must BLOCK, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestDesignDecidedByHumanConverges(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{di("export", "agreed", "pm", "", nil, nil)}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged || len(r.Warnings) != 0 {
		t.Fatalf("human-decided design must converge clean, got %s (%v)", r.Status, r.Warnings)
	}
}

// supersede 链环 = BLOCKER（镜像依赖环，T10 §10.3）。
func TestSupersedeCycleBlocks(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{
		di("a", "superseded", "", "b", nil, nil),
		di("b", "superseded", "", "a", nil, nil),
	}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked || !contains(r.Blockers, "supersede cycle") {
		t.Fatalf("supersede cycle must BLOCK, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestSupersedeDanglingWarns(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{di("a", "superseded", "", "ghost", nil, nil)}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged || !contains(r.Warnings, "dangling") {
		t.Fatalf("dangling successor must warn, got %s (%v)", r.Status, r.Warnings)
	}
}

func TestSupersedeNotEffectiveWarns(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{
		di("old", "superseded", "", "new", nil, nil),
		di("new", "draft", "", "", nil, nil),
	}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "not effective") {
		t.Fatalf("draft successor must warn not-effective, got %v", r.Warnings)
	}
}

// 超前曝光：work 开工（doing/testing/done）而方案仍 draft（§4.8 防线二）。
func TestWorkAheadOfDraftDesignWarns(t *testing.T) {
	w := wi("REQ-1", model.StatusDoing)
	idx := memory.New()
	if err := idx.Rebuild(index.Snapshot{Actors: castActors, WorkItems: []model.WorkItem{w}}); err != nil {
		t.Fatal(err)
	}
	kn := Knowledge{Designs: []model.DesignInfo{di("plan", "draft", "", "", nil, []string{"REQ-1"})}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != InProgress || !contains(r.Warnings, "running ahead of undecided design plan") {
		t.Fatalf("ahead-of-draft must warn, got %s (%v)", r.Status, r.Warnings)
	}
}

func TestRelatedItemsDanglingWarns(t *testing.T) {
	idx := idxWithActors(t, castActors)
	kn := Knowledge{Designs: []model.DesignInfo{di("plan", "agreed", "pm", "", nil, []string{"GHOST-1"})}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "related item GHOST-1 missing") {
		t.Fatalf("dangling related item must warn, got %v", r.Warnings)
	}
}

// architecture 覆盖（T10 §10.5）：注册系统无文档 → warning；文档无系统 → warning。
func TestArchitectureCoverageWarns(t *testing.T) {
	snap := index.Snapshot{
		Actors:  castActors,
		Systems: []model.System{{ID: "datax"}, {ID: "ui"}},
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	kn := Knowledge{Architecture: []string{"datax", "ghost-sys"}}
	r, err := Check(idx, kn)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "system ui: no architecture doc") {
		t.Fatalf("uncovered system must warn, got %v", r.Warnings)
	}
	if !contains(r.Warnings, "architecture ghost-sys.md: not a registered system") {
		t.Fatalf("orphan architecture must warn, got %v", r.Warnings)
	}
}

func contains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// checkK 无共识层输入的 Check（既有用例：只测 WorkItem 面）。
func checkK(t *testing.T, items []model.WorkItem) (Result, error) {
	t.Helper()
	return Check(idxFrom(t, items), Knowledge{})
}

// wi 构造「齐备」的任务单（有主 + 有验收描述）——各测试只关心自己的主题；
// 无主/信息不全的行为由 TestUnassigned*/TestIncompleteInfo* 专项锁定。
func wi(id string, status model.WorkStatus) model.WorkItem {
	return model.WorkItem{SchemaVersion: 2, ID: id, Type: model.TypeTask, Title: id,
		Status: status, Revision: 1, Assignee: "be-agent", Description: "验收：x"}
}

// done 项最后上报进度 < 1.0：警告但不阻塞（信号分辨力）。
func TestDoneWithPartialProgressWarns(t *testing.T) {
	w := wi("W-1", model.StatusDone)
	w.Progress = &model.ProgressReport{Value: 0.5, Actor: "a", ReportedAt: "2026-09-09T10:00:00+08:00", Basis: model.BasisManual}
	w.Evidence = []model.Evidence{{Type: model.EvidenceManualCheck, Source: "human", Locator: "review"}}
	r, err := checkK(t, []model.WorkItem{w})
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
	r, err := checkK(t, []model.WorkItem{w})
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
	r, err := checkK(t, []model.WorkItem{w})
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
	r, err := checkK(t, []model.WorkItem{w})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("doing at 50%% must not warn, got %v", r.Warnings)
	}
}

func TestFixturesInProgress(t *testing.T) {
	r, err := checkFixtures(t)
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
	r, err := checkK(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("empty board should be CONVERGED, got %s", r.Status)
	}
}

func TestAllDoneConverged(t *testing.T) {
	r, err := checkK(t, []model.WorkItem{wi("A-1", model.StatusDone), wi("B-1", model.StatusCancelled)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("all done should be CONVERGED, got %s (%v)", r.Status, r.Blockers)
	}
}

// done 零证据：基石 ASR 意图（防过早喊 done 污染下游）的可见化，只警告不阻塞。
func TestDoneWithoutEvidenceWarns(t *testing.T) {
	r, err := checkK(t, []model.WorkItem{wi("W-1", model.StatusDone)})
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
	r, err := checkK(t, []model.WorkItem{w})
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
		r, err := checkK(t, []model.WorkItem{w})
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
	r, err := checkK(t, []model.WorkItem{w})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("done + live contract is a legal terminal state, got warnings %v", r.Warnings)
	}
}

// C2 语义审计：breaking 由 agent ack = BLOCKED（防手改绕过写入路径的门）。
func TestBreakingAckedByNonHumanBlocks(t *testing.T) {
	w := wi("W-1", model.StatusDoing)
	w.Contract = &model.Contract{Kind: "http", Status: model.ContractProposed, Breaking: true}
	w.HumanAck = &model.HumanAck{Approver: "dev-agent"}
	snap := index.Snapshot{
		Actors:    []model.ActorFile{{Actor: model.Actor{ID: "dev-agent", Type: model.ActorAgent, DisplayName: "Dev"}}},
		WorkItems: []model.WorkItem{w},
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	r, err := Check(idx, Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("breaking acked by agent must BLOCK, got %s (%v)", r.Status, r.Blockers)
	}
	found := false
	for _, b := range r.Blockers {
		if strings.Contains(b, "non-human actor") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want non-human-ack blocker, got %v", r.Blockers)
	}
}

func TestExplicitBlocked(t *testing.T) {
	r, err := checkK(t, []model.WorkItem{wi("A-1", model.StatusBlocked)})
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
	r, err := checkK(t, []model.WorkItem{a})
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
	r, err := checkK(t, []model.WorkItem{a})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("done item's stale dep must not block, got %s (%v)", r.Status, r.Blockers)
	}
}

// 依赖目标被取消 = 永不满足，open 项必须 BLOCKED（比悬空更糟：missing 还可能重现，cancelled 不会）。
func TestDependsOnCancelledBlocks(t *testing.T) {
	a := wi("A-1", model.StatusDoing)
	a.DependsOn = []string{"B-1"}
	b := wi("B-1", model.StatusCancelled)
	r, err := checkK(t, []model.WorkItem{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("dep-on-cancelled should be BLOCKED, got %s", r.Status)
	}
	if !strings.Contains(strings.Join(r.Blockers, ";"), "dependency B-1 cancelled") {
		t.Fatalf("blockers wrong: %v", r.Blockers)
	}
}

// done 项依赖被取消的单：历史残留，不阻塞（与 stale dep 同理）。
func TestDoneItemWithCancelledDepNotBlocking(t *testing.T) {
	a := wi("A-1", model.StatusDone)
	a.DependsOn = []string{"B-1"}
	b := wi("B-1", model.StatusCancelled)
	r, err := checkK(t, []model.WorkItem{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Converged {
		t.Fatalf("done item's cancelled dep must not block, got %s (%v)", r.Status, r.Blockers)
	}
}

func TestCycle(t *testing.T) {
	a := wi("A-1", model.StatusDoing)
	a.DependsOn = []string{"B-1"}
	b := wi("B-1", model.StatusDoing)
	b.DependsOn = []string{"A-1"}
	r, err := checkK(t, []model.WorkItem{a, b})
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
	r, err := checkK(t, []model.WorkItem{a})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("verified without evidence should be BLOCKED, got %s", r.Status)
	}
	a.Evidence = []model.Evidence{{Type: model.EvidenceTestResult, Source: "t"}}
	r2, err := checkK(t, []model.WorkItem{a})
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
	r, err := checkK(t, []model.WorkItem{a})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked {
		t.Fatalf("breaking without ack should be BLOCKED, got %s", r.Status)
	}
	a.HumanAck = &model.HumanAck{Approver: "zhangsan"}
	r2, err := checkK(t, []model.WorkItem{a})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != InProgress {
		t.Fatalf("with ack should be IN_PROGRESS, got %s", r2.Status)
	}
}

// --- 发布条件（2026-09-18 推演）：地址 / 执行者 / 可判定 ---

// active 却无主 = 进行中是孤儿：BLOCKER（写路径只挡进入 doing，手改/遗留由审计兜）。
func TestActiveWorkWithoutAssigneeBlocks(t *testing.T) {
	for _, st := range []model.WorkStatus{model.StatusDoing, model.StatusTesting, model.StatusBlocked} {
		w := wi("W-1", st)
		w.Assignee = ""
		r, err := checkK(t, []model.WorkItem{w})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != Blocked || !contains(r.Blockers, "active work without assignee") {
			t.Fatalf("%s 无主必须 BLOCK，got %s (%v)", st, r.Status, r.Blockers)
		}
	}
}

// backlog/ready 无主 = 待认领池：只警告（默认指派，认领为补充）。
func TestUnassignedReadyWarns(t *testing.T) {
	for _, st := range []model.WorkStatus{model.StatusBacklog, model.StatusReady} {
		w := wi("W-1", st)
		w.Assignee = ""
		r, err := checkK(t, []model.WorkItem{w})
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != InProgress || !contains(r.Warnings, "unassigned") {
			t.Fatalf("%s 无主应 warn，got %s (%v)", st, r.Status, r.Warnings)
		}
	}
}

// ready 起按类型缺最小信息集 → warning（backlog 不查：草稿期不逼信息）。
func TestIncompleteInfoWarns(t *testing.T) {
	bug := wi("B-1", model.StatusReady)
	bug.Type = model.TypeBug
	bug.Description = ""
	bug.DetectedBy = ""
	rel := wi("R-1", model.StatusReady)
	rel.Type = model.TypeRelease
	rel.TargetVersion = ""
	draft := wi("B-2", model.StatusBacklog)
	draft.Type = model.TypeBug
	draft.Description = ""
	draft.DetectedBy = ""
	r, err := checkK(t, []model.WorkItem{bug, rel, draft})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "B-1: incomplete bug — missing description+detected_by") {
		t.Fatalf("bug 缺 description+detected_by 应 warn: %v", r.Warnings)
	}
	if !contains(r.Warnings, "R-1: incomplete release — missing target_version") {
		t.Fatalf("release 缺 target_version 应 warn: %v", r.Warnings)
	}
	for _, w := range r.Warnings {
		if strings.Contains(w, "B-2") {
			t.Fatalf("backlog 不应查信息完备: %v", r.Warnings)
		}
	}
}

// v1 迁移链锁：live 卡 owner 解析失败 → MigrateSchema 产出 needs_resolution
// 无主 doing → converge BLOCK（先 migrate resolve-owner 再 converge）。
// 用真实 MigrateSchema 产出而非手构 WorkItem——converge 不读 MigrationStatus，
// 手构版本与 doing 无主用例等价，锁不住链路。
func TestMigratedNeedsResolutionBlocks(t *testing.T) {
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	cards := []card.Card{{
		ID: "legacy-live", Owner: "ghost", Task: "v1 遗留 live 卡",
		Status: card.Live, Version: 1,
		Contract: card.Contract{Kind: "http"},
	}}
	if _, err := migrate.MigrateSchema(repo, cards); err != nil {
		t.Fatal(err)
	}
	w, err := repo.GetWorkItem("legacy-live")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != model.StatusDoing || w.Assignee != "" || w.MigrationStatus != migrate.StatusNeedsResolution {
		t.Fatalf("migrate 应产出无主 needs_resolution doing，got %+v", w)
	}
	r, err := checkK(t, []model.WorkItem{w})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != Blocked || !contains(r.Blockers, "active work without assignee") {
		t.Fatalf("迁移无主 active 必须 BLOCK，got %s (%v)", r.Status, r.Blockers)
	}
}
