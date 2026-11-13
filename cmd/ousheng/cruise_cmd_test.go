package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// cruiseFixture：data 座位——T-001 doing（恢复）、TASK-009 ready 无主（领活）、
// T-002 blocked 依赖 T-003（反应·等）、T-004 blocked 无依赖（反应·僵持）。
func cruiseFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "init")
	// 注册序 = 协议正序：human pm 先上绳（F4 门）→ data 以 agent 型注册 → me 切
	// data。原 fixture `me data`（human）在前、team add 幂等 skip 不改型——data
	// 实为 human 型座位（§36 Y6 实锤）。
	mustRun(t, dir, "me", "pm", "--name", "PM")
	mustRun(t, dir, "system", "add", "lake")
	mustRun(t, dir, "team", "add", "data", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "lake")
	mustRun(t, dir, "me", "data", "--name", "data 座位")
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
	// --allow-new 打开第 4 节 + JSON 面（复查修正：断言前置——后续段落会突变
	// 看板状态，时序耦合防后人踩雷）。
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data", "--allow-new")
	if !strings.Contains(out, "4·new 自提（--allow-new）") || !strings.Contains(out, "只许 task/bug/test/notice") {
		t.Fatalf("allow-new must open self-propose section:\n%s", out)
	}
	js := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--json")
	if !strings.Contains(js, `"idle": false`) {
		t.Fatalf("json idle flag:\n%s", js)
	}
	// tester 视角：待测池必须进反应节（复查缺陷 ③ 的锁）。
	mustRun(t, dir, "team", "add", "testbot", "--type", "agent", "--responsible-human", "pm", "--role", "tester", "--system", "lake")
	// 场景 A：testing 单在 dev 手里（未移交）——tester 从待测池看到（§26 本义）。
	mustRun(t, dir, "work", "update", "T-003", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-003", "--status", "doing", "--actor", "data")
	mustRun(t, dir, "work", "update", "T-003", "--status", "testing", "--actor", "data")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "testbot")
	if !strings.Contains(out, "待测池（testing 态，dev 未移交）") || !strings.Contains(out, "T-003") {
		t.Fatalf("tester seat must see awaiting_test in react:\n%s", out)
	}

	// 全局反应：dangling 依赖（converge BLOCKER 的可动作面）必须进任何座位的 plan。
	p5 := filepath.Join(dir, ".ousheng", "work", "T-004.yaml")
	b5, _ := os.ReadFile(p5)
	os.WriteFile(p5, []byte(strings.Replace(string(b5), "status: blocked", "status: blocked\ndepends_on:\n    - GHOST-999", 1)), 0o644)
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if !strings.Contains(out, "依赖悬空 GHOST-999") || !strings.Contains(out, "converge BLOCKER") {
		t.Fatalf("dangling dep must surface in react (夜里无人收敛):\n%s", out)
	}
	// 协商信号：他人在我的单上留 evidence → 我的 plan 必须点名（协商闭环）。
	mustRun(t, dir, "team", "add", "mlake-peer", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "lake")
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--note", "与你的实现矛盾，建议协商", "--actor", "mlake-peer")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if !strings.Contains(out, "mlake-peer 在此单留言") {
		t.Fatalf("peer evidence on my item must surface (negotiation loop):\n%s", out)
	}

	// 全局阻塞存在时空座位也不 idle（夜里任何人都能修 converge BLOCKER——
	// 复查 B 语义：巡航不因"不是我的活"而睡）。
	mustRun(t, dir, "team", "add", "nobody-seat", "--type", "agent", "--responsible-human", "pm")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "nobody-seat")
	if !strings.Contains(out, "依赖悬空 GHOST-999") {
		t.Fatalf("global blocker must keep seats awake:\n%s", out)
	}
	// idle：干净板上的空座位 = 无活（造活是病）。
	fresh := t.TempDir()
	mustRun(t, fresh, "init")
	mustRun(t, fresh, "me", "pm", "--name", "PM")
	mustRun(t, fresh, "team", "add", "empty-seat", "--type", "agent", "--responsible-human", "pm")
	if out := mustRun(t, fresh, "cruise", "plan", "--actor", "empty-seat"); !strings.Contains(out, "cruise: idle") {
		t.Fatalf("clean empty seat must be idle:\n%s", out)
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
		"宪章：未立", "巡航未启用",
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
	// 造挂起项：draft design 齐轮次（驾驶舱应出拍板表单）。
	writeDesignFile(t, dir, "night-plan",
		"status: draft\nowner: data\nsystems: [lake]\nrelated_items: []\n", "# 夜间方案\n")
	rd := filepath.Join(dir, ".ousheng", "designs", "night-plan", "round-1.md")
	os.MkdirAll(filepath.Dir(rd), 0o755)
	os.WriteFile(rd, []byte("## data — 11-09\n方案 A\n"), 0o644)
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
	// P4 驾驶舱：观察 + 发单 + 拍板表单（写路径复用既有 POST 端点）。
	for _, want := range []string{"快速发单", "/work/create", "挂起待你处理", "值守（present）", "/cruise/pm-presence", "我离开（away）"} {
		if !strings.Contains(body, want) {
			t.Fatalf("cockpit missing %q:\n%s", want, body[:300])
		}
	}
}

