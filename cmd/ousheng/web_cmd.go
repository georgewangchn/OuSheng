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
	"encoding/json"
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
	"sort"
	"strconv"
	"strings"
	"time"

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
	mux := withNoStore(newWebMux(dir))
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

// withNoStore：实时看板禁缓存（PM 实用反馈：浏览器吃旧页看不到新面板）——
// 陈旧看板 = 基于旧状态拍板，缓存对决策面是正确性问题而非体验问题。
func withNoStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
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
	mux.HandleFunc("GET /design/{topic}", s.handleDesignView)
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

// 呈现层词汇表（数据层保持工程语义，呈现层中文——PM 不学术语，页面亮中文）。
var webStatusZh = map[string]string{
	"backlog":   "待排期",
	"ready":     "待开工",
	"doing":     "进行中",
	"blocked":   "卡住了",
	"testing":   "验收中",
	"done":      "已完成",
	"cancelled": "已作废",
}

var webTypeZh = map[string]string{
	"task":       "任务",
	"bug":        "缺陷",
	"feature":    "功能",
	"requirement": "需求",
	"test":       "测试",
	"deployment": "部署",
	"release":    "发布",
}

var webNextActionZh = map[string]string{
	"backlog":   "转待开工",
	"ready":     "开工",
	"doing":     "进行",
	"blocked":   "标记卡住",
	"testing":   "转验收",
	"done":      "完成",
	"cancelled": "作废",
}

// webTestingExtraZh：testing 下的两个高频去向给更贴切的话术。
var webActionAliasZh = map[string]map[string]string{
	"testing":  {"doing": "打回返工", "done": "验收通过"},
	"blocked":  {"doing": "解除卡住"},
	"done":     {"doing": "重开"},
	"cancelled": {"backlog": "重新排队"},
}

var webEvidenceZh = map[string]string{
	"git_commit":   "代码提交",
	"test_result":  "测试结果",
	"manual_check": "人工检查",
	"document":     "文档记录",
	"ci_run":       "CI 运行",
	"deployment":   "部署记录",
	"pull_request": "代码合入",
	"log":          "日志",
}

var webBasisZh = map[string]string{
	"manual":                  "人工判断",
	"implementation-checklist": "实现清单",
	"test-cases":              "测试用例",
	"subtasks":                "子任务",
	"story-points":            "故事点",
	"milestone":               "里程碑",
}

var webDesignStatusZh = map[string]string{
	"draft":      "草案",
	"agreed":     "已生效",
	"superseded": "已废止",
	"withdrawn":  "已撤回",
}

var webContractZh = map[string]string{
	"http": "接口", "cli": "CLI", "lib": "库", "event": "事件",
}

var webContractStatusZh = map[string]string{
	"proposed": "提案中", "agreed": "已定", "live": "生效中", "verified": "已验证", "deprecated": "已废弃",
}

// webNextActions：合法后继状态 + 中文动作标签（复用 model 公开的
// CanWorkTransition——不复制边表，一语义一实现）。
func webNextActions(cur model.WorkStatus) []webAction {
	var out []webAction
	for _, st := range []model.WorkStatus{model.StatusBacklog, model.StatusReady, model.StatusDoing, model.StatusBlocked, model.StatusTesting, model.StatusDone, model.StatusCancelled} {
		if st == cur || !model.CanWorkTransition(cur, st) {
			continue
		}
		label := webNextActionZh[string(st)]
		if alias, ok := webActionAliasZh[string(cur)][string(st)]; ok {
			label = alias
		}
		out = append(out, webAction{To: string(st), Label: label})
	}
	return out
}

type webAction struct {
	To    string
	Label string
}

type webCol struct {
	Name string // 英文原始值（测试锚点 + 工程对照）
	Zh   string
	Items []model.WorkItem
}

type webCardBadge struct {
	Text  string
	Class string
}

// webSuggest：「你的下一步」面板（档案 §24 追记：规则性关注聚合 = 传感器带宽，
// 非语义性建议——固定协议严重度排序，每行 = 事实 + 可用杠杆，无重要性话术）。
type webSuggest struct {
	Drafts    []model.DesignInfo // 等拍板：草案
	C2        []model.WorkItem   // 等拍板：破坏性变更未确认
	Blocked   []webBlockedCard   // 卡住了：时长 + 最新动态摘要
	Unowned   []model.WorkItem   // active 无主（converge BLOCKER 同源）
	PoolCount int                // ready/backlog 无主（待认领池）
	Overdue   []model.WorkItem   // 已超期
	DepCut    []model.WorkItem   // 依赖被作废
}

func (sg webSuggest) Count() int {
	return len(sg.Drafts) + len(sg.C2) + len(sg.Blocked) + len(sg.Unowned) + len(sg.Overdue) + len(sg.DepCut)
}

type webDoneCard struct {
	Item  model.WorkItem
	DoneOn string // 审计流推导，无记录为空
}

// webDoneSinceMap：一次扫审计流，取每单最新 done/cancelled 时戳（历史归档展示）。
func webDoneSinceMap(dir string) map[string]time.Time {
	out := map[string]time.Time{}
	root := filepath.Join(dir, ".ousheng", "activity")
	months, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	scan := func(line string) {
		var a struct {
			TS       string `json:"ts"`
			Action   string `json:"action"`
			WorkItem string `json:"work_item"`
			Detail   string `json:"detail"`
		}
		if json.Unmarshal([]byte(line), &a) != nil || a.Action != "status_changed" {
			return
		}
		if !strings.HasSuffix(a.Detail, "done") && !strings.HasSuffix(a.Detail, "cancelled") {
			return
		}
		t, err := time.Parse(time.RFC3339, a.TS)
		if err != nil {
			return
		}
		if prev, ok := out[a.WorkItem]; !ok || t.After(prev) {
			out[a.WorkItem] = t
		}
	}
	for _, m := range months {
		files, err := os.ReadDir(filepath.Join(root, m.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, m.Name(), f.Name()))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.Contains(line, "status_changed") {
					scan(line)
				}
			}
		}
	}
	return out
}

