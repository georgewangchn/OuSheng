package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cruiseFixture：data 座位——T-001 doing（恢复）、TASK-009 ready 无主（领活）、
// T-002 blocked 依赖 T-003（反应·等）、T-004 blocked 无依赖（反应·僵持）。
func cruiseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "data", "--name", "data 座位")
	mustRun(t, dir, "system", "add", "lake")
	mustRun(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "data", "--role", "dev", "--system", "lake")
	os.WriteFile(filepath.Join(dir, ".ousheng", "roles.yaml"), []byte("schema_version: 1\nroles:\n    - id: dev\n      name: dev\n    - id: tester\n      name: tester\n"), 0o644)
	mustRun(t, dir, "todo", "进行中", "--system", "lake") // T-001
	mustRun(t, dir, "todo", "等他", "--system", "lake")  // T-002
	mustRun(t, dir, "todo", "被等", "--system", "lake")  // T-003
	mustRun(t, dir, "todo", "僵持", "--system", "lake")  // T-004
	return dir
}

func mkCruiseBoard(t *testing.T, dir string) {
	t.Helper()
	// T-001 → doing（assignee data）；T-002 → blocked 依赖 T-003；T-004 → blocked 无依赖；TASK-009 形态：另建 ready 无主。
	assign := func(id string) {
		mustRun(t, dir, "work", "assign", id, "--assignee", "data", "--role", "dev", "--actor", "data")
	}
	assign("T-001")
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-001", "--status", "doing", "--actor", "data")
	assign("T-002")
	mustRun(t, dir, "work", "update", "T-002", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-002", "--status", "doing", "--actor", "data")
	p := filepath.Join(dir, ".ousheng", "work", "T-002.yaml")
	b, _ := os.ReadFile(p)
	// todo 生成的卡 omitempty 无 depends_on 键——显式插入。
	os.WriteFile(p, []byte(strings.Replace(string(b), "status: doing", "status: doing\ndepends_on:\n    - T-003", 1)), 0o644)
	mustRun(t, dir, "work", "update", "T-002", "--status", "blocked", "--actor", "data")
	assign("T-004")
	mustRun(t, dir, "work", "update", "T-004", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-004", "--status", "doing", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-004", "--status", "blocked", "--actor", "data")
	// ready 无主（领活池）。
	todo := filepath.Join(dir, ".ousheng", "work", "TASK-009.yaml")
	os.WriteFile(todo, []byte("schema_version: 2\nid: TASK-009\ntype: task\ntitle: 无主可领\nsystem: lake\ndescription: d\nstatus: ready\nrevision: 1\n"), 0o644)
}

// TestCruisePlan：优先序四节、僵持提示、idle、--allow-new 门、JSON 面。
func TestCruisePlan(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)

	out := mustRun(t, dir, "cruise", "plan", "--actor", "data")
	for _, want := range []string{
		"1·resume 恢复 (1)", "T-001",
		"2·claim 领活 (1)", "TASK-009",
		"3·react 反应 (2)", "T-002", "T-004",
		"僵持——按宪章协商/投票",
		"4·new 自提：关闭",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan missing %q:\n%s", want, out)
		}
	}
	if i, j := strings.Index(out, "1·resume"), strings.Index(out, "2·claim"); i > j || i < 0 {
		t.Fatalf("优先序恢复先于领活:\n%s", out)
	}
	// --allow-new 打开第 4 节。
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data", "--allow-new")
	if !strings.Contains(out, "4·new 自提（--allow-new）") || !strings.Contains(out, "只许 task/bug/test/notice") {
		t.Fatalf("allow-new must open self-propose section:\n%s", out)
	}
	// tester 视角：待测池必须进反应节（复查缺陷 ③ 的锁）。
	mustRun(t, dir, "team", "add", "testbot", "--type", "agent", "--responsible-human", "data", "--role", "tester", "--system", "lake")
	// 场景 A：testing 单在 dev 手里（未移交）——tester 从待测池看到（§26 本义）。
	mustRun(t, dir, "work", "update", "T-003", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-003", "--status", "doing", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-003", "--status", "testing", "--actor", "data")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "testbot")
	if !strings.Contains(out, "待测池（testing 态，dev 未移交）") || !strings.Contains(out, "T-003") {
		t.Fatalf("tester seat must see awaiting_test in react:\n%s", out)
	}

	// idle：新注册的空座位视角 = 无活（造活是病）。
	mustRun(t, dir, "team", "add", "nobody-seat", "--type", "agent", "--responsible-human", "data")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "nobody-seat")
	if !strings.Contains(out, "cruise: idle") || !strings.Contains(out, "造活是病") {
		t.Fatalf("empty seat must be idle:\n%s", out)
	}
	// JSON 面。
	js := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--json")
	if !strings.Contains(js, `"idle": false`) {
		t.Fatalf("json idle flag:\n%s", js)
	}
}