// TestHandoverSessionGenerations（P0 代际制）：save --session → plan attach 行；
// save 换代 → attach 消失；attach 命令补记。
func TestHandoverSessionGenerations(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	saveHandover(t, dir, "lake", "data", "# gen1\n导航\n")
	// 无 session：无 attach 行。
	out := mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if strings.Contains(out, "attach:") {
		t.Fatalf("no session recorded — no attach line:\n%s", out)
	}
	// 驱动器补记 session → plan 出 attach 行。
	mustRun(t, dir, "handover", "attach", "--system", "lake", "--actor", "data", "--session", "ses_gen1")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if !strings.Contains(out, "attach: ses_gen1") || !strings.Contains(out, "opencode run -s ses_gen1") {
		t.Fatalf("attach line missing after driver attach:\n%s", out)
	}
	// agent save 不带 session（换代）→ attach 消失（新代起点）。
	saveHandover(t, dir, "lake", "data", "# gen2\n换代后的导航\n")
	out = mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if strings.Contains(out, "attach: ses_gen1") {
		t.Fatalf("generation rollover must clear attach:\n%s", out)
	}
}

// TestCruisePlanWait（P2 活到即醒）：有活立即返回（耗时 << wait）；空闲超时返回 idle。
func TestCruisePlanWait(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	start := nowUnix()
	out := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--wait", "30")
	if !strings.Contains(out, "1·resume") || nowUnix()-start > 10 {
		t.Fatalf("work present must return fast (wait must not block): took %ds\n%s", nowUnix()-start, out)
	}
	// 空座位：--wait 2 秒超时后返回 idle（不悬挂）。
	fresh := t.TempDir()
	mustRun(t, fresh, "init")
	mustRun(t, fresh, "me", "pm", "--name", "PM")
	mustRun(t, fresh, "system", "add", "lake")
	mustRun(t, fresh, "team", "add", "s1", "--type", "agent", "--responsible-human", "pm", "--system", "lake")
	start = nowUnix()
	out = mustRun(t, fresh, "cruise", "plan", "--actor", "s1", "--wait", "2")
	if !strings.Contains(out, "cruise: idle") || nowUnix()-start > 15 {
		t.Fatalf("idle seat must time out to idle, took %ds:\n%s", nowUnix()-start, out)
	}
}

func nowUnix() int64 { return time.Now().Unix() }

// TestCruiseStatusStall（P3 空转曝光）：doing 无 evidence → ⚠；有新证据不报。
func TestCruiseStatusStall(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	out := mustRun(t, dir, "cruise", "status", "--hours", "1", "--stall-hours", "12")
	if !strings.Contains(out, "空转嫌疑") || !strings.Contains(out, "T-001") || !strings.Contains(out, "从未有证据") {
		t.Fatalf("stalled doing item must surface:\n%s", out)
	}
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--note", "推进", "--actor", "data")
	out = mustRun(t, dir, "cruise", "status", "--hours", "1", "--stall-hours", "12")
	if strings.Contains(out, "T-001（从未") {
		t.Fatalf("fresh evidence must clear stall:\n%s", out)
	}
}

