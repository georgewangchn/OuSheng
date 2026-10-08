package main

// web 面板（档案 §24）：PM 观测 + 拍板的第 4 入口（CLI/MCP/plugin 之后）。
//
// 架构三决（正向推演产物，勿破坏）：
//  ① 字面级一语义一实现——handler 直调 run() 派发器（cmd 函数 = 纯函数
//     args→streams→exit code），门（类型/本地性/C2/CAS）在 cmd 函数体内，
//     web 面结构上绕不过。对照测试锁状态等价（TestWebOpsMatchCLI）。
//  ② 身份绑定 me——web 身份 = readWorkspaceActor（me 文件），不吸收座位配置；
//     表单永远没有 actor 字段，客户端不可指定身份。agent 机跑 web →
//     actor=me=agent → 拍板操作类型门自动拒 = web 天然只读拍板面。
//  ③ 传输层双封口——loopback 硬校验 + Origin 门（POST 他源 403），
//     §23 的「本机进程=本机 human」信任假设不被网络监听器打破。
//
// 边界（§24）：投影+薄写适配，永不写 YAML，永不碰注册表/配置（死法边界），
// 无智能无自动化无建议，读走 memory per-request，前台命令非常驻。

import (
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"ousheng/internal/context"
	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
)

func cmdWeb(args []string, stdout, stderr io.Writer) int {
	fs := newFS("web")
	addr := fs.String("addr", "127.0.0.1:7878", "listen address (loopback only)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	if err := validateLoopback(*addr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	dir := fs.Dir()
	// fail-closed：web 操作身份 = 本机 me，缺失即拒（§23/§24）。
	me := readWorkspaceActor(dir)
	if me == "" {
		fmt.Fprintln(stderr, "本机未声明身份（先 ousheng me <human>）——web 操作身份 = 本机 me，缺失即拒（fail-closed，档案 §23/§24）")
		return 1
	}
	// todo 路径经 resolveActor→actingActor 读进程 cwd 的座位配置——
	// chdir 到工作区使归因确定性落在 me（工作区无 .opencode 座位配置）。
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	mux := newWebMux(dir)
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "ousheng web → http://%s （工作区 %s，身份 %s；Ctrl-C 退出）\n", *addr, dir, me)
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// validateLoopback：web 只绑回环地址（档案 §24 ③）。LAN 暴露 = §23 拍板身份
// 在传输层重新开口（远程 curl POST = 无需借用即挥舞本机 me 的拍板权）。
func validateLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --addr %q: %v", addr, err)
	}
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	return fmt.Errorf("--addr 必须为回环地址（127.0.0.1/[::1]/localhost）——web 只绑本机（档案 §24：LAN 暴露 = 拍板身份在传输层重新开口，远程访问需另行裁决带鉴权方案）")
}

// ---------------------------------------------------------------------------
// server

type webServer struct {
	dir string
	me  string
}

func newWebMux(dir string) *http.ServeMux {
	s := &webServer{dir: dir, me: readWorkspaceActor(dir)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleBoard)
	mux.HandleFunc("GET /work/{id}", s.handleDetail)
	mux.HandleFunc("POST /work/{id}/status", s.guard(s.handleStatus))
	mux.HandleFunc("POST /work/{id}/assign", s.guard(s.handleAssign))
	mux.HandleFunc("POST /work/{id}/ack", s.guard(s.handleAck))
	mux.HandleFunc("POST /work/create", s.guard(s.handleCreate))
	mux.HandleFunc("POST /design/{topic}/decide", s.guard(s.handleDecide))
	mux.HandleFunc("POST /design/{topic}/supersede", s.guard(s.handleSupersede))
	mux.HandleFunc("POST /design/{topic}/withdraw", s.guard(s.handleWithdraw))
	return mux
}

// guard：写路径前置门——Origin 校验（§24 ③：浏览器侧 CSRF 借用拍板权必拒）
// + me 非空（fail-closed）。
func (s *webServer) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				http.Error(w, "cross-origin write rejected（档案 §24：浏览器侧借用拍板权必拒）", http.StatusForbidden)
				return
			}
		}
		if s.me == "" {
			http.Error(w, "本机未声明身份（先 ousheng me）——web 操作身份 = 本机 me，缺失即拒", http.StatusInternalServerError)
			return
		}
		next(w, r)
	}
}

