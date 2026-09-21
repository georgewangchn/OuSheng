package main

import (
	"bytes"
	"os"
	"os/exec"
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
	if code != 0 || !strings.Contains(out, "ousheng 0.3.1") {
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

// 2026-09-20 BUG-001 事故锁：sync 只 pull 不 push，226 发单后提交烂在本地、
// 全舰队不可见直至人工推送。sync 必须在 pull 成功后 push 收口（软失败不阻塞）。
func TestCLISyncPushesLocalCommits(t *testing.T) {
	dir := testfix.Setup(t)
	remote := filepath.Join(t.TempDir(), "central.git")
	g := func(cwd string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	g(dir, "init", "--bare", remote)
	g(dir, "remote", "add", "origin", remote)
	g(dir, "push", "-q", "-u", "origin", "main")
	// ousheng 写路径造本地未推提交（同 BUG-001 事故形态：bug/todo 自动提交后无人推）
	if err := os.WriteFile(filepath.Join(dir, ".ousheng", "me"), []byte("zhangsan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, dir, "bug", "report", "--id", "BUG-100", "--title", "推送收口验证",
		"--system", "lakehouse-k8s", "--detected-by", "backend-agent")
	localHead := strings.TrimSpace(g(dir, "rev-parse", "HEAD"))
	out := runCLI(t, dir, "sync")
	if strings.Contains(out, "push skipped") {
		t.Fatalf("push 不应失败:\n%s", out)
	}
	if remoteHead := strings.TrimSpace(g(remote, "rev-parse", "main")); remoteHead != localHead {
		t.Fatalf("sync 未推送本地提交：remote=%s local=%s\n%s", remoteHead, localHead, out)
	}
	// 幂等：无新提交时 sync 静默（不误报 push skipped）
	out = runCLI(t, dir, "sync")
	if strings.Contains(out, "push skipped") {
		t.Fatalf("无未推提交的 sync 应静默:\n%s", out)
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

func TestCLIIndexRebuildWritesSQLite(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "index", "rebuild")
	if !strings.Contains(out, "index rebuilt: 4 work items") {
		t.Fatalf("rebuild wrong:\n%s", out)
	}
	db := filepath.Join(dir, ".ousheng", "cache", "index.db")
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("index.db not written: %v", err)
	}
	out = runCLI(t, dir, "index", "status")
	if !strings.Contains(out, "sqlite index: ") || strings.Contains(out, "none") {
		t.Fatalf("status should show sqlite index:\n%s", out)
	}
	// rm + rebuild（S5 验收命令序列）
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	runCLI(t, dir, "index", "rebuild")
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("index.db not rebuilt after rm: %v", err)
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

// work show 的 detail 层保真锁（2026-09-18 审计 P6）：evidence 类型、
// 依赖摘要的 due_on/progress_reported 字段名（曾因缺 yaml tag 渲染成
// dueon/progressvalue），且 deps 不重复渲染。
func TestWorkShowDetailFidelity(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "work", "show", "FEAT-CDC-001")
	for _, want := range []string{"evidence:", "type: git_commit", "type: test_result", "progress:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("work show 缺 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dueon") || strings.Contains(out, "progressvalue") {
		t.Fatalf("work show 字段名烂（缺 yaml tag）:\n%s", out)
	}
	if n := strings.Count(out, "deps:"); n != 1 {
		t.Fatalf("work show deps 应只渲染一次，got %d:\n%s", n, out)
	}
}

// 双入口对照锁（档案 §14/§15）：CLI 与 MCP 同一契约——未注册身份拒绝、
// activity 归属 acting actor。MCP 侧对照见 cmd/mcp TestMCPRejectsUnresolvedActor。
// 新入口必须复用 internal/* 内核并补两侧对照，不许各自实现（一语义一实现）。
func TestCLIRejectsUnregisteredActor(t *testing.T) {
	dir := testfix.Setup(t)
	so, se, code := runCLIRaw([]string{"bug", "report", "--id", "BUG-401", "--title", "x",
		"--system", "datax-backend", "--actor", "ghost", "--dir", dir})
	if code == 0 || !strings.Contains(se, "unknown actor") {
		t.Fatalf("未注册 actor 必须拒：code=%d\nstdout:%s\nstderr:%s", code, so, se)
	}
	if _, se, code := runCLIRaw([]string{"bug", "report", "--id", "BUG-402", "--title", "y",
		"--system", "datax-backend", "--actor", "", "--dir", dir}); code == 0 || !strings.Contains(se, "actor required") {
		t.Fatalf("空 actor 必须拒：code=%d\n%s", code, se)
	}
	runCLI(t, dir, "bug", "report", "--id", "BUG-403", "--title", "z", "--system", "datax-backend", "--actor", "test-agent")
	matches, _ := filepath.Glob(filepath.Join(dir, ".ousheng", "activity", "*", "test-agent.jsonl"))
	if len(matches) == 0 {
		t.Fatal("activity 应归属 acting actor=test-agent")
	}
}

// 分叉处置锁（2026-09-21，档案 §16）：两端各有未合并提交时，sync 必须给人话
// 指引 + 修复命令，且不自动 rebase（冲突需人拍板）、不丢本地数据、不推分叉态。
func TestCLISyncDivergenceGuides(t *testing.T) {
	dir1 := testfix.Setup(t)
	bare := filepath.Join(t.TempDir(), "central.git")
	gitRun(t, dir1, "init", "--bare", bare)
	gitRun(t, dir1, "remote", "add", "origin", bare)
	gitRun(t, dir1, "push", "-q", "-u", "origin", "main")
	dir2 := filepath.Join(t.TempDir(), "clone2")
	gitRun(t, t.TempDir(), "clone", "-q", bare, dir2)
	gitRun(t, dir2, "config", "user.email", "t@t")
	gitRun(t, dir2, "config", "user.name", "t")

	// 两端各自本地提交 → dir1 推上游，dir2 本地未推 = 分叉
	if err := os.WriteFile(filepath.Join(dir1, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir1, "add", "-A")
	gitRun(t, dir1, "commit", "-qm", "from machine A")
	gitRun(t, dir1, "push", "-q")
	if err := os.WriteFile(filepath.Join(dir2, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir2, "add", "-A")
	gitRun(t, dir2, "commit", "-qm", "from machine B")
	dir2Head := strings.TrimSpace(gitRun(t, dir2, "rev-parse", "HEAD"))

	out := runCLI(t, dir2, "sync")
	if !strings.Contains(out, "分叉") || !strings.Contains(out, "pull --rebase") {
		t.Fatalf("分叉应给人话指引 + 修复命令（不刷裸 git 报错）:\n%s", out)
	}
	// 不自动 rebase：本地提交原样保留
	if head := strings.TrimSpace(gitRun(t, dir2, "rev-parse", "HEAD")); head != dir2Head {
		t.Fatalf("sync 不得改动本地提交（自动 rebase 禁止）: %s → %s", dir2Head, head)
	}
	if _, err := os.Stat(filepath.Join(dir2, "b.txt")); err != nil {
		t.Fatal("本地文件不得丢失")
	}
	// 不推分叉态：上游仍停在 A 的提交
	if up := strings.TrimSpace(gitRun(t, bare, "rev-parse", "main")); up == dir2Head {
		t.Fatal("分叉态不得推送")
	}
}

// 超限项曝光锁（2026-09-21，BUG-002 实测）：尺寸门让超限项静默冻结（所有写被拒），
// 只有下次写才暴露——converge 必须把"超限 = 写冻结"报出来（硬门 + 曝光两层）。
func TestCLIConvergeExposesOversizedItem(t *testing.T) {
	dir := testfix.Setup(t)
	// 直接落一个超限 YAML（绕过写路径——正是超限单的既成事实形态）
	big := "schema_version: 2\nid: BUG-900\ntype: bug\ntitle: 超限\nstatus: backlog\nsystem: datax-backend\ndescription: |\n  " +
		strings.Repeat("长", 4200) + "\n"
	path := filepath.Join(dir, ".ousheng", "work", "BUG-900.yaml")
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Size() <= 8192 {
		t.Fatalf("test setup: %d bytes, need > 8192", fi.Size())
	}
	out := runCLI(t, dir, "converge")
	if !strings.Contains(out, "BUG-900 超限") || !strings.Contains(out, "写冻结") {
		t.Fatalf("converge 应曝光超限项（写冻结）:\n%s", out)
	}
}
