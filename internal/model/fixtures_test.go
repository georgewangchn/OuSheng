package model

import (
	"os"
	"path/filepath"
	"testing"
)

// fixtureDir 是五场景 fixture 工作区（§44/§45）：
// Requirement / Bug / Progress / Agent Context / Board Projection 全部由这份数据驱动。
const fixtureDir = "../../fixtures/lakehouse"

func TestFixturesLoad(t *testing.T) {
	pf, err := DecodeProjectFile(mustRead(t, filepath.Join(fixtureDir, "project.yaml")))
	if err != nil {
		t.Fatalf("project.yaml: %v", err)
	}
	if pf.Project.ID != "smart-lakehouse" {
		t.Fatalf("unexpected project %+v", pf.Project)
	}

	sf, err := DecodeSystemsFile(mustRead(t, filepath.Join(fixtureDir, "systems.yaml")))
	if err != nil {
		t.Fatalf("systems.yaml: %v", err)
	}
	if len(sf.Systems) == 0 {
		t.Fatal("systems fixture empty")
	}

	if _, err := DecodeRolesFile(mustRead(t, filepath.Join(fixtureDir, "roles.yaml"))); err != nil {
		t.Fatalf("roles.yaml: %v", err)
	}
	if _, err := DecodeAssignmentsFile(mustRead(t, filepath.Join(fixtureDir, "assignments.yaml"))); err != nil {
		t.Fatalf("assignments.yaml: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(fixtureDir, "actors"))
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string]bool{}
	for _, e := range entries {
		f, err := DecodeActorFile(mustRead(t, filepath.Join(fixtureDir, "actors", e.Name())))
		if err != nil {
			t.Fatalf("actors/%s: %v", e.Name(), err)
		}
		actors[f.Actor.ID] = true
	}
	if len(actors) != 6 {
		t.Fatalf("expected 6 actors, got %d", len(actors))
	}

	// 所有 work 文件可解码、可校验、互相引用合法
	wentries, err := os.ReadDir(filepath.Join(fixtureDir, "work"))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range wentries {
		raw := mustRead(t, filepath.Join(fixtureDir, "work", e.Name()))
		w, err := DecodeWorkItem(raw)
		if err != nil {
			t.Fatalf("work/%s decode: %v", e.Name(), err)
		}
		if err := ValidateWorkItem(w, raw); err != nil {
			t.Fatalf("work/%s validate: %v", e.Name(), err)
		}
		ids[w.ID] = true
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 work items, got %d", len(ids))
	}
	// 引用完整性：deps / assignee / accountable / system 均可解析
	for _, e := range wentries {
		raw := mustRead(t, filepath.Join(fixtureDir, "work", e.Name()))
		w, _ := DecodeWorkItem(raw)
		for _, dep := range w.DependsOn {
			if !ids[dep] {
				t.Errorf("%s depends on missing %s", w.ID, dep)
			}
		}
		if w.System != "" && !systemExists(sf, w.System) {
			t.Errorf("%s references missing system %s", w.ID, w.System)
		}
		if w.Assignee != "" && !actors[w.Assignee] {
			t.Errorf("%s references missing actor %s", w.ID, w.Assignee)
		}
		if w.AccountableHuman != "" && !actors[w.AccountableHuman] {
			t.Errorf("%s references missing accountable_human %s", w.ID, w.AccountableHuman)
		}
	}
}

func systemExists(sf SystemsFile, id string) bool {
	for _, s := range sf.Systems {
		if s.ID == id {
			return true
		}
	}
	return false
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
