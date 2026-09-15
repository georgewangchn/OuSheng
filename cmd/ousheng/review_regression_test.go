package main

// 回归测试：code review 发现的缺陷逐项锁定。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ousheng/internal/converge"
	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	sqliteidx "ousheng/internal/index/sqlite"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/testfix"
)

// Fix 2: GetActor 路径穿越。
func TestReviewGetActorPathTraversal(t *testing.T) {
	dir := testfix.Setup(t)
	repo := gityaml.Open(dir)
	for _, evil := range []string{"../project", "a/b", "..", "zhangsan/../zhangsan"} {
		if _, err := repo.GetActor(evil); err == nil {
			t.Fatalf("path traversal %q must be rejected", evil)
		}
	}
	// 合法 id 仍可读
	if _, err := repo.GetActor("zhangsan"); err != nil {
		t.Fatal(err)
	}
}

// Fix 3: BySystem("")/ByVersion("") 空 = unassigned，memory 与 sqlite 一致（S5）。
func TestReviewEmptyKeyEquivalence(t *testing.T) {
	dir := testfix.Setup(t)
	snap, err := index.Load(gityaml.Open(dir))
	if err != nil {
		t.Fatal(err)
	}
	// 注入一个无 system/version 的项
	snap.WorkItems = append(snap.WorkItems, model.WorkItem{
		SchemaVersion: 2, ID: "X-NO-SYS", Type: model.TypeTask,
		Title: "无系统归属", Status: model.StatusBacklog, Revision: 1,
	})
	mem := memory.New()
	if err := mem.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	sidx, err := sqliteidx.Open(dir + "/.ousheng/cache/review.db")
	if err != nil {
		t.Fatal(err)
	}
	defer sidx.Close()
	if err := sidx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	mw, _ := mem.BySystem("")
	sw, serr := sidx.BySystem("")
	if serr != nil || len(mw) != len(sw) || len(mw) != 1 || mw[0].ID != "X-NO-SYS" || sw[0].ID != "X-NO-SYS" {
		t.Fatalf("BySystem(\"\") diverges: mem=%v sq=%v err=%v", idsOf(mw), idsOf(sw), serr)
	}
	mv, _ := mem.ByVersion("")
	sv, serr := sidx.ByVersion("")
	if serr != nil || len(mv) != len(sv) || len(mv) != 1 || mv[0].ID != "X-NO-SYS" || sv[0].ID != "X-NO-SYS" {
		t.Fatalf("ByVersion(\"\") diverges: mem=%v sq=%v err=%v", idsOf(mv), idsOf(sv), serr)
	}
}

func idsOf(ws []model.WorkItem) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}

// Fix 4: 完全重复的 assignment 行在 canonical 解码层拒绝。
func TestReviewDuplicateAssignmentRejected(t *testing.T) {
	raw := `schema_version: 1
assignments:
  - actor: zhangsan
    role: backend
    system: datax-backend
    responsibility: accountable
    active: true
  - actor: zhangsan
    role: backend
    system: datax-backend
    responsibility: accountable
    active: false
`
	if _, err := model.DecodeAssignmentsFile([]byte(raw)); err == nil {
		t.Fatal("duplicate assignment row must be rejected")
	}
}