// exec：直调 CLI 派发器——同一套 flag 解析、同一套门、同一套语义（§24 ①）。
func (s *webServer) exec(args ...string) (int, string, string) {
	full := append(append([]string{}, args...), "--dir", s.dir)
	var out, errb strings.Builder
	code := run(full, &out, &errb)
	return code, out.String(), errb.String()
}

// sync：页面加载前拉齐 + 写后收口推送（§24 R3：观测新鲜度 = 中央 HEAD，
// 失败显式 WARN 非静默）。
func (s *webServer) sync() string {
	_, out, _ := s.exec("sync")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

func (s *webServer) water() string {
	out, err := exec.Command("git", "-C", s.dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD").Output()
	if err != nil {
		return "—"
	}
	f := strings.Fields(string(out))
	if len(f) == 2 {
		return "落后 " + f[0] + " / 领先 " + f[1]
	}
	return "—"
}

func (s *webServer) fail(w http.ResponseWriter, code int, title, msg string) {
	w.WriteHeader(code)
	webErrorTmpl.Execute(w, map[string]any{"Title": title, "Msg": msg})
}

// ---------------------------------------------------------------------------
// 读路径

type webCol struct {
	Name  string
	Items []model.WorkItem
}

type boardData struct {
	Project     string
	Me          string
	SyncLine    string
	Water       string
	CurSystem   string
	Systems     []string
	Drafts      []model.DesignInfo
	Agreed      []model.DesignInfo
	C2Pending   []model.WorkItem
	ConvergeOut string
	Cols        []webCol
	DoneCount   int
}

func (s *webServer) handleBoard(w http.ResponseWriter, r *http.Request) {
	code, convOut, _ := s.exec("converge")
	_ = code // converge 面板纯展示（BLOCKER/WARNING 行文本自明）
	bd := boardData{
		Me:          s.me,
		SyncLine:    s.sync(),
		Water:       s.water(),
		CurSystem:   r.URL.Query().Get("system"),
		ConvergeOut: strings.TrimSpace(convOut),
	}
	repo := gityaml.Open(s.dir)
	if proj, err := repo.GetProject(); err == nil {
		bd.Project = proj.ID + " · " + proj.Name
	}
	if systems, err := repo.ListSystems(); err == nil {
		for _, sy := range systems {
			bd.Systems = append(bd.Systems, sy.ID)
		}
	}
	if designs, err := repo.ListDesigns(); err == nil {
		for _, d := range designs {
			switch d.Design.Status {
			case model.DesignDraft:
				bd.Drafts = append(bd.Drafts, d)
			case model.DesignAgreed:
				bd.Agreed = append(bd.Agreed, d)
			}
		}
	}
	c, err := ctxService(s.dir)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "工作区不可用", err.Error())
		return
	}
	items, err := c.Idx.All()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "索引装载失败", err.Error())
		return
	}
	byStatus := map[string][]model.WorkItem{}
	for _, w := range items {
		open := model.WorkItemOpen(w.Status)
		if bd.CurSystem != "" && w.System != bd.CurSystem {
			continue
		}
		if open {
			byStatus[string(w.Status)] = append(byStatus[string(w.Status)], w)
		} else {
			bd.DoneCount++
		}
		if open && w.Contract != nil && w.Contract.Breaking && w.HumanAck == nil {
			bd.C2Pending = append(bd.C2Pending, w)
		}
	}
	for _, st := range []string{"backlog", "ready", "doing", "blocked", "testing"} {
		bd.Cols = append(bd.Cols, webCol{Name: st, Items: byStatus[st]})
	}
	webBoardTmpl.Execute(w, bd)
}

type detailData struct {
	D        *context.WorkItemDetail
	Me       string
	Statuses []string
}

func (s *webServer) handleDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := ctxService(s.dir)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "工作区不可用", err.Error())
		return
	}
	d, err := c.GetWorkItem(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "工单不存在", err.Error())
		return
	}
	webDetailTmpl.Execute(w, detailData{
		D:        d,
		Me:       s.me,
		Statuses: []string{"backlog", "ready", "doing", "blocked", "testing", "done", "cancelled"},
	})
}

// ---------------------------------------------------------------------------
// 写路径（全部经 run() 派发器——门在 cmd 函数体内，§24 ①）

