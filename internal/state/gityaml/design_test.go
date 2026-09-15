package gityaml

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/internal/gityamltest"
	"ousheng/internal/model"
)

func newDesignRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "t")
	run("config", "user.email", "t@t.local")
	return Open(dir)
}

func writeDesign(t *testing.T, dir, topic, fm, body string) {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "designs", topic, "design.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\n"+fm+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListDesignsEmptyWorkspace(t *testing.T) {
	r := newDesignRepo(t)
	ds, err := r.ListDesigns()
	if err != nil || ds != nil {
		t.Fatalf("no designs dir must yield nil,nil; got %v, %v", ds, err)
	}
	a, err := r.ListArchitecture()
	if err != nil || a != nil {
		t.Fatalf("no architecture dir must yield nil,nil; got %v, %v", a, err)
	}
}

func TestListDesignsWithRounds(t *testing.T) {
	r := newDesignRepo(t)
	writeDesign(t, r.Dir, "export-csv",
		"status: draft\nowner: datax-agent\nsystems: [datax, ui]\n", "# 方案\n正文\n")
	td := filepath.Join(r.Dir, ".ousheng", "designs", "export-csv")
	os.WriteFile(filepath.Join(td, "round-1.md"), []byte("## datax-agent — 09-15\nok\n"), 0o644)
	os.WriteFile(filepath.Join(td, "round-2.md"), []byte("## ui-agent — 09-15\n异议\n## datax-agent — 09-15\n回应\n"), 0o644)
	// round-10 必须压过 round-2（数值序而非字典序）
	os.WriteFile(filepath.Join(td, "round-10.md"), []byte("## pm — 09-16\n拍\n"), 0o644)

	ds, err := r.ListDesigns()
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Topic != "export-csv" {
		t.Fatalf("designs wrong: %+v", ds)
	}
	d := ds[0]
	if !d.RoundExists {
		t.Fatal("round exists flag wrong")
	}
	if strings.Join(d.LatestSpeakers, ",") != "pm" {
		t.Fatalf("latest round must be round-10 with pm only, got %v", d.LatestSpeakers)
	}
}

func TestListDesignsNoRounds(t *testing.T) {
	r := newDesignRepo(t)
	writeDesign(t, r.Dir, "solo", "status: draft\nowner: a\n", "b\n")
	ds, err := r.ListDesigns()
	if err != nil {
		t.Fatal(err)
	}
	if ds[0].RoundExists || ds[0].LatestSpeakers != nil {
		t.Fatalf("no rounds must yield empty speakers: %+v", ds[0])
	}
}

func TestListDesignsMissingDesignMD(t *testing.T) {
	r := newDesignRepo(t)
	if err := os.MkdirAll(filepath.Join(r.Dir, ".ousheng", "designs", "broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ListDesigns(); err == nil || !strings.Contains(err.Error(), "missing design.md") {
		t.Fatalf("topic dir without design.md must error, got %v", err)
	}
}

func TestListDesignsBadFrontmatter(t *testing.T) {
	r := newDesignRepo(t)
	writeDesign(t, r.Dir, "bad", "status: nope\nowner: a\n", "b\n")
	if _, err := r.ListDesigns(); err == nil || !strings.Contains(err.Error(), "invalid design status") {
		t.Fatalf("corrupt frontmatter must error loudly, got %v", err)
	}
}

func TestListArchitecture(t *testing.T) {
	r := newDesignRepo(t)
	dir := filepath.Join(r.Dir, ".ousheng", "architecture")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "datax.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "ui.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644) // 非 md 忽略
	a, err := r.ListArchitecture()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a, ",") != "datax,ui" {
		t.Fatalf("architecture wrong: %v", a)
	}
}

func TestGetDesignRawTopicValidation(t *testing.T) {
	r := newDesignRepo(t)
	if _, _, err := r.GetDesignRaw("../escape"); err == nil || !strings.Contains(err.Error(), "invalid design topic") {
		t.Fatalf("path traversal must be rejected, got %v", err)
	}
	if _, _, err := r.GetDesignRaw("nonexistent"); err == nil {
		t.Fatal("missing design must error")
	}
}

func TestUpdateDesignRoundtripAndCommit(t *testing.T) {
	r := newDesignRepo(t)
	writeDesign(t, r.Dir, "export-csv",
		"status: draft\nowner: datax-agent\n", "# 方案\n\n正文含 secret-marker 不变。\n")
	d, body, err := r.GetDesignRaw("export-csv")
	if err != nil {
		t.Fatal(err)
	}
	d.Status = model.DesignAgreed
	d.DecidedBy = "pm"
	d.DecidedAt = "2026-09-15T10:00:00+08:00"
	acts := []model.Activity{{TS: model.Now(), Actor: "pm", Action: "design_decided", WorkItem: "export-csv"}}
	if err := r.UpdateDesign("export-csv", d, body, acts, "design: decide export-csv"); err != nil {
		t.Fatal(err)
	}
	d2, body2, err := r.GetDesignRaw("export-csv")
	if err != nil {
		t.Fatal(err)
	}
	if d2.Status != model.DesignAgreed || d2.DecidedBy != "pm" {
		t.Fatalf("update lost: %+v", d2)
	}
	if !strings.Contains(string(body2), "secret-marker") {
		t.Fatalf("body must survive verbatim: %q", body2)
	}
	log := gityamltest.GitLog(r.Dir, "--oneline")
	if !strings.Contains(log, "design: decide export-csv") {
		t.Fatalf("expected design commit in log:\n%s", log)
	}
}
