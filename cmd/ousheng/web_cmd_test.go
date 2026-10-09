package main

// web 面板测试锁（档案 §24）：
//   TestWebOpsMatchCLI           —— 五操作 web/CLI 状态等价（一语义一实现）
//   TestWebDecideOnAgentMachineRejected —— web 面 §23 语义保持（类型门）
//   TestWebAckOnAgentMachineRejected    —— ack 深层门语义保持
//   TestWebRejectsForeignOrigin —— CSRF 借用拍板权必拒
//   TestWebLoopbackOnly         —— 传输层封口：非回环绑定必拒
//   TestWebCmdRequiresMe        —— fail-closed：me 缺失不起服务
//   TestWebNoActorFromClient    —— 表单 actor 字段被忽略，归因 = me
//   TestWebBoardRenders         —— 投影面可用性

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

func webSetup(t *testing.T, dir string) {
	t.Helper()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "datax-server")
	mustRun(t, dir, "team", "add", "dev-agent", "--type", "agent", "--responsible-human", "george")
	mustRun(t, dir, "todo", "任务一", "--system", "datax-server") // T-001
}


// injectBreaking：直改 yaml 注入 breaking 契约（模拟 C2 未确认态的真实来源——
// v1 迁移/手改；写路径引入 breaking 必须原子带 ack，无此态）。
func injectBreaking(t *testing.T, dir, id string) {
	t.Helper()
	p := filepath.Join(dir, ".ousheng", "work", id+".yaml")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	blk := "contract:\n  kind: lib\n  status: proposed\n  breaking: true\n"
	if err := os.WriteFile(p, append(raw, []byte(blk)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func webPost(t *testing.T, dir, path string, form url.Values, origin string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(newWebMux(dir))
	defer srv.Close()
	req, err := http.NewRequest("POST", srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func webGet(t *testing.T, dir, path string) string {
	t.Helper()
	srv := httptest.NewServer(newWebMux(dir))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(resp.Body)
	return b.String()
}

// normalizeBlank：等价比较前把时间性字段归零（RFC3339 字符串逐次必异）。
// 递归穿透指针与嵌套 struct（HumanAck.At / Evidence[].ObservedAt 等）。
func normalizeBlank(v any) {
	normalizeBlankImpl(reflect.ValueOf(v).Elem())
}

func normalizeBlankImpl(rv reflect.Value) {
	switch rv.Kind() {
	case reflect.Ptr:
		if !rv.IsNil() {
			normalizeBlankImpl(rv.Elem())
		}
	case reflect.Struct:
		for i := 0; i < rv.NumField(); i++ {
			if !rv.Type().Field(i).IsExported() {
				continue
			}
			f := rv.Field(i)
			if f.Kind() == reflect.String && (strings.HasSuffix(rv.Type().Field(i).Name, "At") || rv.Type().Field(i).Name == "TS") {
				f.SetString("")
				continue
			}
			normalizeBlankImpl(f)
		}
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			normalizeBlankImpl(rv.Index(i))
		}
	}
}

func loadItem(t *testing.T, dir, id string) model.WorkItem {
	t.Helper()
	w, err := gityaml.Open(dir).GetWorkItem(id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// TestWebOpsMatchCLI：五操作走 web（httptest → run() 派发器）与走 CLI，
// 终态工单/方案状态等价（时间字段归零后 DeepEqual）。
func TestWebOpsMatchCLI(t *testing.T) {
	cliDir, webDir := t.TempDir(), t.TempDir()
	webSetup(t, cliDir)
	webSetup(t, webDir)

	// ① status：CLI vs web
	mustRun(t, cliDir, "work", "update", "T-001", "--status", "ready", "--expect", "1", "--actor", "george")
	if resp := webPost(t, webDir, "/work/T-001/status", url.Values{"status": {"ready"}, "expect": {"1"}}, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("web status must 303, got %d", resp.StatusCode)
	}
	a, b := loadItem(t, cliDir, "T-001"), loadItem(t, webDir, "T-001")
	normalizeBlank(&a)
	normalizeBlank(&b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("status op diverged:\nCLI: %+v\nWEB: %+v", a, b)
	}

	// ② assign
	mustRun(t, cliDir, "work", "assign", "T-001", "--assignee", "dev-agent", "--expect", "2", "--actor", "george")
	webPost(t, webDir, "/work/T-001/assign", url.Values{"assignee": {"dev-agent"}, "expect": {"2"}}, "")
	a, b = loadItem(t, cliDir, "T-001"), loadItem(t, webDir, "T-001")
	normalizeBlank(&a)
	normalizeBlank(&b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("assign op diverged:\nCLI: %+v\nWEB: %+v", a, b)
	}

	// ③ create（task：todo 自动 T-002）
	mustRun(t, cliDir, "todo", "任务二", "--system", "datax-server")
	webPost(t, webDir, "/work/create", url.Values{"kind": {"task"}, "title": {"任务二"}, "system": {"datax-server"}}, "")
	a, b = loadItem(t, cliDir, "T-002"), loadItem(t, webDir, "T-002")
	normalizeBlank(&a)
	normalizeBlank(&b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("create op diverged:\nCLI: %+v\nWEB: %+v", a, b)
	}

	// ④ decide（draft → agreed）
	for _, d := range []string{cliDir, webDir} {
		writeDesignFile(t, d, "csv-plan", "status: draft\nowner: dev-agent\n", "# 方案\n")
	}
	mustRun(t, cliDir, "design", "decide", "csv-plan", "--actor", "george")
	if resp := webPost(t, webDir, "/design/csv-plan/decide", nil, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("web decide must 303, got %d", resp.StatusCode)
	}
	loadDesign := func(dir string) model.DesignDoc {
		raw, err := os.ReadFile(filepath.Join(dir, ".ousheng", "designs", "csv-plan", "design.md"))
		if err != nil {
			t.Fatal(err)
		}
		var wrap struct {
			Doc model.DesignDoc `yaml:",inline"`
		}
		if err := yaml.Unmarshal(bytes.SplitN(raw, []byte("---"), 3)[1], &wrap); err != nil {
			t.Fatal(err)
		}
		return wrap.Doc
	}
	da, db := loadDesign(cliDir), loadDesign(webDir)
	da.DecidedAt, db.DecidedAt = "", ""
	if !reflect.DeepEqual(da, db) {
		t.Fatalf("decide op diverged:\nCLI: %+v\nWEB: %+v", da, db)
	}

	// ⑤ ack（C2：breaking 未 ack 态来自迁移/手改——写路径引入 breaking 必须同时
	// 带 ack，原子不可分。直改 yaml 造真实现场态，再比对 CLI/web ack 等价。）
	for _, d := range []string{cliDir, webDir} {
		mustRun(t, d, "work", "create", "--id", "T-003", "--title", "破坏性变更", "--type", "task", "--system", "datax-server")
		// backlog→ready 先走合法迁移（r2），再直改注入 breaking 契约（无 ack）
		mustRun(t, d, "work", "update", "T-003", "--status", "ready", "--expect", "1", "--actor", "george")
		injectBreaking(t, d, "T-003")
	}
	cliSwap := filepath.Join(cliDir, "swap.yaml")
	cur, _ := os.ReadFile(filepath.Join(cliDir, ".ousheng", "work", "T-003.yaml"))
	os.WriteFile(cliSwap, cur, 0o644)
	mustRun(t, cliDir, "work", "update", "T-003", "--file", cliSwap, "--expect", "2", "--ack", "--actor", "george")
	os.Remove(cliSwap)
	if resp := webPost(t, webDir, "/work/T-003/ack", nil, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("web ack must 303, got %d", resp.StatusCode)
	}
	a, b = loadItem(t, cliDir, "T-003"), loadItem(t, webDir, "T-003")
	normalizeBlank(&a)
	normalizeBlank(&b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("ack op diverged:\nCLI: %+v\nWEB: %+v", a, b)
	}
	if a.HumanAck == nil || a.HumanAck.Approver != "george" {
		t.Fatalf("ack must set human approver, got %+v", a.HumanAck)
	}
}

// TestWebDecideOnAgentMachineRejected：agent 座位机 web（me=agent）拍板 →
// 类型门拒（web 面 §23 语义保持：表单无 actor 字段，身份=me，agent 不可拍板）。
func TestWebDecideOnAgentMachineRejected(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	writeDesignFile(t, dir, "csv-plan", "status: draft\nowner: dev-agent\n", "# 方案\n")
	// 模拟 agent 座位机：me 重写为 agent
	if err := os.WriteFile(filepath.Join(dir, ".ousheng", "me"), []byte("dev-agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp := webPost(t, dir, "/design/csv-plan/decide", nil, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("agent-machine web decide must be rejected, got %d", resp.StatusCode)
	}
}

// TestWebAckOnAgentMachineRejected：agent 座位机 web ack → 深层类型门拒。
func TestWebAckOnAgentMachineRejected(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready", "--expect", "1", "--actor", "george")
	injectBreaking(t, dir, "T-001")
	if err := os.WriteFile(filepath.Join(dir, ".ousheng", "me"), []byte("dev-agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp := webPost(t, dir, "/work/T-001/ack", nil, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("agent-machine web ack must be rejected, got %d", resp.StatusCode)
	}
	if w := loadItem(t, dir, "T-001"); w.HumanAck != nil {
		t.Fatalf("agent-machine web ack must not write HumanAck, got %+v", w.HumanAck)
	}
}

// TestWebRejectsForeignOrigin：他源 Origin 的 POST = 浏览器侧借用拍板权，403。
func TestWebRejectsForeignOrigin(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	resp := webPost(t, dir, "/work/T-001/status", url.Values{"status": {"ready"}, "expect": {"1"}}, "http://evil.example.com")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin must be 403, got %d", resp.StatusCode)
	}
	if w := loadItem(t, dir, "T-001"); w.Status != model.StatusBacklog {
		t.Fatalf("foreign origin POST must not mutate, got status %s", w.Status)
	}
}

// TestWebLoopbackOnly：传输层封口——非回环绑定必拒（fail-closed）。
func TestWebLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", "192.168.1.5:80", ":9000", "junk"} {
		if err := validateLoopback(addr); err == nil {
			t.Fatalf("non-loopback addr %q must be rejected", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:7878", "localhost:80", "[::1]:9000"} {
		if err := validateLoopback(addr); err != nil {
			t.Fatalf("loopback addr %q must pass, got %v", addr, err)
		}
	}
}

// TestWebCmdRequiresMe：me 缺失不起服务（fail-closed，§23/§24）。
func TestWebCmdRequiresMe(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init") // 无 me
	var so, se bytes.Buffer
	if code := cmdWeb([]string{"--dir", dir, "--addr", "127.0.0.1:0"}, &so, &se); code == 0 || !strings.Contains(se.String(), "未声明身份") {
		t.Fatalf("web without me must fail-closed, code=%d err=%s", code, se.String())
	}
}

// TestWebNoActorFromClient：表单塞 actor 字段 = 被忽略，归因恒 = me。
func TestWebNoActorFromClient(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	form := url.Values{"assignee": {"dev-agent"}, "expect": {"1"}, "actor": {"mlake"}}
	if resp := webPost(t, dir, "/work/T-001/assign", form, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("assign must succeed (actor field ignored), got %d", resp.StatusCode)
	}
	// activity 归因必须是 me（george），客户端注入的 mlake 不得出现
	am, err := os.ReadFile(filepath.Join(dir, ".ousheng", "activity"))
	if err != nil {
		t.Logf("no activity dir: %v", err)
	} else if strings.Contains(string(am), "mlake") {
		t.Fatalf("client-injected actor must never appear in activity")
	}
	if w := loadItem(t, dir, "T-001"); w.Assignee != "dev-agent" {
		t.Fatalf("assign must take effect, got %q", w.Assignee)
	}
}

// TestWebBoardRenders：投影面可用（五列 + 拍板队列 + 待认领标记）。
func TestWebBoardRenders(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	mustRun(t, dir, "work", "create", "--id", "T-002", "--title", "无主单", "--type", "task", "--system", "datax-server")
	writeDesignFile(t, dir, "csv-plan", "status: draft\nowner: dev-agent\n", "# 方案\n")
	page := webGet(t, dir, "/")
	for _, want := range []string{"T-001", "backlog", "你的下一步", "csv-plan", "待认领", "datax-server"} {
		if !strings.Contains(page, want) {
			t.Fatalf("board page missing %q", want)
		}
	}
	detail := webGet(t, dir, "/work/T-001")
	if !strings.Contains(detail, "任务一") || !strings.Contains(detail, "backlog") {
		t.Fatalf("detail page missing item info")
	}
}

// TestWebSuggestionPanel：「你的下一步」= 规则性关注聚合（档案 §24 追记）——
// 拍板/卡住/无主/超期/依赖被砍 五类信号确定性推导，无语义排序。
func TestWebSuggestionPanel(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	// 卡住：T-001 → ready → doing → blocked（activity 审计流留痕，天数=0）
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready", "--expect", "1", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "doing", "--expect", "2", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "blocked", "--expect", "3", "--actor", "george")
	// 超期：T-002 截止日已过
	mustRun(t, dir, "todo", "超期任务", "--system", "datax-server", "--due", "2020-01-01", "--assignee", "dev-agent")
	// 依赖被砍：T-003 依赖 T-004，T-004 作废
	mustRun(t, dir, "work", "create", "--id", "T-003", "--title", "依赖者", "--type", "task", "--system", "datax-server", "--depends-on", "T-004", "--actor", "george")
	mustRun(t, dir, "work", "create", "--id", "T-004", "--title", "被砍依赖", "--type", "task", "--system", "datax-server", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-004", "--status", "cancelled", "--expect", "1", "--actor", "george")
	// active 无主：写路径硬门挡（active requires assignee），手改模拟（真实来源=手改/迁移）
	mustRun(t, dir, "work", "create", "--id", "T-005", "--title", "无主进行中", "--type", "task", "--system", "datax-server", "--assignee", "dev-agent", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-005", "--status", "ready", "--expect", "1", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-005", "--status", "doing", "--expect", "2", "--actor", "george")
	p := filepath.Join(dir, ".ousheng", "work", "T-005.yaml")
	raw, _ := os.ReadFile(p)
	os.WriteFile(p, bytes.ReplaceAll(raw, []byte("assignee: dev-agent\n"), []byte("")), 0o644)

	page := webGet(t, dir, "/")
	for _, want := range []string{
		"你的下一步",
		"卡住了的活", "T-001", "今天刚卡住",
		"已超期", "T-002", "2020-01-01",
		"依赖被砍", "T-003",
		"进行中却没人负责", "T-005",
		"待认领池",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("suggestion panel missing %q", want)
		}
	}
}

// TestWebNoStoreHeader：实时看板禁缓存——陈旧页 = 陈旧决策（档案 §24）。
func TestWebNoStoreHeader(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	srv := httptest.NewServer(withNoStore(newWebMux(dir)))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("board must be no-store, got %q", cc)
	}
}

// TestWebDesignView：方案详情页（PM 反馈「有方案但查看不了」）——正文/轮次/状态中文。
func TestWebDesignView(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	writeDesignFile(t, dir, "csv-plan", "status: draft\nowner: dev-agent\n", "# 方案\n导出到 CSV 的分阶段路径。\n")
	os.MkdirAll(filepath.Join(dir, ".ousheng", "designs", "csv-plan"), 0o755)
	os.WriteFile(filepath.Join(dir, ".ousheng", "designs", "csv-plan", "round-1.md"), []byte("## dev-agent — 2026-10-08\n第一轮意见。"), 0o644)
	page := webGet(t, dir, "/design/csv-plan")
	for _, want := range []string{"csv-plan", "草案", "dev-agent", "导出到 CSV", "第 1 轮", "第一轮意见"} {
		if !strings.Contains(page, want) {
			t.Fatalf("design view missing %q", want)
		}
	}
	// 路径穿越必拒（mux/client 会规范化路径，直测 handler 门）
	for _, evil := range []string{"..", "../me", "a/b", "."} {
		req := httptest.NewRequest("GET", "/design/x", nil)
		req.SetPathValue("topic", evil)
		rec := httptest.NewRecorder()
		(&webServer{dir: dir, me: "george"}).handleDesignView(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("topic %q must be 400, got %d", evil, rec.Code)
		}
	}
}

// TestWebArchiveRenders：历史归档——完结单可见 + 完结日期审计流推导。
func TestWebArchiveRenders(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready", "--expect", "1", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "doing", "--expect", "2", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-001", "--status", "done", "--expect", "3", "--actor", "george")
	page := webGet(t, dir, "/")
	for _, want := range []string{"历史归档", "T-001", "完结于"} {
		if !strings.Contains(page, want) {
			t.Fatalf("archive section missing %q", want)
		}
	}
}

// TestWebAgreedSupersedeRenders：已生效方案的 supersede 下拉分支（真实事故：
// 命名类型 eq 在此分支执行中断致页面截断，测试环境无 agreed 方案漏盖）。
func TestWebAgreedSupersedeRenders(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	writeDesignFile(t, dir, "old-plan", "status: draft\nowner: dev-agent\n", "# 旧方案\n")
	mustRun(t, dir, "design", "decide", "old-plan", "--actor", "george") // → agreed
	writeDesignFile(t, dir, "new-plan", "status: draft\nowner: dev-agent\n", "# 新方案\n")
	page := webGet(t, dir, "/")
	for _, want := range []string{"old-plan", "new-plan（草案）", "废止并由它接替", "看方案 →", "历史归档"} {
		if !strings.Contains(page, want) {
			t.Fatalf("agreed/supersede/archive branch missing %q", want)
		}
	}
}

// TestWebDetailEvidenceRenders：带证据/进度/破坏性契约的详情页分支（真实事故：
// evidenceZh/basisZh 收 string 实传命名类型，模板执行中断；测试工单无证据
// 分支漏盖——本测试锁死该类）。
func TestWebDetailEvidenceRenders(t *testing.T) {
	dir := t.TempDir()
	webSetup(t, dir)
	// 证据 + 进度（T-001，无 breaking——C2 深门下 breaking 未 ack 单不可再写入）
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--source", "manual", "--locator", "check-001", "--result", "passed", "--note", "修复已合入", "--actor", "george")
	mustRun(t, dir, "progress", "report", "T-001", "--value", "0.7", "--actor", "george", "--basis", "test-cases")
	page := webGet(t, dir, "/work/T-001")
	for _, want := range []string{
		"证据链", "人工检查", "✓ 通过", "check-001", "修复已合入",
		"进度", "70%", "测试用例",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("detail evidence/progress branch missing %q", want)
		}
	}
	// 破坏性契约 banner 分支（T-002 手改注入 breaking，独立单避免 C2 深门挡写入）
	mustRun(t, dir, "work", "create", "--id", "T-002", "--title", "破坏单", "--type", "task", "--system", "datax-server", "--assignee", "dev-agent", "--actor", "george")
	injectBreaking(t, dir, "T-002")
	page2 := webGet(t, dir, "/work/T-002")
	for _, want := range []string{"破坏性变更", "确认通过"} {
		if !strings.Contains(page2, want) {
			t.Fatalf("detail C2 banner branch missing %q", want)
		}
	}
}