// TestCruiseStatus：三类挂起（拍板/C2/仲裁）+ 夜间摘要投影 + 零噪音。
func TestCruiseStatus(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	// draft 齐 round（等拍板）。
	writeDesignFile(t, dir, "night-plan",
		"status: draft\nowner: data\nsystems: [lake]\nrelated_items: []\n", "# 夜间方案\n")
	rd := filepath.Join(dir, ".ousheng", "designs", "night-plan", "round-1.md")
	os.MkdirAll(filepath.Dir(rd), 0o755)
	os.WriteFile(rd, []byte("## data — 11-01\n方案 A\n## mlake — 11-01\n附议\n"), 0o644)
	// breaking 无 ack（等 C2）。
	p := filepath.Join(dir, ".ousheng", "work", "T-003.yaml")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, []byte(strings.Replace(string(b), "revision: 1", "revision: 2\ncontract:\n    status: proposed\n    breaking: true", 1)), 0o644)

	out := mustRun(t, dir, "cruise", "status", "--hours", "1")
	for _, want := range []string{
		"巡航摘要（近 1h）",
		"挂起等 human 3 项",
		"[拍板] night-plan",
		"[C2]   T-003",
		"[仲裁] T-004",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "⚠ feature") {
		t.Fatalf("no feature created — redline must stay silent:\n%s", out)
	}
	// 零挂起零噪音。
	fresh := t.TempDir()
	mustRun(t, fresh, "init")
	mustRun(t, fresh, "me", "pm", "--name", "PM")
	if out := mustRun(t, fresh, "cruise", "status"); !strings.Contains(out, "挂起等 human：无") {
		t.Fatalf("clean board must be silent:\n%s", out)
	}
}

// TestAdapterCruiseAsset：默认不装；--cruise 写 scripts/cruise-loop.sh（可执行）。
func TestAdapterCruiseAsset(t *testing.T) {
	repo, ws := seatFor(t)
	if _, err := os.Stat(filepath.Join(repo, "scripts", "cruise-loop.sh")); err == nil {
		t.Fatalf("cruise runner must not be installed by default")
	}
	mustRun(t, repo, "adapter", "install", "--workspace", ws, "--cruise")
	p := filepath.Join(repo, "scripts", "cruise-loop.sh")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), "cruise plan") || !strings.Contains(string(b), "轮顶") {
		t.Fatalf("--cruise must write runner with three brakes:\n%v", err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("runner must be executable")
	}
	// doctor 不受影响（可选资产不进检查面）。
	if out := mustRun(t, repo, "doctor"); strings.Contains(out, "FAIL") {
		t.Fatalf("optional asset must not break doctor:\n%s", out)
	}
}

// TestDesignShowCruiseQuorum：show 尾部表决投影（发言者×系统去重——防复本刷票）。
func TestDesignShowCruiseQuorum(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	writeDesignFile(t, dir, "night-plan",
		"status: draft\nowner: data\nsystems: [lake]\nrelated_items: []\n", "# 夜间方案\n")
	rd := filepath.Join(dir, ".ousheng", "designs", "night-plan", "round-1.md")
	os.MkdirAll(filepath.Dir(rd), 0o755)
	os.WriteFile(rd, []byte("## data — 11-01\n方案 A\n"), 0o644)
	out := mustRun(t, dir, "design", "show", "night-plan")
	if !strings.Contains(out, "巡航表决投影：最新轮发言 1 人 data(lake)") {
		t.Fatalf("quorum line missing:\n%s", out)
	}
}

// TestWebCruiseView：/cruise 只读页（httptest + run() 派发器；无表单）。
func TestWebCruiseView(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	srv := httptest.NewServer(newWebMux(dir))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/cruise")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	body := string(b)
	if resp.StatusCode != 200 || !strings.Contains(body, "巡航") || !strings.Contains(body, "下一活") {
		t.Fatalf("cruise view broken: %d\n%s", resp.StatusCode, body[:300])
	}
	if strings.Contains(body, "<form") {
		t.Fatalf("cruise view must be read-only (no forms)")
	}
}