func (s *webServer) ok(w http.ResponseWriter, r *http.Request, to string) {
	s.sync()
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func (s *webServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	st := r.PostFormValue("status")
	exp := r.PostFormValue("expect")
	if st == "" || exp == "" {
		s.fail(w, http.StatusBadRequest, "参数缺失", "status 与 expect 必填")
		return
	}
	if code, _, errb := s.exec("work", "update", id, "--status", st, "--expect", exp, "--actor", s.me); code != 0 {
		s.fail(w, http.StatusBadRequest, "状态变更被拒", errb)
		return
	}
	s.ok(w, r, "/work/"+id)
}

func (s *webServer) handleAssign(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	asg := r.PostFormValue("assignee")
	role := r.PostFormValue("role")
	exp := r.PostFormValue("expect")
	if asg == "" || exp == "" {
		s.fail(w, http.StatusBadRequest, "参数缺失", "assignee 与 expect 必填")
		return
	}
	args := []string{"work", "assign", id, "--assignee", asg, "--expect", exp, "--actor", s.me}
	if role != "" {
		args = append(args, "--role", role)
	}
	if code, _, errb := s.exec(args...); code != 0 {
		s.fail(w, http.StatusBadRequest, "分配被拒", errb)
		return
	}
	s.ok(w, r, "/work/"+id)
}

// handleAck：C2 确认走 --file 全量路径（与 CLI 同门同序）。服务端 POST 时现取
// 当前 yaml + expect=当前 revision——PM ack 的是当下状态，非表单快照（§24 R2）。
func (s *webServer) handleAck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	repo := gityaml.Open(s.dir)
	cur, err := repo.GetWorkItem(id)
	if err != nil {
		s.fail(w, http.StatusNotFound, "工单不存在", err.Error())
		return
	}
	raw, err := yaml.Marshal(cur)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "序列化失败", err.Error())
		return
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("ousheng-web-ack-%s.yaml", id))
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		s.fail(w, http.StatusInternalServerError, "临时文件失败", err.Error())
		return
	}
	defer os.Remove(tmp)
	if code, _, errb := s.exec("work", "update", id, "--file", tmp, "--expect", strconv.Itoa(cur.Revision), "--ack", "--actor", s.me); code != 0 {
		s.fail(w, http.StatusBadRequest, "C2 确认被拒", errb)
		return
	}
	s.ok(w, r, "/work/"+id)
}

var webBugIDRe = regexp.MustCompile(`^BUG-(\d+)$`)

func (s *webServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	kind := r.PostFormValue("kind")
	title := strings.TrimSpace(r.PostFormValue("title"))
	system := r.PostFormValue("system")
	desc := r.PostFormValue("description")
	pri := r.PostFormValue("priority")
	if kind == "" || title == "" || system == "" {
		s.fail(w, http.StatusBadRequest, "参数缺失", "kind/title/system 必填")
		return
	}
	// §24 R5：create 限 task/bug——feature/requirement 动土先起 design（发布条件），
	// 快速表单不得绕过共识纪律。
	var code int
	var errb string
	switch kind {
	case "task":
		args := []string{"todo", title, "--system", system, "--description", desc}
		if pri != "" {
			args = append(args, "--priority", pri)
		}
		code, _, errb = s.exec(args...)
	case "bug":
		repo := gityaml.Open(s.dir)
		items, err := repo.ListWorkItems()
		if err != nil {
			s.fail(w, http.StatusInternalServerError, "枚举失败", err.Error())
			return
		}
		max := 0
		for _, w := range items {
			if m := webBugIDRe.FindStringSubmatch(w.ID); m != nil {
				if n, _ := strconv.Atoi(m[1]); n > max {
					max = n
				}
			}
		}
		id := fmt.Sprintf("BUG-%03d", max+1)
		args := []string{"bug", "report", "--id", id, "--title", title, "--system", system, "--detected-by", s.me, "--description", desc, "--actor", s.me}
		if pri != "" {
			args = append(args, "--priority", pri)
		}
		code, _, errb = s.exec(args...)
	default:
		s.fail(w, http.StatusBadRequest, "类型受限", "web 快速建单仅支持 task/bug（feature/requirement 需先起 design，档案 §24 R5）")
		return
	}
	if code != 0 {
		s.fail(w, http.StatusBadRequest, "建单被拒", errb)
		return
	}
	s.ok(w, r, "/")
}

