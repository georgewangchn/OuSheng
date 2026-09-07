package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"ousheng/internal/board"
	"ousheng/internal/card"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

// setupV1Board 建一个含三张卡的 v1 board store（git repo + cards/）。
func setupV1Board(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	b := board.New(dir)
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}
	cards := []card.Card{
		{
			ID: "auth-api", Owner: "backend", Task: "auth API 上线",
			Status: card.Proposed, Version: 1,
			Contract: card.Contract{Kind: "http", Breaking: false, Interface: map[string]any{"path": "/auth"}},
		},
		{
			ID: "auth-probe", Owner: "tester", Task: "auth 探针",
			Status: card.Proposed, Version: 1,
			Contract: card.Contract{Kind: "cli", Breaking: false},
		},
		{
			ID: "legacy-queue", Owner: "infra", Task: "旧队列下线",
			Status: card.Proposed, Version: 1,
			Contract: card.Contract{Kind: "event", Breaking: true, Interface: map[string]any{"topic": "q1"}},
			HumanAck: &card.HumanAck{Approver: "zhangsan", AtVersion: 1},
		},
	}
	for i, c := range cards {
		if _, err := b.WriteBoard(c, 0); err != nil {
			t.Fatalf("seed card %d: %v", i, err)
		}
	}
	// 生命周期推进到目标状态（v1 状态机：proposed→agreed→live→verified）
	authAPI := cards[0]
	for _, s := range []card.Status{card.Agreed, card.Live} {
		authAPI.Status = s
		var err error
		authAPI, err = b.WriteBoard(authAPI, authAPI.Version)
		if err != nil {
			t.Fatalf("advance auth-api to %s: %v", s, err)
		}
	}
	if authAPI.Version != 3 {
		t.Fatalf("auth-api expected version 3, got %d", authAPI.Version)
	}
	authProbe := cards[1]
	authProbe.Contract = cards[1].Contract
	for _, s := range []card.Status{card.Agreed, card.Live} {
		authProbe.Status = s
		var err error
		authProbe, err = b.WriteBoard(authProbe, authProbe.Version)
		if err != nil {
			t.Fatalf("advance auth-probe to %s: %v", s, err)
		}
	}
	// live 态先补 evidence/依赖，再升级 verified（v1 规则：verified 需完整 evidence）
	authProbe.Evidence = &card.Evidence{Probe: "probe.sh", PassedAtCommit: "abc123", By: "tester"}
	authProbe.DependsOn = []string{"auth-api"}
	var err error
	authProbe, err = b.WriteBoard(authProbe, authProbe.Version)
	if err != nil {
		t.Fatal(err)
	}
	authProbe.Status = card.Verified
	authProbe, err = b.WriteBoard(authProbe, authProbe.Version)
	if err != nil {
		t.Fatal(err)
	}
	if authProbe.Version != 5 {
		t.Fatalf("auth-probe expected version 5, got %d", authProbe.Version)
	}
	return dir
}