type webBlockedCard struct {
	Item model.WorkItem
	Days int // -1 = 审计流无记录（手改/迁移态）
	Note string
}

// webBlockedSince：从 activity 审计流推导卡住起点（最新 status_changed→blocked
// 时戳）。审计是事实源之一（非唯一），无记录返回 -1 天——诚实显示，不编造。
func webBlockedSince(dir, id string) (time.Time, bool) {
	root := filepath.Join(dir, ".ousheng", "activity")
	months, err := os.ReadDir(root)
	if err != nil {
		return time.Time{}, false
	}
	var latest time.Time
	for _, m := range months {
		files, err := os.ReadDir(filepath.Join(root, m.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, m.Name(), f.Name()))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(raw), "\n") {
				if !strings.Contains(line, "\""+id+"\"") {
					continue
				}
				var a struct {
					TS       string `json:"ts"`
					Action   string `json:"action"`
					WorkItem string `json:"work_item"`
					Detail   string `json:"detail"`
				}
				if json.Unmarshal([]byte(line), &a) != nil {
					continue
				}
				if a.WorkItem == id && a.Action == "status_changed" && strings.HasSuffix(a.Detail, "blocked") {
					if t, err := time.Parse(time.RFC3339, a.TS); err == nil && t.After(latest) {
						latest = t
					}
				}
			}
		}
	}
	return latest, !latest.IsZero()
}

// webExcerpt：最新动态摘要（最新证据 note 前 80 字），无则空。
func webExcerpt(w model.WorkItem) string {
	for i := len(w.Evidence) - 1; i >= 0; i-- {
		if n := strings.TrimSpace(w.Evidence[i].Note); n != "" {
			r := []rune(n)
			if len(r) > 80 {
				return string(r[:80]) + "…"
			}
			return n
		}
	}
	return ""
}

type boardData struct {
	Project     string
	Me          string
	SyncTime    string
	SyncOK      bool
	SyncLine    string
	Water       string
	CurSystem   string
	Systems     []string
	Drafts      []model.DesignInfo
	Agreed      []model.DesignInfo
	C2Pending   []model.WorkItem
	ConvergeOut string
	Cols        []webCol
	Done        []webDoneCard
	DoneCount   int
	AllActive   []model.DesignInfo // 草案+已生效（supersede 候选）
	Now         string
	Suggest     webSuggest
}

func webOverdue(w model.WorkItem, today string) bool {
	return w.DueOn != "" && w.DueOn < today && model.WorkItemOpen(w.Status)
}