func (s *webServer) handleDecide(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if code, _, errb := s.exec("design", "decide", topic, "--actor", s.me); code != 0 {
		s.fail(w, http.StatusBadRequest, "拍板被拒", errb)
		return
	}
	s.ok(w, r, "/")
}

func (s *webServer) handleSupersede(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	by := strings.TrimSpace(r.PostFormValue("by"))
	if by == "" {
		s.fail(w, http.StatusBadRequest, "参数缺失", "继任 topic 必填")
		return
	}
	if code, _, errb := s.exec("design", "supersede", topic, "--by", by, "--actor", s.me); code != 0 {
		s.fail(w, http.StatusBadRequest, "supersede 被拒", errb)
		return
	}
	s.ok(w, r, "/")
}

func (s *webServer) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if code, _, errb := s.exec("design", "withdraw", topic, "--actor", s.me); code != 0 {
		s.fail(w, http.StatusBadRequest, "撤回被拒", errb)
		return
	}
	s.ok(w, r, "/")
}

// ---------------------------------------------------------------------------
// 模板（stdlib html/template，自动转义；无外部资产，无前端构建链）

var webBoardTmpl = template.Must(template.New("board").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<title>㸸绳看板 · {{.Project}}</title>
<style>
:root{--ink:#1a1a1a;--dim:#777;--line:#e3e3e3;--acc:#0a6c5a;--warn:#a33;--bg:#fafafa}
*{box-sizing:border-box}body{margin:0;font:14px/1.55 -apple-system,"PingFang SC",sans-serif;color:var(--ink);background:var(--bg)}
header{padding:14px 20px;background:#fff;border-bottom:1px solid var(--line);display:flex;gap:16px;align-items:baseline;flex-wrap:wrap}
header h1{font-size:16px;margin:0}
.tag{font-size:12px;color:var(--dim)}
main{padding:16px 20px;max-width:1280px;margin:0 auto}
h2{font-size:14px;color:var(--dim);margin:20px 0 8px;font-weight:600}
.q{background:#fff;border:1px solid var(--line);border-radius:8px;padding:10px 14px;margin-bottom:8px;display:flex;gap:12px;align-items:center;flex-wrap:wrap}
.q b{font-family:ui-monospace,monospace}
.board{display:grid;grid-template-columns:repeat(5,1fr);gap:10px}
.col{background:#fff;border:1px solid var(--line);border-radius:8px;padding:8px}
.col h3{font-size:12px;color:var(--dim);margin:2px 4px 8px;text-transform:uppercase;letter-spacing:.05em}
.card{display:block;border:1px solid var(--line);border-radius:6px;padding:7px 9px;margin-bottom:7px;text-decoration:none;color:var(--ink);background:#fff}
.card:hover{border-color:var(--acc)}
.card .id{font-family:ui-monospace,monospace;font-size:12px;color:var(--acc)}
.card .t{font-size:13px;margin:2px 0}
.card .m{font-size:11px;color:var(--dim)}
.badge{display:inline-block;font-size:11px;border:1px solid var(--line);border-radius:4px;padding:0 5px;color:var(--dim);margin-right:4px}
.badge.no{color:var(--warn);border-color:var(--warn)}
form{display:inline}
button{font:inherit;font-size:12px;padding:3px 12px;border:1px solid var(--acc);background:var(--acc);color:#fff;border-radius:5px;cursor:pointer}
button.ghost{background:#fff;color:var(--acc)}
button.danger{background:var(--warn);border-color:var(--warn)}
input,select,textarea{font:inherit;font-size:13px;padding:4px 7px;border:1px solid var(--line);border-radius:5px}
details{margin-top:6px}pre{background:#f4f4f4;padding:10px;border-radius:6px;overflow:auto;font-size:12px;white-space:pre-wrap}
.nav a{color:var(--acc);text-decoration:none;font-size:12px;margin-right:10px}
.new{background:#fff;border:1px dashed var(--line);border-radius:8px;padding:12px 14px;margin-top:10px;display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end}
</style></head><body>
<header>
<h1>㸸绳 · {{.Project}}</h1>
<span class="tag">身份 {{.Me}}</span>
<span class="tag">水位 {{.Water}}</span>
<span class="tag">同步：{{.SyncLine}}</span>
<nav class="nav"><a href="/">全部</a>{{range .Systems}} <a href="?system={{.}}">{{.}}</a>{{end}}</nav>
</header>
<main>

<h2>拍板队列（待你出手）</h2>
{{if not .Drafts}}{{if not .C2Pending}}<p class="tag">无待拍板项</p>{{end}}{{end}}
{{range .Drafts}}<div class="q">
<b>{{.Topic}}</b><span class="tag">draft · owner {{.Design.Owner}} · systems {{range .Design.Systems}}{{.}} {{end}}</span>
<form method="post" action="/design/{{.Topic}}/decide"><button>拍板</button></form>
<form method="post" action="/design/{{.Topic}}/withdraw"><button class="ghost">撤回</button></form>
</div>{{end}}
{{range .C2Pending}}<div class="q">
<b><a href="/work/{{.ID}}">{{.ID}}</a></b><span>{{.Title}}</span>
<span class="tag">breaking 契约待 human 确认</span>
<form method="post" action="/work/{{.ID}}/ack"><button>确认 (ack)</button></form>
</div>{{end}}
{{if .Agreed}}<h2>已定方案（可 supersede）</h2>
{{range .Agreed}}<div class="q"><b>{{.Topic}}</b><span class="tag">agreed · {{.Design.DecidedBy}}</span>
<form method="post" action="/design/{{.Topic}}/supersede" style="display:inline-flex;gap:6px">
<input name="by" placeholder="继任 topic" size="14"><button class="ghost">supersede</button></form>
</div>{{end}}{{end}}

<h2>看板{{if .CurSystem}} · {{.CurSystem}}{{end}} <span class="tag">（已关闭 {{.DoneCount}} 张不在列）</span></h2>
<div class="board">
{{range .Cols}}<div class="col"><h3>{{.Name}}</h3>
{{range .Items}}<a class="card" href="/work/{{.ID}}">
<div class="id">{{.ID}}</div>
<div class="t">{{.Title}}</div>
<div class="m"><span class="badge">{{.Type}}</span>{{if .System}}<span class="badge">{{.System}}</span>{{end}}{{if .Assignee}}{{.Assignee}}{{else}}<span class="badge no">待认领</span>{{end}}{{if .Contract}}<span class="badge">{{.Contract.Kind}}/{{.Contract.Status}}</span>{{end}} r{{.Revision}}</div>
</a>{{end}}
{{if not .Items}}<p class="tag" style="padding:4px">—</p>{{end}}
</div>{{end}}
</div>

<h2>快速建单（task / bug）</h2>
<div class="new">
<form method="post" action="/work/create" style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end">
<select name="kind"><option value="task">task</option><option value="bug">bug</option></select>
<input name="title" placeholder="标题" size="28" required>
<select name="system">{{range .Systems}}<option value="{{.}}">{{.}}</option>{{end}}</select>
<select name="priority"><option value="">优先级—</option><option>P0</option><option>P1</option><option>P2</option><option>P3</option></select>
<input name="description" placeholder="描述/验收标准（可选）" size="40">
<button>建单</button>
</form>
<span class="tag">feature/requirement 需先起 design（动土先起 design，档案 §24 R5）</span>
</div>

<details><summary class="tag">converge 审计输出</summary><pre>{{.ConvergeOut}}</pre></details>
</main></body></html>`))

var webDetailTmpl = template.Must(template.New("detail").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<title>{{.D.ID}} · 㸸绳</title>
<style>
body{margin:0;font:14px/1.6 -apple-system,"PingFang SC",sans-serif;color:#1a1a1a;background:#fafafa}
header{padding:12px 20px;background:#fff;border-bottom:1px solid #e3e3e3}
main{padding:16px 20px;max-width:900px;margin:0 auto}
a{color:#0a6c5a}
h1{font-size:16px;margin:0 0 4px}
.tag{font-size:12px;color:#777}
pre.desc{background:#fff;border:1px solid #e3e3e3;border-radius:8px;padding:12px;white-space:pre-wrap;font-family:inherit}
section{margin:18px 0}
form.op{background:#fff;border:1px solid #e3e3e3;border-radius:8px;padding:10px 14px;margin:8px 0;display:flex;gap:8px;align-items:center;flex-wrap:wrap}
button{font:inherit;padding:4px 14px;border:1px solid #0a6c5a;background:#0a6c5a;color:#fff;border-radius:5px;cursor:pointer}
input,select{font:inherit;font-size:13px;padding:4px 7px;border:1px solid #e3e3e3;border-radius:5px}
.ev{background:#fff;border:1px solid #e3e3e3;border-radius:6px;padding:8px 12px;margin:6px 0;font-size:13px}
</style></head><body>
<header><nav><a href="/">← 看板</a></nav></header>
<main>
<h1>{{.D.ID}} · {{.D.Title}}</h1>
<p class="tag">{{.D.Type}} · {{.D.Status}} · r{{.D.Revision}}{{if .D.System}} · system {{.D.System}}{{end}}{{if .D.Assignee}} · assignee {{.D.Assignee}}{{else}} · <span style="color:#a33">待认领</span>{{end}}{{if .D.AccountableHuman}} · accountable {{.D.AccountableHuman}}{{end}}{{if .D.Priority}} · {{.D.Priority}}{{end}}{{if .D.Contract}} · contract {{.D.Contract.Kind}}/{{.D.Contract.Status}}{{if .D.Contract.Breaking}} breaking{{end}}{{end}}{{if .D.DetectedBy}} · detected_by {{.D.DetectedBy}}{{end}}</p>
{{if .D.Contract}}{{if and .D.Contract.Breaking (not .D.HumanAck)}}
<form class="op" method="post" action="/work/{{.D.ID}}/ack">
<span>C2：破坏性变更待 human 确认</span><button>确认 (ack)（以 {{.Me}} 身份）</button>
</form>{{end}}{{end}}
<section>
<form class="op" method="post" action="/work/{{.D.ID}}/status">
<span>状态 →</span>
<select name="status">{{range .Statuses}}{{if ne . (printf "%s" $.D.Status)}}<option value="{{.}}">{{.}}</option>{{end}}{{end}}</select>
<input type="hidden" name="expect" value="{{.D.Revision}}">
<button>变更</button>
</form>
<form class="op" method="post" action="/work/{{.D.ID}}/assign">
<span>分配 →</span>
<input name="assignee" placeholder="assignee actor" required>
<input name="role" placeholder="role（可选）" size="10">
<input type="hidden" name="expect" value="{{.D.Revision}}">
<button>分配</button>
</form>
</section>
{{if .D.Description}}<section><h3>描述</h3><pre class="desc">{{.D.Description}}</pre></section>{{end}}
{{if .D.Deps}}<section><h3>依赖</h3>{{range .D.Deps}}<div class="ev"><a href="/work/{{.ID}}">{{.ID}}</a> · {{.Status}} · {{.Title}}</div>{{end}}</section>{{end}}
{{if .D.Designs}}<section><h3>关联方案</h3>{{range .D.Designs}}<div class="ev">{{.Topic}} · {{.Status}}{{if .DecidedBy}} · decided by {{.DecidedBy}}{{end}}</div>{{end}}</section>{{end}}
{{if .D.Evidence}}<section><h3>证据链</h3>{{range .D.Evidence}}<div class="ev"><b>{{.Type}}</b> · {{.Result}} · {{.Locator}}{{if .Note}}<br><span class="tag">{{.Note}}</span>{{end}}<br><span class="tag">{{.Source}} · {{.ObservedAt}}</span></div>{{end}}</section>{{end}}
{{if .D.Progress}}<section><h3>进度（reported）</h3><div class="ev">{{.D.Progress.Actor}} 报 {{.D.Progress.Value}} · basis {{.D.Progress.Basis}} · {{.D.Progress.ReportedAt}}</div></section>{{end}}
{{if .D.DependsOn}}<section><h3>depends_on</h3><p class="tag">{{range .D.DependsOn}}{{.}} {{end}}</p></section>{{end}}
</main></body></html>`))

var webErrorTmpl = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>操作被拒</title></head>
<body style="font:14px/1.6 -apple-system,'PingFang SC',sans-serif;padding:40px">
<h2 style="color:#a33">{{.Title}}</h2>
<pre style="background:#f4f4f4;padding:12px;border-radius:8px;white-space:pre-wrap">{{.Msg}}</pre>
<p><a href="/" style="color:#0a6c5a">← 返回看板</a></p>
</body></html>`))
