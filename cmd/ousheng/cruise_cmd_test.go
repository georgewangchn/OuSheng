package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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

// TestAdapterCruiseAsset：默认不装；--cruise 写 scripts/cruise-loop.sh（可执行）
// + systemd 模板（§50-B 点火载体）；指引含 cruise ignite、不含 cron 样例
// （§45 取消定时自启——文档/指引漂移锁）。
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
	// systemd 模板随资产分发；StartLimit 在 [Unit] 节（A 机实锤：放 [Service]
	// 被 systemd 静默忽略——限流失效）。
	svc, err := os.ReadFile(filepath.Join(repo, "scripts", "ousheng-cruise@.service"))
	if err != nil {
		t.Fatalf("--cruise must write systemd template: %v", err)
	}
	s := string(svc)
	if !strings.Contains(s, "StartLimitIntervalSec=3600") ||
		strings.Index(s, "StartLimitIntervalSec") > strings.Index(s, "[Service]") {
		t.Fatalf("StartLimit* must live in [Unit] section:\n%s", s)
	}
	// §57：驾驶舱常驻单元随同批资产分发——「PM 唯一写面」的前提是驾驶舱一直在。
	websvc, err := os.ReadFile(filepath.Join(repo, "scripts", "ousheng-web.service"))
	if err != nil {
		t.Fatalf("--cruise must write web cockpit unit: %v", err)
	}
	w := string(websvc)
	for _, want := range []string{"Restart=always", "EnvironmentFile=/etc/default/ousheng-web", "OUSHENG_WEB_TOKEN", "enable --now"} {
		if !strings.Contains(w, want) {
			t.Fatalf("web unit 缺 %q:\n%s", want, w)
		}
	}
	if strings.Contains(w, "--token <") || strings.Contains(w, "--token sekret") {
		t.Fatalf("令牌不得以字面量进单元文件的命令行:\n%s", w)
	}
	// doctor 不受影响（可选资产不装不查；装了=canonical 一致 PASS——§50-C）。
	if out := mustRun(t, repo, "doctor"); strings.Contains(out, "FAIL") {
		t.Fatalf("optional asset must not break doctor:\n%s", out)
	}
	if out := mustRun(t, repo, "doctor"); !strings.Contains(out, "巡航驱动器 = canonical") {
		t.Fatalf("installed driver must PASS canonical check:\n%s", out)
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
	// §44 自动清理：pm 代答后不再计入
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "x",
		"--note", "【pm 代答】Q(pm) 已关闭：已按建议处置", "--actor", "pm")
	out = mustRun(t, dir, "cruise", "report", "--hours", "24")
	if strings.Contains(out, "Q(pm) 攒批") {
		t.Fatalf("代答后不应再出现在积压:\n%s", out)
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

// TestCruisePlanDivergedGate（§42 三修②）：板仓与远端分叉（两源并发写入）时
// plan 必须拒绝派活——11-14 事故机制：pm 侧积压 40 提交期间座位继续按旧态领活，
// 终致 merge「取终态」拍平 5 卡（done 拍回 ready）。门在派活入口，积压即断。
func TestCruisePlanDivergedGate(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	bare := filepath.Join(t.TempDir(), "central.git")
	gitRun(t, dir, "init", "--bare", bare)
	gitRun(t, dir, "remote", "add", "origin", bare)
	gitRun(t, dir, "push", "-q", "-u", "origin", "main")
	clone := filepath.Join(t.TempDir(), "c2")
	gitRun(t, t.TempDir(), "clone", "-q", bare, clone)
	// 双侧各写 → clone 分叉
	mustRun(t, dir, "todo", "A侧单", "--system", "lake", "--actor", "pm")
	gitRun(t, dir, "push", "-q")
	gitRun(t, clone, "commit", "-q", "--allow-empty", "-m", "clone侧提交")
	so, se, code := runIn(t, clone, "cruise", "plan", "--actor", "data")
	if code == 0 {
		t.Fatalf("分叉板仓 plan 必须拒派活:\n%s%s", so, se)
	}
	if !strings.Contains(se, "分叉") || !strings.Contains(se, "sync") {
		t.Fatalf("报错须指引 sync 收口: %s", se)
	}
	// 收口后恢复派活
	gitRun(t, clone, "reset", "-q", "--hard", "origin/main")
	so2, _, code2 := runIn(t, clone, "cruise", "plan", "--actor", "data")
	if code2 != 0 || !strings.Contains(so2, "cruise plan") {
		t.Fatalf("收口后 plan 应恢复:\n%s", so2)
	}
}

// TestCruisePlanEnvGate（§42 三修①）：座位环境域 × 卡 env 门——dev 座位不见
// local 卡、local 座位不见 dev 卡（11-14 事故：T-190/192 dev 座位领 local 活）。
func TestCruisePlanEnvGate(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	mustRun(t, dir, "team", "add", "data-local", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "lake")
	// 两张 ready 无主卡：一 dev（env 空）一 local
	mustRun(t, dir, "work", "create", "--id", "E-001", "--type", "task", "--title", "dev活", "--system", "lake", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "E-001", "--status", "ready", "--actor", "pm")
	mustRun(t, dir, "work", "create", "--id", "E-002", "--type", "task", "--title", "local活", "--system", "lake", "--env", "local", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "E-002", "--status", "ready", "--actor", "pm")

	outDev := mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if !strings.Contains(outDev, "E-001") {
		t.Fatalf("dev 座位应见 dev 卡:\n%s", outDev)
	}
	if strings.Contains(outDev, "E-002") {
		t.Fatalf("dev 座位不得见 local 卡（环境门）:\n%s", outDev)
	}
	outLocal := mustRun(t, dir, "cruise", "plan", "--actor", "data-local")
	if !strings.Contains(outLocal, "E-002") {
		t.Fatalf("local 座位应见 local 卡:\n%s", outLocal)
	}
	if strings.Contains(outLocal, "E-001") {
		t.Fatalf("local 座位不得见 dev 卡（环境门）:\n%s", outLocal)
	}
}

// TestWorkAssignEnvGate（§42 修①写面）：assign 拒跨环境领活——plan 门挡计划节，
// 写面门挡手动领活（读面+写面双门，缺一即绕）。
func TestWorkAssignEnvGate(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	mustRun(t, dir, "work", "create", "--id", "E-010", "--type", "task", "--title", "local活",
		"--system", "lake", "--env", "local", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "E-010", "--status", "ready", "--actor", "pm")
	// dev 座位领 local 卡 → 拒
	so, se, code := runIn(t, dir, "work", "assign", "E-010", "--assignee", "data", "--role", "dev", "--actor", "data")
	if code == 0 {
		t.Fatalf("dev 座位领 local 卡必须拒（写面环境门）:\n%s", so)
	}
	if !strings.Contains(se, "环境") {
		t.Fatalf("报错须指明环境不匹配: %s", se)
	}
	// local 座位领 local 卡 → 通
	mustRun(t, dir, "team", "add", "data-local", "--type", "agent", "--responsible-human", "pm", "--role", "dev", "--system", "lake")
	mustRun(t, dir, "work", "assign", "E-010", "--assignee", "data-local", "--role", "dev", "--actor", "data-local")
	// 门逼 env 正确性：pm 先把另一张卡 env 归位 → dev 即可领（非死锁）
	mustRun(t, dir, "work", "create", "--id", "E-011", "--type", "task", "--title", "改域卡",
		"--system", "lake", "--env", "local", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "E-011", "--status", "ready", "--actor", "pm")
	mustRun(t, dir, "work", "update", "E-011", "--env", "-", "--actor", "pm")
	so2, _, code2 := runIn(t, dir, "work", "assign", "E-011", "--assignee", "data", "--role", "dev", "--actor", "data")
	if code2 != 0 {
		t.Fatalf("env 归位后 dev 应可领:\n%s", so2)
	}
}

// TestCruisePlanReadyResume（§46，11-14 空转事件）：assignee=me 的 ready 卡必须
// 进恢复节——否则「已指派未开工」在 plan 里隐身，座位空转记账（18 张有主 ready
// 卡隐身处实锤）。
func TestCruisePlanReadyResume(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir) // T-001 doing@data
	mustRun(t, dir, "work", "create", "--id", "R-1", "--type", "task", "--title", "指派未开工",
		"--system", "lake", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "R-1", "--status", "ready", "--actor", "pm")
	mustRun(t, dir, "work", "assign", "R-1", "--assignee", "data", "--role", "dev", "--actor", "pm")

	out := mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if !strings.Contains(out, "R-1") || !strings.Contains(out, "已指派未开工") {
		t.Fatalf("ready@me 必须进恢复节并标注未开工:\n%s", out)
	}
	// 他人名下的 ready 不进我的恢复（归属不变）
	mustRun(t, dir, "work", "create", "--id", "R-2", "--type", "task", "--title", "别人的活",
		"--system", "lake", "--description", "d", "--actor", "pm")
	mustRun(t, dir, "work", "update", "R-2", "--status", "ready", "--actor", "pm")
	mustRun(t, dir, "work", "assign", "R-2", "--assignee", "pm", "--role", "dev", "--actor", "pm")
	out2 := mustRun(t, dir, "cruise", "plan", "--actor", "data")
	if strings.Contains(out2, "R-2") {
		t.Fatalf("他人 ready 不得进我的恢复节:\n%s", out2)
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

// TestCruisePlanPreFlight（§55 入口自检三联门）：板仓非 main 分支拒 /
// unmerged 残留拒 / 多 remote 水位差拒派活。
func TestCruisePlanPreFlight(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	// 非 main 分支：新建 feature 分支后 plan 应拒。
	run := func(args ...string) {
		c := append([]string{"-C", dir}, args...)
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("checkout", "-q", "-b", "feature-x")
	if out := mustRunFail(t, dir, "cruise", "plan", "--actor", "data", "--dir", dir); !strings.Contains(out, "非 main") {
		t.Fatalf("非 main 分支应拒:\n%s", out)
	}
	run("checkout", "-q", "main")
	// 多 remote 水位差：双 bare，一旧。
	og1 := filepath.Join(t.TempDir(), "og1")
	og2 := filepath.Join(t.TempDir(), "og2")
	exec.Command("git", "init", "-q", "--bare", og1).Run()
	exec.Command("git", "init", "-q", "--bare", og2).Run()
	run("remote", "add", "og1", og1)
	run("remote", "add", "og2", og2)
	run("push", "-q", "og1", "main")
	run("push", "-q", "og2", "main")
	// soft-only（本地未推 og2）→ 警告放行（review 🔴1 语义：ahead 低危）。
	mustRun(t, dir, "todo", "未推卡", "--system", "lake", "--actor", "data", "--dir", dir)
	run("push", "-q", "og1", "main")
	softSo, softSe, _ := runIn(t, dir, "cruise", "plan", "--actor", "data", "--dir", dir)
	if out := softSo + softSe; !strings.Contains(out, "未推平") {
		t.Fatalf("ahead-only 应警告放行（低危，stderr）:\n%s", out)
	}
	// hard（og2 侧有本地缺的新）→ 拒派活 + 拒词含「分叉」（驱动器等待自愈兼容，review 🔴3）。
	ws2 := filepath.Join(t.TempDir(), "ws2")
	run("clone", "-q", og2, ws2)
	mustRun(t, ws2, "todo", "远端新卡", "--system", "lake", "--actor", "data", "--dir", ws2)
	c := append([]string{"-C", ws2}, "push", "-q", "origin", "main")
	if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
		t.Fatalf("push ws2: %v %s", err, out)
	}
	run("fetch", "-q", "og2")
	if out := mustRunFail(t, dir, "cruise", "plan", "--actor", "data", "--dir", dir); !strings.Contains(out, "分叉") {
		t.Fatalf("本地缺新应拒（含「分叉」兼容驱动器）:\n%s", out)
	}
	// 收口（fixture 捷径：reset 到 og2 后同步 og1——activity 同文件冲突免逐卡）
	_ = exec.Command("git", "-C", dir, "pull", "-q", "--rebase", "og2", "main").Run()
	run("rebase", "--abort")
	run("reset", "-q", "--hard", "og2/main")
	run("push", "-q", "-f", "og1", "main")
	if out := mustRun(t, dir, "cruise", "plan", "--actor", "data", "--dir", dir); strings.Contains(out, "分叉") {
		t.Fatalf("收口后应放行:\n%s", out)
	}
}

// TestCruiseIgniteParamChecks（§55 H3/H4）：截止已过拒；模型退化警告。
func TestCruiseIgniteParamChecks(t *testing.T) {
	ws, _, unitDir, etcDir := igniteFixture(t)
	t.Setenv("OUSHENG_SYSTEMD_DIR", unitDir)
	fakeSystemctl(t)
	envf := filepath.Join(etcDir, "ousheng-cruise-data")
	w := func(body string) { os.WriteFile(envf, []byte(body), 0o644) }
	// 截止已过 → 拒。
	w("CRUISE_REPO=/x\nOUSHENG_CRUISE_ARGS=\"" + ws + " data lake /x 20 600 00:00-00:00 2020-01-01 00:00\"\n")
	t.Setenv("OUSHENG_ETC_DEFAULT", etcDir)
	if out := mustRunFail(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); !strings.Contains(out, "已过期") {
		t.Fatalf("过期截止应拒:\n%s", out)
	}
	// 模型退化 → 警告不拒。
	w("CRUISE_REPO=/x\nOUSHENG_CRUISE_ARGS=\"" + ws + " data lake /x 20 600 00:00-00:00 2099-01-01 00:00\"\nOC_MODEL_CODE=deepseek/deepseek-flash\nOC_MODEL_WAIT=deepseek/deepseek-flash\n")
	if out := mustRun(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); !strings.Contains(out, "模型路由退化") {
		t.Fatalf("退化配置应有警告:\n%s", out)
	}
	// 正常模型无警告。
	w("CRUISE_REPO=/x\nOUSHENG_CRUISE_ARGS=\"" + ws + " data lake /x 20 600 00:00-00:00 2099-01-01 00:00\"\nOC_MODEL_CODE=deepseek/deepseek-v4-pro\nOC_MODEL_WAIT=deepseek/deepseek-flash\n")
	if out := mustRun(t, ws, "cruise", "ignite", "--seat", "data", "--dir", ws); strings.Contains(out, "退化") {
		t.Fatalf("正常路由不应警告:\n%s", out)
	}
}

// TestWebHealthChip（§55 R3）：驾驶舱轻检——分支异常/水位差上 chip；正常显示 ✓。
func TestWebHealthChip(t *testing.T) {
	dir := cruiseFixture(t)
	mkCruiseBoard(t, dir)
	page := webGet(t, dir, "/")
	if !strings.Contains(page, "自检 ✓") {
		t.Fatalf("健康态应有自检 chip:\n%s", page[:400])
	}
	run := func(args ...string) {
		c := append([]string{"-C", dir}, args...)
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("checkout", "-q", "-b", "feature-y")
	page = webGet(t, dir, "/")
	if !strings.Contains(page, "自检：") || !strings.Contains(page, "feature-y") {
		t.Fatalf("分支异常应上 chip:\n%s", page[:400])
	}
}