func (s *webServer) handleBoard(w http.ResponseWriter, r *http.Request) {
	code, convOut, _ := s.exec("converge")
	_ = code // converge 面板纯展示（BLOCKER/WARNING 行文本自明）
	syncLine := s.sync()
	bd := boardData{
		Me:        s.me,
		SyncTime:  time.Now().Format("15:04:05"),
		SyncOK:    strings.Contains(syncLine, "sync收口: ok"),
		SyncLine:  syncLine,
		Water:     s.water(),
		CurSystem: r.URL.Query().Get("system"),
		ConvergeOut: strings.TrimSpace(convOut),
		Now:       time.Now().Format("2006-01-02"),
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
	bd.Suggest.Drafts = bd.Drafts
	bd.Suggest.C2 = bd.C2Pending
	bd.AllActive = append(bd.AllActive, bd.Drafts...)
	bd.AllActive = append(bd.AllActive, bd.Agreed...)
	// 建议面板推导（规则性：协议已知事实，无语义判断）
	statusOf := map[string]model.WorkStatus{}
	for _, it := range items {
		statusOf[it.ID] = it.Status
	}
	for _, it := range items {
		if !model.WorkItemOpen(it.Status) {
			continue
		}
		switch {
		case it.Status == model.StatusBlocked:
			days := -1
			if t, ok := webBlockedSince(s.dir, it.ID); ok {
				days = int(time.Since(t).Hours() / 24)
			}
			bd.Suggest.Blocked = append(bd.Suggest.Blocked, webBlockedCard{Item: it, Days: days, Note: webExcerpt(it)})
		case it.Assignee == "" && (it.Status == model.StatusDoing || it.Status == model.StatusTesting || it.Status == model.StatusBlocked):
			bd.Suggest.Unowned = append(bd.Suggest.Unowned, it)
		case it.Assignee == "" && (it.Status == model.StatusBacklog || it.Status == model.StatusReady):
			bd.Suggest.PoolCount++
		}
		if webOverdue(it, bd.Now) {
			bd.Suggest.Overdue = append(bd.Suggest.Overdue, it)
		}
		for _, dep := range it.DependsOn {
			if st, ok := statusOf[dep]; ok && st == model.StatusCancelled {
				bd.Suggest.DepCut = append(bd.Suggest.DepCut, it)
				break
			}
		}
	}
	for _, st := range []string{"backlog", "ready", "doing", "blocked", "testing"} {
		bd.Cols = append(bd.Cols, webCol{Name: st, Zh: webStatusZh[st], Items: byStatus[st]})
	}
	// 历史归档：完结日期审计流推导，近者在前
	doneAt := webDoneSinceMap(s.dir)
	var zero time.Time
	for _, it := range items {
		if model.WorkItemOpen(it.Status) {
			continue
		}
		dc := webDoneCard{Item: it}
		if t, ok := doneAt[it.ID]; ok {
			dc.DoneOn = t.Format("2006-01-02")
		}
		bd.Done = append(bd.Done, dc)
	}
	sort.Slice(bd.Done, func(i, j int) bool {
		ti, tj := zero, zero
		if bd.Done[i].DoneOn != "" {
			ti, _ = time.Parse("2006-01-02", bd.Done[i].DoneOn)
		}
		if bd.Done[j].DoneOn != "" {
			tj, _ = time.Parse("2006-01-02", bd.Done[j].DoneOn)
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return bd.Done[i].Item.ID < bd.Done[j].Item.ID
	})
	if err := webBoardTmpl.Execute(w, bd); err != nil {
		fmt.Fprintf(os.Stderr, "[web] board render error: %v\n", err)
	}
}

type detailData struct {
	D          *context.WorkItemDetail
	Me         string
	Actions    []webAction
	Actors     []webActorOpt
	StatusZh   string
	TypeZh     string
	Overdue    bool
	ContractZh string
}

type webActorOpt struct {
	ID   string
	Type string
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
	var actors []webActorOpt
	if afs, err := gityaml.Open(s.dir).ListActors(); err == nil {
		for _, af := range afs {
			actors = append(actors, webActorOpt{ID: af.Actor.ID, Type: string(af.Actor.Type)})
		}
	}
	dd := detailData{
		D:        d,
		Me:       s.me,
		Actions:  webNextActions(d.Status),
		Actors:   actors,
		StatusZh: webStatusZh[string(d.Status)],
		TypeZh:   webTypeZh[string(d.Type)],
		Overdue:  webOverdue(d.WorkItem, time.Now().Format("2006-01-02")),
	}
	if d.Contract != nil {
		dd.ContractZh = webContractZh[d.Contract.Kind] + " 契约 · " + webContractStatusZh[string(d.Contract.Status)]
		if d.Contract.Breaking {
			dd.ContractZh += " · 破坏性"
		}
	}
	if err := webDetailTmpl.Execute(w, dd); err != nil {
		fmt.Fprintf(os.Stderr, "[web] detail render error: %v\n", err)
	}
}

type webRound struct {
	N    int
	Body string
}

type designDetailData struct {
	Topic     string
	StatusZh  string
	D         model.DesignDoc
	Body      string
	Rounds    []webRound
	DecidedOn string
}

// handleDesignView：方案详情（正文 + 讨论轮次全览——PM 反馈「有方案但查看不了」）。
func (s *webServer) handleDesignView(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if topic == "" || topic == "." || topic == ".." || strings.ContainsAny(topic, "/\\") {
		s.fail(w, http.StatusBadRequest, "非法 topic", "topic 只能是设计目录名（防路径穿越）")
		return
	}
	repo := gityaml.Open(s.dir)
	d, body, err := repo.GetDesignRaw(topic)
	if err != nil {
		s.fail(w, http.StatusNotFound, "方案不存在", err.Error())
		return
	}
	dd := designDetailData{
		Topic:    topic,
		StatusZh: webDesignStatusZh[string(d.Status)],
		D:        d,
		Body:     string(body),
	}
	if t, err := time.Parse(time.RFC3339, d.DecidedAt); err == nil {
		dd.DecidedOn = t.Format("2006-01-02 15:04")
	}
	// 讨论轮次（round-N.md，新者在前）
	entries, err := os.ReadDir(filepath.Join(s.dir, ".ousheng", "designs", topic))
	if err == nil {
		for _, e := range entries {
			m := webRoundRe.FindStringSubmatch(e.Name())
			if m == nil {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(s.dir, ".ousheng", "designs", topic, e.Name()))
			if err != nil {
				continue
			}
			n, _ := strconv.Atoi(m[1])
			dd.Rounds = append(dd.Rounds, webRound{N: n, Body: string(raw)})
		}
		sort.Slice(dd.Rounds, func(i, j int) bool { return dd.Rounds[i].N > dd.Rounds[j].N })
	}
	if err := webDesignTmpl.Execute(w, dd); err != nil {
		fmt.Fprintf(os.Stderr, "[web] design render error: %v\n", err)
	}
}

var webRoundRe = regexp.MustCompile(`^round-(\d+)\.md$`)

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

var webBoardTmpl = template.Must(template.New("board").Funcs(webFuncs).Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>㸸绳看板 · {{.Project}}</title>
<style>
:root{--ink:#1f2328;--dim:#6a737d;--line:#e1e4e8;--bg:#f6f8fa;--card:#fff;
--acc:#1a7f64;--warn:#cf222e;--amber:#9a6700;--blue:#0969da;--purple:#8250df}
*{box-sizing:border-box}body{margin:0;font:14px/1.6 -apple-system,"PingFang SC","Microsoft YaHei",sans-serif;color:var(--ink);background:var(--bg)}
header{background:var(--card);border-bottom:1px solid var(--line);padding:12px 24px;position:sticky;top:0;z-index:5}
.h1row{display:flex;align-items:baseline;gap:14px;flex-wrap:wrap}
.h1row h1{font-size:17px;margin:0}
.chip{display:inline-block;font-size:12px;padding:1px 9px;border-radius:10px;background:#f0f2f4;color:var(--dim)}
.chip.me{background:#e6f2ee;color:var(--acc)}
.chip.ok{background:#e6f2ee;color:var(--acc)}
.chip.bad{background:#fdebe9;color:var(--warn)}
.chip.alert{background:var(--warn);color:#fff;font-weight:600;text-decoration:none}
nav.sys{margin-top:8px;display:flex;gap:6px;flex-wrap:wrap}
nav.sys a{font-size:12px;color:var(--dim);text-decoration:none;padding:2px 10px;border-radius:10px;border:1px solid transparent}
nav.sys a.on,nav.sys a:hover{color:var(--acc);border-color:var(--acc)}
main{padding:20px 24px 60px;max-width:1400px;margin:0 auto}
h2{font-size:13px;color:var(--dim);margin:26px 0 10px;letter-spacing:.02em}
.q{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 16px;margin-bottom:8px;display:flex;gap:14px;align-items:center;flex-wrap:wrap}
.q .who{font-weight:600}
.q .sub{font-size:12px;color:var(--dim)}
.qempty{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 16px;color:var(--acc);font-size:13px}
.q.grp{background:transparent;border:none;padding:2px 2px;margin:10px 0 4px}
.glvl{font-size:12.5px;font-weight:600;color:var(--dim);letter-spacing:.02em}
.board{display:grid;grid-template-columns:repeat(5,minmax(200px,1fr));gap:12px;overflow-x:auto}
.col{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:8px}
.col h3{font-size:13px;margin:4px 6px 10px;display:flex;align-items:center;gap:6px;font-weight:600}
.col .en{font-size:10px;color:var(--dim);font-weight:400;text-transform:lowercase}
.col .n{font-size:11px;background:#f0f2f4;color:var(--dim);border-radius:8px;padding:0 7px}
.col.s-backlog h3{color:var(--dim)} .col.s-ready h3{color:var(--blue)}
.col.s-doing h3{color:var(--acc)} .col.s-blocked h3{color:var(--warn)} .col.s-testing h3{color:var(--purple)}
.card{display:block;border:1px solid var(--line);border-left-width:3px;border-radius:8px;padding:8px 10px;margin-bottom:8px;text-decoration:none;color:var(--ink);background:var(--card)}
.card:hover{border-color:var(--acc);border-left-color:var(--acc)}
.card.p0{border-left-color:var(--warn)} .card.p1{border-left-color:var(--amber)}
.card .t{font-size:13px;line-height:1.45;margin-bottom:5px;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
.card .m{font-size:11px;color:var(--dim);display:flex;gap:5px;flex-wrap:wrap;align-items:center}
.b{display:inline-block;font-size:10.5px;border-radius:4px;padding:0 5px;background:#f0f2f4;color:var(--dim)}
.b.no{background:#fdebe9;color:var(--warn)}
.b.pr0{background:var(--warn);color:#fff;font-weight:600}
.b.pr1{background:#fff3d6;color:var(--amber)}
.b.prog{background:#e6f2ee;color:var(--acc)}
.b.od{background:var(--warn);color:#fff}
.card .id{font-family:ui-monospace,monospace;font-size:10.5px;color:var(--dim)}
form{display:inline}
button{font:inherit;font-size:12.5px;padding:4px 14px;border:1px solid var(--acc);background:var(--acc);color:#fff;border-radius:6px;cursor:pointer}
button:hover{opacity:.88}
button.ghost{background:var(--card);color:var(--acc)}
button.danger{background:var(--warn);border-color:var(--warn)}
button.sm{padding:2px 10px;font-size:12px}
input,select,textarea{font:inherit;font-size:13px;padding:5px 9px;border:1px solid var(--line);border-radius:6px;background:#fff}
input:focus,select:focus{outline:2px solid #b6d9cd;border-color:var(--acc)}
details{margin-top:8px}
summary{cursor:pointer;font-size:12.5px;color:var(--dim)}
pre{background:#f0f2f4;padding:12px;border-radius:8px;overflow:auto;font-size:12px;white-space:pre-wrap}
.newbox{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-top:10px}
.newbox .row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
.newbox label{font-size:12px;color:var(--dim)}
.hint{font-size:12px;color:var(--dim);margin-top:8px}
.sep{margin:30px 0 0;border-top:1px solid var(--line);padding-top:4px}
</style></head><body>
<header>
<div class="h1row">
<h1>㸸绳 · {{.Project}}</h1>
<span class="chip me">以 {{.Me}} 身份操作</span>
{{if .SyncOK}}<span class="chip ok">✓ 数据最新 {{.SyncTime}}</span>{{else}}<span class="chip bad">⚠ 同步异常：{{.SyncLine}}</span>{{end}}
<span class="chip">数据水位 {{.Water}}</span>
{{if gt .Suggest.Count 0}}<a class="chip alert" href="#queue">⚡ 需要你 {{.Suggest.Count}} 件</a>{{end}}
</div>
<nav class="sys"><a href="/"{{if not .CurSystem}} class="on"{{end}}>全部系统</a>{{range .Systems}}<a href="?system={{.}}"{{if eq . $.CurSystem}} class="on"{{end}}>{{.}}</a>{{end}}</nav>
</header>
<main>

<h2 id="queue">你的下一步 · 需要你的地方</h2>
{{if eq .Suggest.Count 0}}<div class="qempty">✓ 没有需要你出手的事项，一切顺畅。</div>{{end}}
{{if or .Suggest.Drafts .Suggest.C2}}
<div class="q grp"><span class="glvl">🔴 等你拍板</span></div>
{{end}}
{{range .Suggest.Drafts}}<div class="q">
<span class="who"><a href="/design/{{.Topic}}" style="color:inherit">方案「{{.Topic}}」</a></span>
<span class="sub">草案 · 发起人 {{.Design.Owner}}{{if .Design.Systems}} · 涉及 {{range .Design.Systems}}{{.}} {{end}}{{end}} · 评审完成后待你拍板生效</span>
<a href="/design/{{.Topic}}" style="font-size:12.5px;color:var(--acc)">看方案 →</a>
<form method="post" action="/design/{{.Topic}}/decide"><button class="sm">拍板生效</button></form>
<form method="post" action="/design/{{.Topic}}/withdraw"><button class="sm ghost">撤回草案</button></form>
</div>{{end}}
{{range .Suggest.C2}}<div class="q">
<span class="who"><a href="/work/{{.ID}}" style="color:inherit">{{.ID}}</a> {{.Title}}</span>
<span class="sub">包含破坏性变更（接口/契约不兼容），按规则须你人工确认后才能继续</span>
<form method="post" action="/work/{{.ID}}/ack"><button class="sm">确认通过</button></form>
<a href="/work/{{.ID}}" style="font-size:12.5px;color:var(--acc)">先看详情</a>
</div>{{end}}
{{if .Suggest.Blocked}}
<div class="q grp"><span class="glvl">🟠 卡住了的活</span></div>
{{end}}
{{range .Suggest.Blocked}}<div class="q">
<span class="who"><a href="/work/{{.Item.ID}}" style="color:inherit">{{.Item.ID}}</a> {{.Item.Title}}</span>
<span class="sub">{{if ge .Days 1}}已卡 {{.Days}} 天{{else if eq .Days 0}}今天刚卡住{{else}}已卡住（时间未知）{{end}}{{if .Note}} · 最新：{{.Note}}{{end}}</span>
<a href="/work/{{.Item.ID}}" style="font-size:12.5px;color:var(--acc)">看详情 →</a>
</div>{{end}}
{{if .Suggest.Unowned}}
<div class="q grp"><span class="glvl">🔴 进行中却没人负责</span></div>
{{end}}
{{range .Suggest.Unowned}}<div class="q">
<span class="who"><a href="/work/{{.ID}}" style="color:inherit">{{.ID}}</a> {{.Title}}</span>
<span class="sub">{{statusZh (printf "%s" .Status)}} · 无负责人——没人盯就会烂掉</span>
<a href="/work/{{.ID}}" style="font-size:12.5px;color:var(--acc)">去指派 →</a>
</div>{{end}}
{{if .Suggest.Overdue}}
<div class="q grp"><span class="glvl">🟡 已超期</span></div>
{{end}}
{{range .Suggest.Overdue}}<div class="q">
<span class="who"><a href="/work/{{.ID}}" style="color:inherit">{{.ID}}</a> {{.Title}}</span>
<span class="sub">承诺截止 {{.DueOn}}，已过线{{if .Assignee}} · 负责 {{.Assignee}}{{end}}</span>
<a href="/work/{{.ID}}" style="font-size:12.5px;color:var(--acc)">看详情 →</a>
</div>{{end}}
{{if .Suggest.DepCut}}
<div class="q grp"><span class="glvl">🟡 依赖被砍</span></div>
{{end}}
{{range .Suggest.DepCut}}<div class="q">
<span class="who"><a href="/work/{{.ID}}" style="color:inherit">{{.ID}}</a> {{.Title}}</span>
<span class="sub">它依赖的工单已被作废——需要重排或重新挂依赖</span>
<a href="/work/{{.ID}}" style="font-size:12.5px;color:var(--acc)">看详情 →</a>
</div>{{end}}
{{if gt .Suggest.PoolCount 0}}
<div class="q"><span class="who">待认领池</span><span class="sub">另有 {{.Suggest.PoolCount}} 张无主单在待排期/待开工（看板上有红标）</span></div>
{{end}}
{{if .Agreed}}<h2>已生效方案（如需废止，用新方案替代）</h2>
{{range .Agreed}}{{$cur := .Topic}}<div class="q">
<span class="who"><a href="/design/{{.Topic}}" style="color:inherit">「{{.Topic}}」</a></span><span class="sub">已生效 · {{.Design.DecidedBy}} 拍板</span>
<a href="/design/{{.Topic}}" style="font-size:12.5px;color:var(--acc)">看方案 →</a>
{{if gt (len $.AllActive) 1}}
<form method="post" action="/design/{{.Topic}}/supersede" style="display:inline-flex;gap:6px;align-items:center">
<select name="by">{{range $.AllActive}}{{if ne .Topic $cur}}{{if eq (printf "%s" .Design.Status) "draft"}}<option value="{{.Topic}}">{{.Topic}}（草案）</option>{{else}}<option value="{{.Topic}}">{{.Topic}}（已生效）</option>{{end}}{{end}}{{end}}</select>
<button class="sm ghost">废止并由它接替</button></form>
{{else}}<span class="sub">（暂无其他方案可接替——需先有新方案）</span>{{end}}
</div>{{end}}{{end}}

<h2>工作看板 <span style="font-weight:400">· 已完结 {{.DoneCount}} 张不在列</span></h2>
<div class="board">
{{range .Cols}}<div class="col s-{{.Name}}"><h3>{{.Zh}} <span class="en">{{.Name}}</span> <span class="n">{{len .Items}}</span></h3>
{{range .Items}}<a class="card{{if eq .Priority "P0"}} p0{{else if eq .Priority "P1"}} p1{{end}}" href="/work/{{.ID}}">
<div class="t">{{.Title}}</div>
<div class="m">
<span class="b">{{typeZh .Type}}</span>
{{if .System}}<span class="b">{{.System}}</span>{{end}}
{{if eq .Priority "P0"}}<span class="b pr0">P0</span>{{else if eq .Priority "P1"}}<span class="b pr1">P1</span>{{end}}
{{if .Progress}}<span class="b prog">{{printf "%.0f%%" (.Progress.Value | pct100)}}</span>{{end}}
{{if isOverdue . $.Now}}<span class="b od">已超期</span>{{end}}
{{if .Assignee}}<span>{{.Assignee}}</span>{{else}}<span class="b no">待认领</span>{{end}}
<span class="id">{{.ID}}</span>
</div>
</a>{{end}}
{{if not .Items}}<p style="color:var(--dim);font-size:12px;padding:4px 6px">暂无</p>{{end}}
</div>{{end}}
</div>

<h2>快速建单</h2>
<div class="newbox">
<form method="post" action="/work/create">
<div class="row">
<label>类型</label><select name="kind"><option value="task">任务</option><option value="bug">缺陷</option></select>
<label>标题</label><input name="title" placeholder="一句话说清要做什么" size="30" required>
<label>系统</label><select name="system">{{range .Systems}}<option value="{{.}}">{{.}}</option>{{end}}</select>
<label>优先级</label><select name="priority"><option value="">未定</option><option>P0</option><option>P1</option><option>P2</option><option>P3</option></select>
</div>
<div class="row" style="margin-top:8px">
<label>描述</label><input name="description" placeholder="背景 / 验收标准（可选）" size="52">
<button>创建</button>
</div>
</form>
<p class="hint">功能 / 需求类工作请先走方案评审（design 流程），不在快速建单之列。</p>
</div>

<details class="sep"><summary>历史归档 · 已完成/已作废 {{.DoneCount}} 张（点开浏览）</summary>
<div style="margin-top:8px">
{{range .Done}}<div class="q" style="padding:7px 14px">
<span class="id" style="font-family:ui-monospace,monospace;font-size:12px;color:var(--acc)"><a href="/work/{{.Item.ID}}">{{.Item.ID}}</a></span>
<a href="/work/{{.Item.ID}}" style="color:inherit;font-size:13px">{{.Item.Title}}</a>
<span class="sub">{{typeZh .Item.Type}} · {{.Item.System}}{{if .DoneOn}} · 完结于 {{.DoneOn}}{{else}} · 完结时间无记录{{end}}{{if eq (printf "%s" .Item.Status) "cancelled"}} · <span style="color:var(--warn)">作废</span>{{end}}</span>
</div>{{end}}
{{if not .Done}}<p style="color:var(--dim);font-size:13px">暂无已完结工单。</p>{{end}}
</div>
</details>

<details class="sep"><summary>收敛审计原始输出（converge）</summary><pre>{{.ConvergeOut}}</pre></details>
</main></body></html>`))

var webDetailTmpl = template.Must(template.New("detail").Funcs(webFuncs).Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.D.ID}} · 㸸绳</title>
<style>
:root{--ink:#1f2328;--dim:#6a737d;--line:#e1e4e8;--bg:#f6f8fa;--card:#fff;--acc:#1a7f64;--warn:#cf222e;--amber:#9a6700;--blue:#0969da;--purple:#8250df}
*{box-sizing:border-box}body{margin:0;font:14px/1.65 -apple-system,"PingFang SC","Microsoft YaHei",sans-serif;color:var(--ink);background:var(--bg)}
header{background:var(--card);border-bottom:1px solid var(--line);padding:10px 24px;font-size:13px}
a{color:var(--acc);text-decoration:none}
main{padding:20px 24px 60px;max-width:920px;margin:0 auto}
h1{font-size:17px;margin:0 0 6px;font-weight:600}
.meta{display:flex;gap:6px;flex-wrap:wrap;align-items:center;font-size:12px;color:var(--dim);margin-bottom:14px}
.st{font-weight:600;padding:1px 10px;border-radius:10px;color:#fff}
.st.backlog{background:var(--dim)} .st.ready{background:var(--blue)} .st.doing{background:var(--acc)}
.st.blocked{background:var(--warn)} .st.testing{background:var(--purple)} .st.done{background:#6a737d} .st.cancelled{background:#8b949e}
.b{display:inline-block;font-size:11px;border-radius:4px;padding:0 6px;background:#f0f2f4;color:var(--dim)}
.b.od{background:var(--warn);color:#fff}
.box{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 16px;margin:10px 0}
.box h3{font-size:13px;margin:2px 0 8px;color:var(--dim)}
.warnbox{background:#fff8f0;border:1px solid #e8b684;border-radius:10px;padding:12px 16px;margin:10px 0}
.warnbox .why{font-size:12.5px;color:var(--amber);margin-bottom:8px}
.oprow{display:flex;gap:8px;flex-wrap:wrap;align-items:center}
.oplbl{font-size:12.5px;color:var(--dim);margin-right:2px}
button{font:inherit;font-size:13px;padding:5px 16px;border:1px solid var(--acc);background:var(--acc);color:#fff;border-radius:6px;cursor:pointer}
button:hover{opacity:.88}
button.ghost{background:var(--card);color:var(--acc)}
button.danger{background:var(--warn);border-color:var(--warn)}
input,select{font:inherit;font-size:13px;padding:5px 9px;border:1px solid var(--line);border-radius:6px;background:#fff}
pre.desc{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px 16px;white-space:pre-wrap;font-family:inherit;font-size:13.5px;line-height:1.7}
.ev{border:1px solid var(--line);border-radius:8px;padding:10px 14px;margin:8px 0;font-size:13px;background:var(--card)}
.ev .note{color:var(--dim);font-size:12.5px;margin-top:3px;white-space:pre-wrap}
.ev .meta{margin:4px 0 0;font-size:11.5px}
.ok{color:var(--acc);font-weight:600} .fail{color:var(--warn);font-weight:600}
.bar{height:8px;background:#f0f2f4;border-radius:4px;overflow:hidden;margin:6px 0;max-width:320px}
.bar>div{height:100%;background:var(--acc)}
</style></head><body>
<header><a href="/">← 返回看板</a></header>
<main>
<h1>{{.D.ID}} · {{.D.Title}}</h1>
<div class="meta">
<span class="st {{.D.Status}}">{{.StatusZh}}</span>
<span class="b">{{.TypeZh}}</span>
{{if .D.System}}<span class="b">系统 {{.D.System}}</span>{{end}}
{{if .D.Assignee}}<span>负责：{{.D.Assignee}}</span>{{else}}<span class="b" style="color:var(--warn)">待认领</span>{{end}}
{{if .D.AccountableHuman}}<span>问责人：{{.D.AccountableHuman}}</span>{{end}}
{{if .D.DetectedBy}}<span>发现者：{{.D.DetectedBy}}</span>{{end}}
{{if .D.Priority}}<span class="b">优先级 {{.D.Priority}}</span>{{end}}
{{if .D.DueOn}}<span>截止 {{.D.DueOn}}{{if .Overdue}} <span class="b od">已超期</span>{{end}}</span>{{end}}
{{if .ContractZh}}<span class="b">{{.ContractZh}}</span>{{end}}
<span>版本 {{.D.Revision}}</span>
</div>

{{if and .D.Contract .D.Contract.Breaking (not .D.HumanAck)}}
<div class="warnbox">
<div class="why">⚠ 该工单包含破坏性变更（接口/契约不兼容，可能影响其他系统）——按规则需要你（human）确认后才能继续推进。</div>
<form method="post" action="/work/{{.D.ID}}/ack"><button>确认通过（以 {{.Me}} 身份）</button></form>
</div>
{{end}}

<div class="box"><h3>流转状态</h3>
<div class="oprow">
{{if .Actions}}<span class="oplbl">下一步：</span>
{{range .Actions}}<form method="post" action="/work/{{$.D.ID}}/status">
<input type="hidden" name="status" value="{{.To}}">
<input type="hidden" name="expect" value="{{$.D.Revision}}">
<button{{if eq .To "cancelled"}} class="danger"{{end}}>{{.Label}}</button>
</form>{{end}}
{{else}}<span class="oplbl">当前状态无可流转目标</span>{{end}}
</div>
</div>

<div class="box"><h3>指派</h3>
<div class="oprow">
{{if .D.Assignee}}<span class="oplbl">当前：{{.D.Assignee}} →</span>{{else}}<span class="oplbl">无人负责 →</span>{{end}}
<form method="post" action="/work/{{.D.ID}}/assign" style="display:inline-flex;gap:8px">
<input name="assignee" list="actors" placeholder="选择或输入 actor" required>
<datalist id="actors">{{range .Actors}}<option value="{{.ID}}">{{.Type}}</option>{{end}}</datalist>
<input type="hidden" name="expect" value="{{.D.Revision}}">
<button class="ghost">指派</button>
</form>
</div>
</div>

{{if .D.Description}}<div class="box"><h3>描述与验收标准</h3><pre class="desc" style="border:none;padding:0;background:transparent">{{.D.Description}}</pre></div>{{end}}
{{if .D.Progress}}<div class="box"><h3>进度（{{.D.Progress.Actor}} 汇报 · 依据：{{basisZh .D.Progress.Basis}}）</h3>
<div class="bar"><div style="width:{{printf "%.0f" (mul100 .D.Progress.Value)}}%"></div></div>
<span style="font-size:12.5px;color:var(--dim)">{{printf "%.0f" (mul100 .D.Progress.Value)}}% · 汇报于 {{.D.Progress.ReportedAt}}</span>
</div>{{end}}
{{if .D.Deps}}<div class="box"><h3>依赖的工单</h3>
{{range .D.Deps}}<div class="ev"><a href="/work/{{.ID}}">{{.ID}}</a> · {{statusZh .Status}} · {{.Title}}</div>{{end}}
</div>{{end}}
{{if .D.Designs}}<div class="box"><h3>关联方案</h3>
{{range .D.Designs}}<div class="ev">「{{.Topic}}」 · {{if eq .Status "agreed"}}已生效（{{.DecidedBy}} 拍板）{{else if eq .Status "draft"}}草案（评审中）{{else}}{{.Status}}{{end}}</div>{{end}}
</div>{{end}}
{{if .D.Evidence}}<div class="box"><h3>证据链（从旧到新）</h3>
{{range .D.Evidence}}<div class="ev">
<b>{{evidenceZh .Type}}</b> · {{if eq .Result "passed"}}<span class="ok">✓ 通过</span>{{else if eq .Result "failed"}}<span class="fail">✗ 失败</span>{{else}}{{.Result}}{{end}} · <span style="font-family:ui-monospace,monospace;font-size:12px">{{.Locator}}</span>
{{if .Note}}<div class="note">{{.Note}}</div>{{end}}
<div class="meta">{{.Source}} · {{.ObservedAt}}</div>
</div>{{end}}
</div>{{end}}
{{if .D.DependsOn}}<div class="box"><h3>depends_on</h3><p style="font-size:12px;color:var(--dim);margin:0">{{range .D.DependsOn}}<span class="b">{{.}}</span> {{end}}</p></div>{{end}}
</main></body></html>`))

var webDesignTmpl = template.Must(template.New("design").Funcs(webFuncs).Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8">
<title>方案 {{.Topic}} · 㸸绳</title>
<style>
:root{--ink:#1f2328;--dim:#6a737d;--line:#e1e4e8;--bg:#f6f8fa;--card:#fff;--acc:#1a7f64;--warn:#cf222e}
*{box-sizing:border-box}body{margin:0;font:14px/1.65 -apple-system,"PingFang SC","Microsoft YaHei",sans-serif;color:var(--ink);background:var(--bg)}
header{background:var(--card);border-bottom:1px solid var(--line);padding:10px 24px;font-size:13px}
a{color:var(--acc);text-decoration:none}
main{padding:20px 24px 60px;max-width:920px;margin:0 auto}
h1{font-size:17px;margin:0 0 6px}
.meta{display:flex;gap:6px;flex-wrap:wrap;align-items:center;font-size:12px;color:var(--dim);margin-bottom:14px}
.st{font-weight:600;padding:1px 10px;border-radius:10px;color:#fff}
.st.draft{background:#0969da} .st.agreed{background:var(--acc)} .st.superseded{background:#8b949e} .st.withdrawn{background:#8b949e}
.b{display:inline-block;font-size:11px;border-radius:4px;padding:0 6px;background:#f0f2f4;color:var(--dim)}
.box{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:12px 16px;margin:10px 0}
.box h3{font-size:13px;margin:2px 0 8px;color:var(--dim)}
pre{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:14px 16px;white-space:pre-wrap;font-size:13.5px;line-height:1.7;overflow:auto}
details{margin:8px 0}
summary{cursor:pointer;font-size:13px;color:var(--acc)}
</style></head><body>
<header><a href="/">← 返回看板</a></header>
<main>
<h1>方案「{{.Topic}}」</h1>
<div class="meta">
<span class="st {{.D.Status}}">{{.StatusZh}}</span>
<span>发起人 {{.D.Owner}}</span>
{{if .D.Systems}}<span>涉及 {{range .D.Systems}}<span class="b">{{.}}</span> {{end}}{{end}}
{{if .D.RelatedItems}}<span>关联工单 {{range .D.RelatedItems}}<span class="b">{{.}}</span> {{end}}{{end}}
{{if .D.DecidedBy}}<span>{{.D.DecidedBy}} 拍板{{if .DecidedOn}}于 {{.DecidedOn}}{{end}}</span>{{end}}
{{if .D.SupersededBy}}<span>已由「{{.D.SupersededBy}}」接替</span>{{end}}
</div>
{{if .Body}}<div class="box"><h3>方案正文</h3><pre style="border:none;padding:0;background:transparent">{{.Body}}</pre></div>{{end}}
{{if .Rounds}}<div class="box"><h3>讨论轮次（新者在前）</h3>
{{range .Rounds}}<details><summary>第 {{.N}} 轮</summary><pre>{{.Body}}</pre></details>{{end}}
</div>{{end}}
{{if not .Body}}{{if not .Rounds}}<div class="box"><p style="color:var(--dim);margin:4px">（方案尚未写正文，也没有讨论轮次——内容生成不经绳，由人和 agent 直接写文件。）</p></div>{{end}}{{end}}
</main></body></html>`))

var webErrorTmpl = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="zh"><head><meta charset="utf-8"><title>操作未成功</title></head>
<body style="font:14px/1.65 -apple-system,'PingFang SC',sans-serif;padding:48px;background:#f6f8fa">
<div style="max-width:640px;margin:0 auto">
<h2 style="color:#cf222e;font-size:16px">{{.Title}}</h2>
<pre style="background:#fff;border:1px solid #e1e4e8;padding:14px;border-radius:10px;white-space:pre-wrap;font-size:13px">{{.Msg}}</pre>
<p style="font-size:13px;color:#6a737d">操作被规则拒绝时这里会如实展示原因（不静默）。通常是版本冲突（别人先改了）——返回看板刷新后重试。</p>
<p><a href="/" style="color:#1a7f64">← 返回看板</a></p>
</div>
</body></html>`))

// webFuncs：模板函数（呈现层中文词汇表，数据层不动）。Parse 前注册。
var webFuncs = template.FuncMap{
	"typeZh":     func(t model.WorkItemType) string { return webTypeZh[string(t)] },
	"statusZh":   func(s string) string { return webStatusZh[s] },
	"isOverdue":  func(w model.WorkItem, today string) bool { return webOverdue(w, today) },
	"pct100":     func(v float64) float64 { return v * 100 },
	"mul100":     func(v float64) float64 { return v * 100 },
	"evidenceZh": func(t model.EvidenceType) string { return webEvidenceZh[string(t)] },
	"basisZh":    func(b model.ProgressBasis) string { return webBasisZh[string(b)] },
}
