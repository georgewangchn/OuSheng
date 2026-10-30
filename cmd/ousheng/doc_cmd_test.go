package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeDoc 在工作区里落地一篇文档（git add 前调用）。
func writeDoc(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitCommitAt 以指定时间提交全部改动——陈旧判定需要确定的先后关系，
// 用真实时钟会让"文档早于交付"这一条件在同一秒内失去确定性。
func gitCommitAt(t *testing.T, dir, when, msg string) {
	t.Helper()
	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+when,
		"GIT_COMMITTER_DATE="+when,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

// docFixture 搭一个带 git 历史的工作区：
//
//	docs/design/csv-plan.md       → design.doc 强认领（decision，远早于交付 → 陈旧）
//	docs/design/notes.md          → 无认领（孤儿）
//	docs/design/中文方案.md        → 无认领（孤儿，且锁中文路径不被 git 转义）
//	docs/acceptance/T-001-*.md    → work item document 证据弱认领（evidence）
//	docs/acceptance/T-003-*.md    → 无认领（孤儿；用来验证补一条 locator 就脱罪）
//	design/databuddy/*.md         → 零认领目录树（census 区域）
//	README.md                     → 结构性豁免
func docFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "cms-api")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent",
		"--responsible-human", "george", "--role", "dev", "--system", "cms-api")
	mustRun(t, dir, "todo", "结果导出", "--system", "cms-api") // T-001
	mustRun(t, dir, "todo", "接口梳理", "--system", "cms-api") // T-002

	writeDoc(t, dir, "docs/design/csv-plan.md", "# CSV 方案定稿\n")
	writeDoc(t, dir, "docs/design/notes.md", "# 随手记\n")
	writeDoc(t, dir, "docs/design/中文方案.md", "# 中文方案\n")
	writeDoc(t, dir, "docs/acceptance/T-001-验收报告.md", "# 验收\n")
	writeDoc(t, dir, "docs/acceptance/T-003-待补证据.md", "# 待补\n")
	for _, n := range []string{"1", "2", "3"} {
		writeDoc(t, dir, "design/databuddy/"+n+".md", "x\n")
	}
	writeDoc(t, dir, "README.md", "# readme\n")

	writeDesignFile(t, dir, "csv-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\nrelated_items: [T-001]\ndoc: docs/design/csv-plan.md\n",
		"# CSV 方案\n")
	// 指向不存在文档的 topic：拍板必须 fail-closed。
	writeDesignFile(t, dir, "ghost-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\ndoc: docs/design/missing.md\n",
		"# 幽灵方案\n")
	// 一条 document 证据把 docs/acceptance 这棵树纳入监控（认领即扫描面）。
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "document",
		"--locator", "docs/acceptance/T-001-验收报告.md", "--actor", "george")

	gitRun(t, dir, "add", "-A")
	// 文档定稿远早于后续交付 → decision 角色必须判陈旧。
	gitCommitAt(t, dir, "2020-01-01T10:00:00+08:00", "docs")
	return dir
}

// 拍板门：doc 指向不存在的文件必须拒（fail-closed）。
// 否则"方案已拍板"与"没人知道定稿在哪"可以同时成立。
func TestDesignDecideRequiresExistingDoc(t *testing.T) {
	dir := docFixture(t)
	if _, se, code := runIn(t, dir, "design", "decide", "ghost-plan", "--actor", "george"); code == 0 ||
		!strings.Contains(se, "does not exist") {
		t.Fatalf("decide on missing doc must fail closed, got code=%d err=%s", code, se)
	}
	// 拒绝后 topic 必须仍是 draft（不能留下半拍板状态）。
	raw, err := os.ReadFile(filepath.Join(dir, ".ousheng", "designs", "ghost-plan", "design.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "status: draft") {
		t.Fatalf("rejected decide must leave status draft:\n%s", raw)
	}
	// 文档真实存在 → 放行。
	if out := mustRun(t, dir, "design", "decide", "csv-plan", "--actor", "george"); !strings.Contains(out, "decided") {
		t.Fatalf("decide with existing doc must pass: %s", out)
	}
}

// doc map：户口本——受监控文档的认领态、角色归属、根推导。
func TestDocMap(t *testing.T) {
	dir := docFixture(t)
	mustRun(t, dir, "design", "decide", "csv-plan", "--actor", "george")

	out := mustRun(t, dir, "doc", "map")
	for _, want := range []string{
		"docs/design/csv-plan.md", // 强认领 → decision
		"decision", "csv-plan",    //
		"docs/acceptance/T-001-验收报告.md", // 弱认领 → evidence
		"evidence", "T-001",             //
		"docs/design/notes.md", // 孤儿
		"ORPHAN",               //
		"docs/design/中文方案.md",  // 中文路径必须原样出现（quotepath 锁）
		"docs/design",          // 根由认领自动推导
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doc map must contain %q:\n%s", want, out)
		}
	}
	// 结构性文件与绳自己的地盘不进户口本。
	for _, bad := range []string{"README.md", ".ousheng/designs"} {
		if strings.Contains(out, bad) {
			t.Fatalf("doc map must not track %q:\n%s", bad, out)
		}
	}
	// 零认领的 databuddy 树整树免打扰（D3 边界）。
	if strings.Contains(out, "databuddy") {
		t.Fatalf("zero-claim tree must stay out of doc map:\n%s", out)
	}
}

