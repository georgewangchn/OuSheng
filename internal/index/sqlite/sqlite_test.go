package sqlite

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

func loadSnapshot(t *testing.T) index.Snapshot {
	t.Helper()
	dir := testfix.Setup(t)
	snap, err := index.Load(gityaml.Open(dir))
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func buildBoth(t *testing.T) (*memory.MemIndex, *SQLiteIndex) {
	t.Helper()
	snap := loadSnapshot(t)
	mem := memory.New()
	if err := mem.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "index.db")
	sidx, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sidx.Close() })
	if err := sidx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	return mem, sidx
}

// S5 硬约束：memory 与 sqlite 查询结果完全一致。
func TestEquivalenceWithMemory(t *testing.T) {
	mem, sidx := buildBoth(t)

	compareIDs := func(name string, memItems []model.WorkItem, memErr error, sqliteItems []model.WorkItem, sqErr error) {
		t.Helper()
		if memErr != nil || sqErr != nil {
			t.Fatalf("%s: errors %v / %v", name, memErr, sqErr)
		}
		var mIDs, sIDs []string
		for _, w := range memItems {
			mIDs = append(mIDs, w.ID)
		}
		for _, w := range sqliteItems {
			sIDs = append(sIDs, w.ID)
		}
		if !reflect.DeepEqual(mIDs, sIDs) {
			t.Fatalf("%s: ids differ\n mem: %v\n sq:  %v", name, mIDs, sIDs)
		}
		// payload 全等（含 status/progress/evidence）
		if !reflect.DeepEqual(memItems, sqliteItems) {
			t.Fatalf("%s: items differ\n mem: %+v\n sq:  %+v", name, memItems, sqliteItems)
		}
	}

	m, me := mem.All()
	s, se := sidx.All()
	compareIDs("All", m, me, s, se)

	m, me = mem.ByAssignee("backend-agent")
	s, se = sidx.ByAssignee("backend-agent")
	compareIDs("ByAssignee", m, me, s, se)

	m, me = mem.ByAccountable("zhangsan")
	s, se = sidx.ByAccountable("zhangsan")
	compareIDs("ByAccountable", m, me, s, se)

	m, me = mem.BySystem("datax-backend")
	s, se = sidx.BySystem("datax-backend")
	compareIDs("BySystem", m, me, s, se)

	m, me = mem.ByVersion("v2.0")
	s, se = sidx.ByVersion("v2.0")
	compareIDs("ByVersion", m, me, s, se)

	m, me = mem.ByStatus(model.StatusDoing)
	s, se = sidx.ByStatus(model.StatusDoing)
	compareIDs("ByStatus", m, me, s, se)

	m, me = mem.ByType(model.TypeBug)
	s, se = sidx.ByType(model.TypeBug)
	compareIDs("ByType", m, me, s, se)

	m, me = mem.ActiveByActor("backend-agent")
	s, se = sidx.ActiveByActor("backend-agent")
	compareIDs("ActiveByActor", m, me, s, se)

	// Get 命中与未命中
	mw, mok, _ := mem.Get("BUG-017")
	sw, sok, serr := sidx.Get("BUG-017")
	if serr != nil || mok != sok || !reflect.DeepEqual(mw, sw) {
		t.Fatalf("Get differ: %+v/%v vs %+v/%v (%v)", mw, mok, sw, sok, serr)
	}
	_, mok, _ = mem.Get("NOPE")
	_, sok, serr = sidx.Get("NOPE")
	if serr != nil || mok != sok {
		t.Fatalf("Get miss differ")
	}

	// Blockers
	mbs, _ := mem.BlockersOf("BUG-017")
	sbs, sqErr := sidx.BlockersOf("BUG-017")
	if sqErr != nil || !reflect.DeepEqual(mbs, sbs) {
		t.Fatalf("BlockersOf differ: %+v vs %+v (%v)", mbs, sbs, sqErr)
	}

	// Registry
	ma, mok, _ := mem.Actor("backend-agent")
	sa, sok, serr := sidx.Actor("backend-agent")
	if serr != nil || mok != sok || !reflect.DeepEqual(ma, sa) {
		t.Fatalf("Actor differ")
	}
	mact, _ := mem.Actors()
	sact, sqErr := sidx.Actors()
	if sqErr != nil || !reflect.DeepEqual(mact, sact) {
		t.Fatalf("Actors differ")
	}
	msys, mok, _ := mem.System("datax-backend")
	ssys, sok, serr := sidx.System("datax-backend")
	if serr != nil || mok != sok || !reflect.DeepEqual(msys, ssys) {
		t.Fatalf("System differ")
	}
	mroles, _ := mem.Roles()
	sroles, sqErr := sidx.Roles()
	if sqErr != nil || !reflect.DeepEqual(mroles, sroles) {
		t.Fatalf("Roles differ")
	}
	mas, _ := mem.AssignmentsByActor("backend-agent")
	sas, sqErr := sidx.AssignmentsByActor("backend-agent")
	if sqErr != nil || !reflect.DeepEqual(mas, sas) {
		t.Fatalf("AssignmentsByActor differ")
	}
	mss, _ := mem.AssignmentsBySystem("datax-backend")
	sss, sqErr := sidx.AssignmentsBySystem("datax-backend")
	if sqErr != nil || !reflect.DeepEqual(mss, sss) {
		t.Fatalf("AssignmentsBySystem differ")
	}
}

// S5：rm index.db → rebuild → 查询结果与重建前一致。
func TestDeleteAndRebuildIdentical(t *testing.T) {
	snap := loadSnapshot(t)
	dbPath := filepath.Join(t.TempDir(), "index.db")

	sidx, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sidx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	before, err := sidx.All()
	if err != nil {
		t.Fatal(err)
	}
	sidx.Close()

	// rm .ousheng/cache/index.db
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	// ousheng index rebuild
	sidx2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sidx2.Close()
	if err := sidx2.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	after, err := sidx2.All()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("S5 violated: results differ after rm + rebuild")
	}
}

// 悬空依赖在 sqlite 与 memory 语义一致（missing）。
func TestMissingDepSemantics(t *testing.T) {
	snap := loadSnapshot(t)
	snap.WorkItems = append(snap.WorkItems, model.WorkItem{
		SchemaVersion: 2, ID: "X-9", Type: model.TypeTask, Title: "悬空",
		Status: model.StatusDoing, Revision: 1, DependsOn: []string{"GHOST"},
	})
	mem := memory.New()
	if err := mem.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	sidx, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sidx.Close()
	if err := sidx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	mbs, _ := mem.BlockersOf("X-9")
	sbs, err := sidx.BlockersOf("X-9")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mbs, sbs) {
		t.Fatalf("missing dep semantics differ: %+v vs %+v", mbs, sbs)
	}
}