// TestCruiseReport（P1 + §36 强化）：简报结构 + UTF-8 完整（rune 截断）+
// BUG-123 进清单（原 BUG-0 前缀启发式漏 3 位 bug）+ 座位行序确定 + JSON 面。
func TestCruiseReport(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--note", "推进", "--actor", "data")
	writeDesignFile(t, dir, "rep-plan", "status: draft\nowner: data\nsystems: [lake]\n", "# x\n")
	rd := filepath.Join(dir, ".ousheng", "designs", "rep-plan", "round-1.md")
	os.MkdirAll(filepath.Dir(rd), 0o755)
	os.WriteFile(rd, []byte("## data — 11-10\n方案\n"), 0o644)
	// Y1：长中文标题——40 字节切分会产出非法 UTF-8（mojibake 实锤），rune 截断必须治。
	mustRun(t, dir, "work", "create", "--id", "BUG-012", "--type", "bug",
		"--title", strings.Repeat("汉字截断验证", 12), "--system", "lake",
		"--detected-by", "data", "--description", "d", "--actor", "data")
	// Q(pm) 攒批计数（§37 复盘 #6）：卡面 Q(pm) evidence 是 pm 真回复通道。
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x",
		"--note", "【Q(pm) 攒批】①测试问题一②测试问题二", "--actor", "data")
	out := mustRun(t, dir, "cruise", "report", "--hours", "24")
	if !strings.Contains(out, "Q(pm) 攒批") || !strings.Contains(out, "T-001") {
		t.Fatalf("report 应显 Q(pm) 积压:\n%s", out)
	}
	js := mustRun(t, dir, "cruise", "report", "--hours", "24", "--json")
	if !strings.Contains(js, `"q_pm_backlog": 1`) {
		t.Fatalf("report json 应含 q_pm_backlog:\n%s", js)
	}
	// Y2：BUG-123（3 位编号）必须进清单。
	mustRun(t, dir, "bug", "report", "--id", "BUG-123", "--title", "三位编号", "--system", "lake", "--detected-by", "data")
	mustRun(t, dir, "team", "add", "testbot", "--type", "agent", "--responsible-human", "pm", "--role", "tester", "--system", "lake")
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--note", "tester 也有活动", "--actor", "testbot")
	out = mustRun(t, dir, "cruise", "report", "--hours", "24")
	for _, want := range []string{"巡航简报", "每座位战果", "data", "待你处理", "BUG-012", "BUG-123"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
	if !utf8.ValidString(out) {
		t.Fatalf("report 必须是合法 UTF-8（rune 截断）:\n%s", out)
	}
	// Y4：座位行序确定（按 actor 排序）。
	iData, iTest := strings.Index(out, "| data |"), strings.Index(out, "| testbot |")
	if iData < 0 || iTest < 0 || iData > iTest {
		t.Fatalf("座位行序必须按 actor 排序:\n%s", out)
	}
	// JSON 面（cron/web 消费）。
	js = mustRun(t, dir, "cruise", "report", "--hours", "24", "--json")
	for _, want := range []string{`"seats"`, `"items"`, `"waiting"`} {
		if !strings.Contains(js, want) {
			t.Fatalf("report json missing %q:\n%s", want, js)
		}
	}
}