// Fix 5: 已关闭项之间的历史依赖环不再阻塞收敛。
func TestReviewClosedCycleNotBlocking(t *testing.T) {
	a := model.WorkItem{SchemaVersion: 2, ID: "A-1", Type: model.TypeTask, Title: "a", Status: model.StatusDone, Revision: 1, DependsOn: []string{"B-1"}}
	b := model.WorkItem{SchemaVersion: 2, ID: "B-1", Type: model.TypeTask, Title: "b", Status: model.StatusCancelled, Revision: 1, DependsOn: []string{"A-1"}}
	snap := index.Snapshot{WorkItems: []model.WorkItem{a, b}}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	r, err := converge.Check(idx, converge.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != converge.Converged {
		t.Fatalf("closed-item cycle must not block, got %s (%v %v)", r.Status, r.Blockers, r.Cycle)
	}
	// open 项成环仍然阻塞（两端都 open 才成环）
	a.Status = model.StatusDoing
	b.Status = model.StatusDoing
	snap.WorkItems = []model.WorkItem{a, b}
	idx2 := memory.New()
	if err := idx2.Rebuild(snap); err != nil {
		t.Fatal(err)
	}
	r2, err := converge.Check(idx2, converge.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != converge.Blocked || len(r2.Cycle) == 0 {
		t.Fatalf("open-item cycle must block, got %s", r2.Status)
	}
}

// Fix 7: CLI evidence add 走 workspace 单一路径后，activity 记录格式不变。
func TestReviewEvidenceActivityFormat(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "evidence", "add", "K8S-003",
		"--type", "manual_check", "--locator", "review-check", "--actor", "wangwu")
	if !strings.Contains(out, "evidence added to K8S-003") {
		t.Fatalf("evidence add wrong: %q", out)
	}
	acts := runCLI(t, dir, "activity", "list")
	if !strings.Contains(acts, "evidence_added") || !strings.Contains(acts, "manual_check review-check") {
		t.Fatalf("activity record missing or malformed:\n%s", acts)
	}
}

// Fix 1: 位置参数夹在 flag 中间不再丢失后半 flag（三明治解析）。
// 注：work list 不收位置参数（多余即报错），过滤值必须带 --system。
func TestReviewSandwichedFlags(t *testing.T) {
	dir := testfix.Setup(t)
	// --open 在前、--system 的值夹中间、--type 在后：两个过滤器都要生效
	out := runCLI(t, dir, "work", "list", "--open", "--system", "datax-backend", "--type", "bug")
	// BUG-017 是 datax-backend 的 open bug，必须出现；FEAT-CDC-001 是 feature 必须被过滤
	if !strings.Contains(out, "BUG-017") {
		t.Fatalf("sandwiched flags lost filter — BUG-017 missing:\n%s", out)
	}
	if strings.Contains(out, "FEAT-CDC-001") {
		t.Fatalf("--type bug filter lost:\n%s", out)
	}
}

// 场景测试实锤：静默吞位置参数会让用户基于错误数据决策。
// 1) init <dir> 位置参数必须生效；2) list/view 类命令多余位置参数必须报错。
func TestReviewRejectsStrayPositionals(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	full := append([]string{}, "init", sub, "--project-id", "p1", "--project-name", "P")
	var stdout, stderr bytes.Buffer
	if code := run(full, &stdout, &stderr); code != 0 {
		t.Fatalf("init with positional dir failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(sub, ".ousheng", "project.yaml")); err != nil {
		t.Fatalf("workspace must be created in positional dir: %v", err)
	}

	dir := testfix.Setup(t)
	for _, bad := range [][]string{
		{"work", "list", "datax-backend", "--dir", dir}, // 忘写 --system
		{"view", "kanban", "extra", "--dir", dir},       // 多余参数
		{"activity", "list", "junk", "--dir", dir},
		{"actor", "list", "junk", "--dir", dir},
		{"converge", "junk", "--dir", dir},
		{"work", "show", "BUG-017", "extra", "--dir", dir}, // 多写一个 id
	} {
		var so, se bytes.Buffer
		if code := run(bad, &so, &se); code == 0 {
			t.Fatalf("cli %v must reject stray positional, got success:\n%s", bad[:len(bad)-2], so.String())
		}
	}
}

// 方案 §37：view actor 子命令必须存在；§21：看板 Role/Human 显示名。
func TestReviewViewActorAndDisplayNames(t *testing.T) {
	dir := testfix.Setup(t)
	out := runCLI(t, dir, "view", "actor", "zhangsan")
	if !strings.Contains(out, "zhangsan") || !strings.Contains(out, "张三") {
		t.Fatalf("view actor output missing actor info:\n%s", out)
	}
	kb := runCLI(t, dir, "view", "kanban")
	if !strings.Contains(kb, "Role      backend (Java后端开发)") {
		t.Fatalf("kanban card must show role display name (§21):\n%s", kb)
	}
	if !strings.Contains(kb, "Human     zhangsan (张三)") {
		t.Fatalf("kanban card must show human display name (§21):\n%s", kb)
	}
}
