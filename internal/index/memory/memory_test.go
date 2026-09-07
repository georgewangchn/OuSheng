package memory

import (
	"testing"

	"ousheng/internal/index"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

func build(t *testing.T) index.Index {
	t.Helper()
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	snap, err := index.Load(repo)
	if err != nil {
		t.Fatal(err)
	}
	idx := New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestMemoryIndexQueries(t *testing.T) {
	idx := build(t)

	got, err := idx.ByAssignee("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("backend-agent should have 2 items, got %d", len(got))
	}
	got, err = idx.ActiveByActor("backend-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("backend-agent active should be 2, got %d", len(got))
	}
	got, err = idx.BySystem("datax-backend")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("datax-backend should have 2 items, got %d", len(got))
	}
	got, err = idx.ByVersion("v2.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("v2.0 should have 4 items, got %d", len(got))
	}
	got, err = idx.ByStatus(model.StatusDoing)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("doing should have 2 items, got %d", len(got))
	}
	got, err = idx.ByType(model.TypeBug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("bug type should have 1 item, got %d", len(got))
	}
	got, err = idx.ByAccountable("zhangsan")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("zhangsan accountable should be 2, got %d", len(got))
	}
	if _, ok, _ := idx.Get("BUG-017"); !ok {
		t.Fatal("BUG-017 missing")
	}
	if _, ok, _ := idx.Get("NOPE"); ok {
		t.Fatal("NOPE should not exist")
	}
}

func TestMemoryIndexBlockers(t *testing.T) {
	idx := build(t)
	// BUG-017 与 FEAT-CDC-001 都依赖 K8S-003（backlog → not-done）
	bs, err := idx.BlockersOf("BUG-017")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].DepID != "K8S-003" || bs[0].Reason != "not-done" {
		t.Fatalf("BUG-017 blockers wrong: %+v", bs)
	}
	// K8S-003 无依赖
	if bs, _ := idx.BlockersOf("K8S-003"); len(bs) != 0 {
		t.Fatalf("K8S-003 should have no blockers: %+v", bs)
	}
}

func TestMemoryIndexRegistries(t *testing.T) {
	idx := build(t)
	a, ok, _ := idx.Actor("backend-agent")
	if !ok || a.Actor.Type != model.ActorAgent || a.Manifest == nil {
		t.Fatalf("actor lookup failed: %+v", a)
	}
	if _, ok, _ := idx.System("datax-backend"); !ok {
		t.Fatal("system lookup failed")
	}
	if got, _ := idx.AssignmentsByActor("backend-agent"); len(got) != 1 {
		t.Fatalf("assignments by actor wrong: %d", len(got))
	}
	if got, _ := idx.AssignmentsBySystem("datax-backend"); len(got) != 4 {
		t.Fatalf("assignments by system wrong: %d", len(got))
	}
	if got, _ := idx.Roles(); len(got) != 7 {
		t.Fatalf("roles wrong: %d", len(got))
	}
}

func TestMemoryIndexMissingDep(t *testing.T) {
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	snap, _ := index.Load(repo)
	// 注入悬空依赖
	snap.WorkItems = append(snap.WorkItems, model.WorkItem{
		SchemaVersion: 2, ID: "X-1", Type: model.TypeTask, Title: "悬空",
		Status: model.StatusDoing, Revision: 1, DependsOn: []string{"GHOST-9"},
	})
	idx := New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	bs, err := idx.BlockersOf("X-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Reason != "missing" {
		t.Fatalf("missing dep not detected: %+v", bs)
	}
}