// TestCruiseReportRoundDigest（§37 F2）：座位轮摘要——activity 聚簇成轮，纯记账轮
// （全 evidence_added 的零 delta 维持轮）必须曝光；JSON 面同步。
func TestCruiseReportRoundDigest(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir) // T-001 doing@data
	// 造一轮 2h 前的「纯记账轮」：只 evidence_added（隔 >15min → 独立轮）
	old := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	mon := time.Now().Add(-2 * time.Hour).Format("2006-01")
	p := filepath.Join(dir, ".ousheng", "activity", mon, "data.jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, item := range []string{"T-001", "T-002"} {
		line := fmt.Sprintf(`{"ts":%q,"actor":"data","action":"evidence_added","work_item":%q,"detail":"维持 #%d"}`+"\n", old, item, i)
		if err := appendFile(p, line); err != nil {
			t.Fatal(err)
		}
	}
	// 当前轮：真活（状态变迁 + evidence）
	mustRun(t, dir, "work", "update", "T-001", "--status", "testing", "--actor", "data")
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x", "--note", "n", "--actor", "data")

	// 混合时区偏移（Z 与 +08:00）同瞬双格式：时序排序应并进同一纯记账轮；
	// 坏 TS 行跳过不造伪轮；-90m 变迁轮 / -30m 纯记账轮独立计数。
	zTS := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	midTS := time.Now().Add(-90 * time.Minute).Format(time.RFC3339)
	later := time.Now().Add(-30 * time.Minute).Format(time.RFC3339)
	for _, line := range []string{
		`{"ts":"NOT-A-TIME","actor":"data","action":"evidence_added","work_item":"T-404","detail":"坏 TS 必须跳过不造伪轮"}` + "\n",
		fmt.Sprintf(`{"ts":%q,"actor":"data","action":"evidence_added","work_item":"T-777","detail":"utc 同瞬轮"}`+"\n", zTS),
		fmt.Sprintf(`{"ts":%q,"actor":"data","action":"status_changed","work_item":"T-778","detail":"变迁轮"}`+"\n", midTS),
		fmt.Sprintf(`{"ts":%q,"actor":"data","action":"evidence_added","work_item":"T-779","detail":"末轮纯记账"}`+"\n", later),
	} {
		if err := appendFile(p, line); err != nil {
			t.Fatal(err)
		}
	}

	out := mustRun(t, dir, "cruise", "report", "--hours", "24")
	for _, want := range []string{"座位轮摘要", "纯记账轮", "T-002"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
	// data 三轮：-2h 同瞬双格式合一纯记账轮 / -90m 变迁轮 / -30m 纯记账轮；坏 TS 不计
	if !strings.Contains(out, "| data | 4 | 2 |") {
		t.Fatalf("data 应为 3 轮/2 纯记账轮（坏 TS 跳过、同瞬双偏移合轮）:\n%s", out)
	}
	if strings.Contains(out, "T-404") {
		t.Fatalf("坏 TS 行不得进触单集:\n%s", out)
	}
	js := mustRun(t, dir, "cruise", "report", "--hours", "24", "--json")
	for _, want := range []string{`"rounds": 4`, `"pure_rounds": 2`} {
		if !strings.Contains(js, want) {
			t.Fatalf("report json missing %q:\n%s", want, js)
		}
	}
}

func appendFile(p, s string) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

// TestWebCruiseHandoverSection（§37 F3）：驾驶舱座位交接快照——pm 一屏读各座位
// 最后的人话（观察面可以富，注入面必须瘦）。
func TestWebCruiseHandoverSection(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	hd := filepath.Join(dir, ".ousheng", "handover", "lake")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nsystem: lake\nactor: data\nsaved_at: \"2026-11-12T09:00:00+08:00\"\n---\n# data 末轮人话摘要\nT-001 全绿交付，候 pm 验收。\n"
	if err := os.WriteFile(filepath.Join(hd, "data.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := webGet(t, dir, "/cruise")
	for _, want := range []string{"座位交接快照", "data", "人话摘要", "候 pm 验收"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cruise page missing %q:\n%s", want, out)
		}
	}
}

// TestCruiseHeartbeat（P2）：心跳投影+告警线。
func TestCruiseHeartbeat(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	out := mustRun(t, dir, "cruise", "heartbeat", "--min", "30")
	if !strings.Contains(out, "座位心跳") || !strings.Contains(out, "data") {
		t.Fatalf("heartbeat must show seats:\n%s", out)
	}
}

// TestCruiseDispatch（P4）：调度器输出（座位列表+活况+idle 汇总）。
func TestCruiseDispatch(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	out := mustRun(t, dir, "cruise", "dispatch", "--rounds", "1", "--seats", "data", "--wait", "1")
	if !strings.Contains(out, "cruise dispatch") || !strings.Contains(out, "data") {
		t.Fatalf("dispatch output:\n%s", out)
	}
	if !strings.Contains(out, "给外部 runner 消费") {
		t.Fatalf("有活轮必须等消费窗口（§36 Y5 灭自旋）:\n%s", out)
	}
}
