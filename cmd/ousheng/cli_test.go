package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/internal/testfix"
)

func runCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append(append([]string{}, args...), "--dir", dir)
	var stdout, stderr bytes.Buffer
	code := run(full, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cli %v failed (code %d):\nstdout: %s\nstderr: %s", args, code, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func runCLIRaw(args []string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestCLIVersion(t *testing.T) {
	out, _, code := runCLIRaw([]string{"version"})
	if code != 0 || !strings.Contains(out, "ousheng 0.3.0") {
		t.Fatalf("version wrong: %q %d", out, code)
	}
}

func TestCLIInit(t *testing.T) {
	dir := t.TempDir()
	out := runCLI(t, dir, "init", "--project-id", "my-proj", "--project-name", "我的项目")
	if !strings.Contains(out, "initialized workspace") {
		t.Fatalf("init output wrong: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ousheng", "project.yaml")); err != nil {
		t.Fatal("project.yaml missing")
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".ousheng", "cache", ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "*") {
		t.Fatal("cache gitignore missing")
	}
	// converge 空工作区 → CONVERGED
	out = runCLI(t, dir, "converge")
	if !strings.Contains(out, "CONVERGED") {
		t.Fatalf("empty converge wrong: %q", out)
	}
}

func TestCLIContextMeScenario(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "context", "me", "--actor", "backend-agent")
	// §47 验收全要素
	for _, want := range []string{
		`"actor": "backend-agent"`,
		`"responsible_human": "zhangsan"`,
		`"backend"`,
		`"datax-backend"`,
		`"target_version": "v2.0"`,
		"FEAT-CDC-001",
		"BUG-017",
		"K8S-003",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("context me missing %q:\n%s", want, out)
		}
	}
}

func TestCLIWorkLifecycle(t *testing.T) {
	dir := testfix.Setup(t)

	// create
	out := runCLI(t, dir, "work", "create",
		"--id", "TASK-101", "--type", "task", "--title", "CLI 冒烟任务",
		"--system", "datax-backend", "--version", "v2.1",
		"--assignee", "backend-agent", "--role", "backend",
		"--accountable", "zhangsan", "--actor", "zhangsan")
	if !strings.Contains(out, "created TASK-101") {
		t.Fatalf("create wrong: %q", out)
	}

	// list filter
	out = runCLI(t, dir, "work", "list", "--system", "datax-backend", "--open")
	if !strings.Contains(out, "TASK-101") || !strings.Contains(out, "BUG-017") {
		t.Fatalf("list wrong:\n%s", out)
	}

	// status: backlog → ready → doing
	runCLI(t, dir, "work", "update", "TASK-101", "--status", "ready", "--actor", "backend-agent")
	runCLI(t, dir, "work", "update", "TASK-101", "--status", "doing", "--actor", "backend-agent")

	// progress report
	out = runCLI(t, dir, "progress", "report", "TASK-101",
		"--value", "0.5", "--actor", "backend-agent", "--basis", "subtasks")
	if !strings.Contains(out, "50%") {
		t.Fatalf("progress wrong: %q", out)
	}

	// evidence add（git_commit 经 adapter 验证：workspace 自身 HEAD）
	out = runCLI(t, dir, "evidence", "add", "TASK-101",
		"--type", "git_commit", "--locator", "HEAD", "--actor", "backend-agent")
	if !strings.Contains(out, "evidence added") {
		t.Fatalf("evidence add wrong: %q", out)
	}

	// evidence list
	out = runCLI(t, dir, "evidence", "list", "TASK-101")
	if !strings.Contains(out, "git_commit") {
		t.Fatalf("evidence list wrong: %q", out)
	}

	// work show 含 progress
	out = runCLI(t, dir, "work", "show", "TASK-101")
	if !strings.Contains(out, "title: CLI 冒烟任务") || !strings.Contains(out, "basis: subtasks") {
		t.Fatalf("work show wrong:\n%s", out)
	}

	// activity list
	out = runCLI(t, dir, "activity", "list")
	for _, want := range []string{"created", "status_changed", "progress_reported", "evidence_added"} {
		if !strings.Contains(out, want) {
			t.Fatalf("activity missing %s:\n%s", want, out)
		}
	}

	// view kanban
	out = runCLI(t, dir, "view", "kanban")
	if !strings.Contains(out, "== DOING") || !strings.Contains(out, "TASK-101") {
		t.Fatalf("kanban wrong:\n%s", out)
	}

	// converge: 有 open → IN_PROGRESS
	out = runCLI(t, dir, "converge")
	if !strings.Contains(out, "IN_PROGRESS") {
		t.Fatalf("converge wrong: %q", out)
	}
}