// doc check：孤儿 + 陈旧。
func TestDocCheck(t *testing.T) {
	dir := docFixture(t)
	mustRun(t, dir, "design", "decide", "csv-plan", "--actor", "george")
	// T-001 走完 doing → done（证据已在 fixture 里留下，时间远晚于定稿提交）。
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "doing", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "done", "--actor", "george")

	out := mustRun(t, dir, "doc", "check")
	if !strings.Contains(out, "doc orphan: docs/design/notes.md") {
		t.Fatalf("notes.md must be reported as orphan:\n%s", out)
	}
	if !strings.Contains(out, "doc orphan: docs/design/中文方案.md") {
		t.Fatalf("Chinese-named orphan must be reported:\n%s", out)
	}
	// 定稿提交于 2020，交付在当下 → decision 角色陈旧。
	if !strings.Contains(out, "doc stale: docs/design/csv-plan.md") ||
		!strings.Contains(out, "T-001") {
		t.Fatalf("doc committed long before its owner's delivery must be stale:\n%s", out)
	}
	// 验收报告是完成时快照，永不判陈旧（D2）。
	if strings.Contains(out, "T-001-验收报告.md (evidence)") {
		t.Fatalf("evidence role must never be judged stale:\n%s", out)
	}
}

// 弱认领给文件脱罪：同一篇验收报告，没有证据 locator 时是孤儿，补一条就不是。
func TestDocCheckEvidenceClaimClearsOrphan(t *testing.T) {
	dir := docFixture(t)
	before := mustRun(t, dir, "doc", "check")
	if !strings.Contains(before, "doc orphan: docs/acceptance/T-003-待补证据.md") {
		t.Fatalf("unclaimed acceptance doc must be an orphan:\n%s", before)
	}
	mustRun(t, dir, "evidence", "add", "T-002", "--type", "document",
		"--locator", "docs/acceptance/T-003-待补证据.md", "--actor", "george")
	after := mustRun(t, dir, "doc", "check")
	if strings.Contains(after, "T-003-待补证据.md") {
		t.Fatalf("evidence locator must clear the orphan:\n%s", after)
	}
}

// doc census：未纳管区域——零认领（或部分无主）且不在任何受监控根下的目录树。
func TestDocCensus(t *testing.T) {
	dir := docFixture(t)
	out := mustRun(t, dir, "doc", "census")
	if !strings.Contains(out, "design/databuddy") || !strings.Contains(out, "3") {
		t.Fatalf("census must report the zero-claim tree:\n%s", out)
	}
	// 已纳管的 docs/design 不报（那里面是 orphan 的活，不是 census 的活）。
	if strings.Contains(out, "docs/design") {
		t.Fatalf("monitored tree must not appear in census:\n%s", out)
	}
	// 阈值以下不报。
	if got := mustRun(t, dir, "doc", "census", "--min", "10"); !strings.Contains(got, "clean") {
		t.Fatalf("below threshold must stay silent:\n%s", got)
	}
}

// 无 git（或 git 里还没有任何跟踪的 .md）：认领层退化为"只跟踪认领指向的
// 文件"，不响亮失败——刚 init 的工作区跑 doc map 不该是一片空白。
func TestDocLayerWithoutGit(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	writeDoc(t, dir, "docs/design/a.md", "# a\n")
	writeDesignFile(t, dir, "t1", "status: draft\nowner: george\ndoc: docs/design/a.md\n", "# t1\n")

	// init 自带 git 仓但尚无提交 → 空扫描面，退化到认领路径核对。
	if out := mustRun(t, dir, "doc", "map"); !strings.Contains(out, "docs/design/a.md") {
		t.Fatalf("design.doc claim must work on an empty repo:\n%s", out)
	}
	// 连 .git 都去掉 → 同一降级路径，census 明确说明原因。
	os.RemoveAll(filepath.Join(dir, ".git"))
	if out := mustRun(t, dir, "doc", "map"); !strings.Contains(out, "docs/design/a.md") {
		t.Fatalf("design.doc claim must work without git:\n%s", out)
	}
	if got := mustRun(t, dir, "doc", "census"); !strings.Contains(got, "not a git repository") {
		t.Fatalf("census without git must say so:\n%s", got)
	}
}