func TestMigrateSchema(t *testing.T) {
	v1dir := setupV1Board(t)
	// v2 workspace 建在同一 git repo：fixtures 提供注册表（backend/tester 是 role，zhangsan 是 actor）
	wsdir := t.TempDir()
	if err := testfix.CopyWorkspace(wsdir); err != nil {
		t.Fatal(err)
	}
	// 把 v1 cards 拷进同一仓库（模拟同一项目演进）
	if err := os.MkdirAll(filepath.Join(wsdir, "cards"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(v1dir, "cards"))
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(v1dir, "cards", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsdir, "cards", e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repo := gityaml.Open(wsdir)
	cards, err := ReadCards(wsdir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("expected 3 cards, got %d", len(cards))
	}

	res, err := MigrateSchema(repo, cards)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Migrated) != 3 {
		t.Fatalf("expected 3 migrated, got %v", res.Migrated)
	}
	// backend/tester 是 role → acting_role + needs_resolution；infra 都不匹配 → needs_resolution
	if len(res.NeedsResolution) != 3 {
		t.Fatalf("expected 3 needs_resolution, got %v", res.NeedsResolution)
	}

	// 字段映射检查
	w, err := repo.GetWorkItem("auth-api")
	if err != nil {
		t.Fatal(err)
	}
	if w.Revision != 3 { // card.version → revision（绝不进 target_version）
		t.Fatalf("revision mismatch: %d", w.Revision)
	}
	if w.TargetVersion != "" {
		t.Fatalf("target_version must stay empty, got %q", w.TargetVersion)
	}
	if w.Title != "auth API 上线" || w.Status != model.StatusDoing {
		t.Fatalf("title/status mismatch: %+v", w)
	}
	if w.Contract == nil || w.Contract.Status != model.ContractLive || w.Contract.Kind != "http" {
		t.Fatalf("contract mismatch: %+v", w.Contract)
	}
	if w.ActingRole != "backend" || w.Assignee != "" {
		t.Fatalf("owner resolution mismatch: role=%q assignee=%q", w.ActingRole, w.Assignee)
	}
	if w.MigrationStatus != StatusNeedsResolution || w.LegacyOwner != "backend" {
		t.Fatalf("migration status mismatch: %+v", w)
	}

	// verified 卡：evidence 迁移为 typed；human_ack 迁移
	vw, err := repo.GetWorkItem("auth-probe")
	if err != nil {
		t.Fatal(err)
	}
	if vw.Status != model.StatusDone {
		t.Fatalf("verified card should map to done, got %s", vw.Status)
	}
	if len(vw.Evidence) != 1 || vw.Evidence[0].Type != model.EvidenceTestResult || vw.Evidence[0].Locator != "abc123" {
		t.Fatalf("evidence migration mismatch: %+v", vw.Evidence)
	}

	// breaking 卡：human_ack 保留
	bw, err := repo.GetWorkItem("legacy-queue")
	if err != nil {
		t.Fatal(err)
	}
	if bw.HumanAck == nil || bw.HumanAck.Approver != "zhangsan" || bw.HumanAck.AtRevision != 1 {
		t.Fatalf("human_ack migration mismatch: %+v", bw.HumanAck)
	}

	// v1 cards 未被删除
	if _, err := os.Stat(filepath.Join(wsdir, "cards", "auth-api.yaml")); err != nil {
		t.Fatal("v1 cards must survive migration")
	}

	// 幂等：重跑全跳过
	res2, err := MigrateSchema(repo, cards)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Skipped) != 3 || len(res2.Migrated) != 0 {
		t.Fatalf("idempotency broken: %+v", res2)
	}
}

func TestResolveOwner(t *testing.T) {
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	cards := []card.Card{{
		ID: "auth-api", Owner: "backend", Task: "auth API 上线",
		Status: card.Live, Version: 3,
		Contract: card.Contract{Kind: "http"},
	}}
	if _, err := MigrateSchema(repo, cards); err != nil {
		t.Fatal(err)
	}

	w, err := ResolveOwner(repo, "auth-api", "backend-agent", "backend", "zhangsan", "zhangsan")
	if err != nil {
		t.Fatal(err)
	}
	if w.Assignee != "backend-agent" || w.ActingRole != "backend" || w.AccountableHuman != "zhangsan" {
		t.Fatalf("resolve mismatch: %+v", w)
	}
	if w.MigrationStatus != "" || w.LegacyOwner != "" {
		t.Fatalf("migration residue: %+v", w)
	}

	// 非 needs_resolution 的对象拒绝 resolve
	if _, err := ResolveOwner(repo, "auth-api", "x", "y", "", "zhangsan"); err == nil {
		t.Fatal("double resolve must be rejected")
	}
}

func TestOwnerResolvesToActorWhenMatch(t *testing.T) {
	// owner 恰好是已注册 actor id → 直接 assignee
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	cards := []card.Card{{
		ID: "ui-polish", Owner: "ui-agent", Task: "UI 打磨",
		Status: card.Proposed, Version: 1,
		Contract: card.Contract{Kind: "lib"},
	}}
	res, err := MigrateSchema(repo, cards)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NeedsResolution) != 0 {
		t.Fatalf("expected clean resolution, got %v", res.NeedsResolution)
	}
	w, _ := repo.GetWorkItem("ui-polish")
	if w.Assignee != "ui-agent" || w.MigrationStatus != "" {
		t.Fatalf("assignee resolution mismatch: %+v", w)
	}
}