func TestCLIBugReport(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "bug", "report",
		"--id", "BUG-900", "--title", "CLI 报 bug",
		"--system", "datax-ui", "--detected-by", "test-agent", "--actor", "test-agent")
	if !strings.Contains(out, "reported BUG-900") {
		t.Fatalf("bug report wrong: %q", out)
	}
	out = runCLI(t, dir, "work", "show", "BUG-900")
	if !strings.Contains(out, "type: bug") || !strings.Contains(out, "detected_by: test-agent") {
		t.Fatalf("bug fields wrong:\n%s", out)
	}
}

func TestCLIRegistryViews(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "actor", "list")
	if !strings.Contains(out, "backend-agent") || !strings.Contains(out, "zhangsan") {
		t.Fatalf("actor list wrong:\n%s", out)
	}
	out = runCLI(t, dir, "system", "show", "datax-backend")
	if !strings.Contains(out, "responsible: zhangsan / backend") || !strings.Contains(out, "open_bugs: 1") {
		t.Fatalf("system show wrong:\n%s", out)
	}
	out = runCLI(t, dir, "assignment", "list")
	if !strings.Contains(out, "backend-agent") || !strings.Contains(out, "executor") {
		t.Fatalf("assignment list wrong:\n%s", out)
	}
	out = runCLI(t, dir, "view", "version", "--version", "v2.0")
	if !strings.Contains(out, "datax-backend / v2.0") {
		t.Fatalf("version view wrong:\n%s", out)
	}
}

func TestCLIMigrate(t *testing.T) {
	// v1 board → 同目录 migrate
	dir := t.TempDir()
	if err := seedV1Cards(t, dir); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, dir, "migrate", "schema")
	if !strings.Contains(out, "migrated: 1") || !strings.Contains(out, "needs owner resolution") {
		t.Fatalf("migrate wrong:\n%s", out)
	}
	// resolve
	out = runCLI(t, dir, "migrate", "resolve-owner", "auth-api",
		"--assignee", "backend-agent", "--role", "backend", "--actor", "zhangsan")
	if !strings.Contains(out, "resolved auth-api") {
		t.Fatalf("resolve wrong: %q", out)
	}
	// 幂等
	out = runCLI(t, dir, "migrate", "schema")
	if !strings.Contains(out, "skipped (already migrated): 1") {
		t.Fatalf("idempotency wrong:\n%s", out)
	}
}

func TestCLISyncNoRemote(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "sync")
	// 无 remote：pull skipped 但索引与看板照常
	if !strings.Contains(out, "index rebuilt: 4 work items") {
		t.Fatalf("sync index wrong:\n%s", out)
	}
	if !strings.Contains(out, "smart-lakehouse") {
		t.Fatalf("sync summary wrong:\n%s", out)
	}
}

func TestCLIIndexStatus(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "index", "status")
	if !strings.Contains(out, "memory index: 4 work items") {
		t.Fatalf("index status wrong:\n%s", out)
	}
	if !strings.Contains(out, "sqlite index: none") {
		t.Fatalf("index status should note zero-sqlite mode:\n%s", out)
	}
}

func TestCLICASConflictSurfaces(t *testing.T) {
	dir := testfix.Setup(t)
	// 两次同 expect 的 update，第二次必须冲突
	runCLI(t, dir, "work", "update", "K8S-003", "--status", "ready", "--actor", "wangwu")
	var stdout, stderr bytes.Buffer
	code := run([]string{"work", "update", "K8S-003", "--status", "ready", "--expect", "1", "--dir", dir, "--actor", "wangwu"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "conflict") {
		t.Fatalf("expected CAS conflict, code=%d stderr=%q", code, stderr.String())
	}
}