// JSON 出口：三条命令都要能给程序消费（MCP 三入口复用同一层）。
func TestDocJSON(t *testing.T) {
	dir := docFixture(t)
	for _, args := range [][]string{
		{"doc", "map", "--json"},
		{"doc", "check", "--json"},
		{"doc", "census", "--json"},
	} {
		out := mustRun(t, dir, args...)
		if !strings.HasPrefix(strings.TrimSpace(out), "[") && !strings.HasPrefix(strings.TrimSpace(out), "{") {
			t.Fatalf("%v must emit JSON:\n%s", args, out)
		}
	}
}

// doc bloat 三信号（TASK-039）：真实 git 历史触发 growth、契约仓联动触发 size。
// 契约正本走 system.contract_doc + repos.yaml 映射（档案 §30 第四类认领）。
func TestDocCheckBloat(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "cms-api")

	// ①+②：40KB 方案，changelog 节占 ~60%。
	big := "# CSV 方案\n" + strings.Repeat("a", 12*1024) + "\n## Changelog\n" + strings.Repeat("b", 26*1024) + "\n"
	writeDoc(t, dir, "docs/design/csv-plan.md", big)
	// ③：8 天前 8KB → 现在 33KB（+25KB/7d）。
	writeDoc(t, dir, "docs/design/grow.md", strings.Repeat("g", 8*1024))
	// 健康：小、无 changelog、增长 <1KB。
	writeDoc(t, dir, "docs/design/calm.md", strings.Repeat("c", 6*1024))
	writeDesignFile(t, dir, "csv-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\ndoc: docs/design/csv-plan.md\n", "# x\n")
	writeDesignFile(t, dir, "grow-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\ndoc: docs/design/grow.md\n", "# x\n")
	writeDesignFile(t, dir, "calm-plan",
		"status: draft\nowner: george\nsystems: [cms-api]\ndoc: docs/design/calm.md\n", "# x\n")
	gitRun(t, dir, "add", "-A")
	gitCommitAt(t, dir, "2026-10-20T10:00:00+08:00", "v1 small")
	writeDoc(t, dir, "docs/design/grow.md", strings.Repeat("g", 33*1024))
	writeDoc(t, dir, "docs/design/calm.md", strings.Repeat("c", 7*1024))
	gitRun(t, dir, "add", "-A")
	gitCommitAt(t, dir, "2026-10-28T10:00:00+08:00", "v2 grown")

	// 契约正本：系统映射仓 + contract_doc 声明。
	sysRepo := t.TempDir()
	gitRun(t, sysRepo, "init", "-q", "-b", "main")
	gitRun(t, sysRepo, "config", "user.name", "t")
	gitRun(t, sysRepo, "config", "user.email", "t@t.local")
	os.WriteFile(filepath.Join(sysRepo, "INFO-CARD.md"), []byte(strings.Repeat("i", 96*1024)), 0o644)
	gitRun(t, sysRepo, "add", "-A")
	gitCommitAt(t, sysRepo, "2026-10-20T10:00:00+08:00", "contract v1")
	mustRun(t, dir, "repo", "set", "cms-api", sysRepo)
	setContractDoc(t, dir, "cms-api", "INFO-CARD.md")

	out := mustRun(t, dir, "doc", "check")
	for _, want := range []string{
		"doc bloat: docs/design/csv-plan.md (decision, csv-plan) [size]",
		"doc bloat: docs/design/csv-plan.md (decision, csv-plan) [changelog]",
		"doc bloat: docs/design/grow.md (decision, grow-plan) [growth]",
		"doc bloat: cms-api:INFO-CARD.md (contract, cms-api) [size]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "calm") {
		t.Fatalf("healthy doc must stay silent:\n%s", out)
	}
	// JSON 面同给 bloat。
	js := mustRun(t, dir, "doc", "check", "--json")
	if !strings.Contains(js, `"Kind": "size"`) {
		t.Fatalf("json must carry bloat kind:\n%s", js)
	}
	// 阈值可调（卡边界：告警不拒绝；0=关）。
	off := mustRun(t, dir, "doc", "check", "--bloat-bytes", "0", "--bloat-changelog", "0", "--bloat-growth", "0")
	if strings.Contains(off, "doc bloat") {
		t.Fatalf("0 must disable all bloat signals:\n%s", off)
	}
}

// setContractDoc 手改 systems.yaml 声明契约正本（登记面轻字段，暂无 CLI 面）。
func setContractDoc(t *testing.T, dir, sys, rel string) {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "systems.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	inserted := false
	for _, l := range lines {
		out = append(out, l)
		if !inserted && strings.TrimSpace(l) == "- id: "+sys {
			out = append(out, "      contract_doc: "+rel)
			inserted = true
		}
	}
	if !inserted {
		t.Fatalf("system %s not found in systems.yaml", sys)
	}
	if err := os.WriteFile(p, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}
