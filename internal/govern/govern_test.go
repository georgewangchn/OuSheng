package govern

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// 合成用例：每条约束一个正例一个反例——校验器自己也得被锁（E6：锁要锁行为）。
func TestLedgerRules(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	hasTest := func(n string) bool { return n == "TestReal" }
	exists := func(p string) bool { return p == ".githooks/pre-push" }
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	cases := []struct {
		name string
		h    rulingHeader
		want []string // 每项须命中一条违规；空 = 必须合规
	}{
		{"缺 enforcement", rulingHeader{Section: 57}, []string{"缺 enforcement"}},
		{"未知类型", rulingHeader{Section: 57, Enforcement: []string{"vibes"}}, []string{"未知类型 vibes"}},
		{"lock 名不存在", rulingHeader{Section: 57, Enforcement: []string{"lock"}, Locks: []string{"TestGhost"}}, []string{"TestGhost 不是仓里的测试函数"}},
		{"声明 lock 未列", rulingHeader{Section: 57, Enforcement: []string{"lock"}}, []string{"未列 locks"}},
		{"gate 无锁", rulingHeader{Section: 57, Enforcement: []string{"gate"}}, []string{"却没有 locks"}},
		{"default 无锁", rulingHeader{Section: 57, Enforcement: []string{"default"}}, []string{"却没有 locks"}},
		{"hook 路径不存在", rulingHeader{Section: 57, Enforcement: []string{"lock", "hook"}, Locks: []string{"TestReal"}, Hook: []string{".githooks/nope"}}, []string{".githooks/nope 不存在"}},
		{"meter 未写量尺", rulingHeader{Section: 57, Enforcement: []string{"lock", "meter"}, Locks: []string{"TestReal"}}, []string{"未写 meter: 量尺"}},
		{"纪律缺量尺缺复审", rulingHeader{Section: 57, Enforcement: []string{"discipline"}}, []string{"必须配 meter", "必须带 review"}},
		{"纪律复审日太远", rulingHeader{Section: 57, Enforcement: []string{"discipline"}, Meter: "ousheng pm status", Review: day(120)}, []string{"一脚踢远"}},
		{"纪律到期", rulingHeader{Section: 58, Enforcement: []string{"discipline"}, Meter: "ousheng pm status", Review: day(-2)}, []string{"§58 纪律复审到期"}},
		{"纪律复审日格式错", rulingHeader{Section: 57, Enforcement: []string{"discipline"}, Meter: "x", Review: "2026/12/01"}, []string{"不是 YYYY-MM-DD"}},
		{"纪律复审日=今天 不算过期", rulingHeader{Section: 57, Enforcement: []string{"discipline"}, Meter: "x", Review: day(0)}, nil},
		{"全类型合规", rulingHeader{Section: 57, Enforcement: []string{"gate", "default", "meter", "lock", "hook", "discipline"},
			Locks: []string{"TestReal", "TestReal"}, Hook: []string{".githooks/pre-push"}, Meter: "cruise status", Review: day(30)}, nil},
	}
	for _, c := range cases {
		got := validateHeader(c.h, now, hasTest, exists)
		for _, w := range c.want {
			hit := false
			for _, g := range got {
				if strings.Contains(g, w) {
					hit = true
				}
			}
			if !hit {
				t.Errorf("%s: 应报含 %q 的违规，实得 %v", c.name, w, got)
			}
		}
		if len(c.want) == 0 && len(got) != 0 {
			t.Errorf("%s: 应合规，实得 %v", c.name, got)
		}
	}
}

// 解析：≥56 的节必须有头；缺头被点名；旧节不追溯。
func TestLedgerParse(t *testing.T) {
	archive := "## 55. 旧节无头（不追溯）\n正文\n" +
		"## 56. 有头\n<!-- ruling:\nenforcement: gate, lock\nlocks: TestA, TestB\nmeter: cruise status\nreview: 2026-12-08\n-->\n正文\n" +
		"## 57. 缺头\n正文\n" +
		"### 57.1 三级标题不算节\n"
	hs, missing := parseLedger(archive)
	if len(missing) != 1 || missing[0] != 57 {
		t.Fatalf("缺头应恰为 §57: %v", missing)
	}
	if len(hs) != 1 || hs[0].Section != 56 {
		t.Fatalf("应解析出 §56: %+v", hs)
	}
	h := hs[0]
	if strings.Join(h.Enforcement, ",") != "gate,lock" || strings.Join(h.Locks, ",") != "TestA,TestB" || h.Meter != "cruise status" || h.Review != "2026-12-08" {
		t.Fatalf("字段解析错: %+v", h)
	}
}

// TestRulingLedger（真档案）：这是带牙的那条——纪律类复审到期，本测试即红，
// 直到有人测量量尺、写回读数并续期（或转机制）。全仓只有「提交前跑 go test」
// 这一条纪律撑着它；再往上没有元规则（§57 反事实 5）。
func TestRulingLedger(t *testing.T) {
	root := filepath.Join("..", "..")
	b, err := os.ReadFile(filepath.Join(root, "docs", "设计裁决与推演记录.md"))
	if err != nil {
		t.Fatal(err)
	}
	headers, missing := parseLedger(string(b))
	for _, m := range missing {
		t.Errorf("§%d 缺 ruling 头（<!-- ruling: enforcement: … -->，档案 §57）——裁决必须声明执行类型；纯文字裁决=纪律=会复发", m)
	}
	tests := indexTestFuncs(root)
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(root, p))
		return err == nil
	}
	for _, h := range headers {
		for _, e := range validateHeader(h, time.Now(), func(n string) bool { return tests[n] }, exists) {
			t.Error(e)
		}
	}
}

// --- 读面吞错 AST 锁 ---

// watchedReads：读面 API（清单类）。它们的错误被吞 = 读数静默归零（§56 实锤：
// 审计流一行坏行 → 全舰队「交付 0 单」42h）。
var watchedReads = map[string]bool{
	"ListActivity": true, "ListActors": true, "ListWorkItems": true, "ListDesigns": true,
	"ListHandover": true, "ListCredentials": true,
}

func watchedCall(e ast.Expr) *ast.SelectorExpr {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) > 1 {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !watchedReads[sel.Sel.Name] {
		return nil
	}
	return sel
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// swallowViolations：两种吞错形态——①`x, _ := R()`；②`if x, err := R(); err == nil {…}`
// 无 else（失败分支什么都不说）。显式处理（有 else、或有 if err != nil）即合规。
func swallowViolations(fset *token.FileSet, path string, src []byte) []string {
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var out []string
	pos := func(n ast.Node) string { return fset.Position(n.Pos()).String() }
	ast.Inspect(f, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			// 形态 ①：`x, _ := R()`——AST 里一个多返回值调用对两个左值。
			if len(s.Rhs) == 1 && len(s.Lhs) >= 2 {
				if sel := watchedCall(s.Rhs[0]); sel != nil {
					if id, ok := s.Lhs[len(s.Lhs)-1].(*ast.Ident); ok && id.Name == "_" {
						out = append(out, pos(s)+": "+sel.Sel.Name+"() 的错误被 `_` 吞——读面降级必须显式（档案 §56）")
					}
				}
				return true
			}
			// 形态 ①'：等长多元赋值里的逐项吞错。
			for i, rhs := range s.Rhs {
				sel := watchedCall(rhs)
				if sel == nil || len(s.Lhs) != len(s.Rhs) {
					continue
				}
				if id, ok := s.Lhs[i].(*ast.Ident); ok && id.Name == "_" {
					out = append(out, pos(s)+": "+sel.Sel.Name+"() 的错误被 `_` 吞——读面降级必须显式（档案 §56）")
				}
			}
		case *ast.IfStmt:
			if s.Init == nil || s.Else != nil {
				return true
			}
			as, ok := s.Init.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 2 {
				return true
			}
			sel := watchedCall(as.Rhs[0])
			if sel == nil {
				return true
			}
			errName := identName(as.Lhs[1])
			be, ok := s.Cond.(*ast.BinaryExpr)
			if !ok || be.Op != token.EQL || !strings.Contains(errName, "err") {
				return true
			}
			if identName(be.X) == errName && identName(be.Y) == "nil" {
				out = append(out, pos(s)+": "+sel.Sel.Name+"() 失败分支无 else——错误被静默吞（读面降级必须响亮）")
			}
		}
		return true
	})
	return out
}

// 校验器自锁：三种形态各一例——两种吞错必须被抓，显式处理必须放行。
func TestSwallowDetectorSelfLock(t *testing.T) {
	src := `package x
func a(r R) {
	x1, _ := r.ListActivity()
	_ = x1
	if x2, err := r.ListActors(); err == nil {
		_ = x2
	}
	if x3, err := r.ListDesigns(); err == nil {
		_ = x3
	} else {
		warn(err)
	}
	x4, aerr := r.ListHandover()
	if aerr != nil {
		warn(aerr)
	}
	_ = x4
	y, _ := r.Unrelated()
	_ = y
}
`
	fset := token.NewFileSet()
	got := swallowViolations(fset, "x.go", []byte(src))
	if len(got) != 2 {
		t.Fatalf("应恰抓到 2 处（_ 吞 + 无 else），实得 %d:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, g := range got {
		if !strings.Contains(g, "ListActivity") && !strings.Contains(g, "ListActors") {
			t.Fatalf("抓错了对象（显式处理与无关调用必须放行）: %s", g)
		}
	}
}

// TestNoSwallowedReadErrors（真仓）：产品代码里不得有读面错误的静默吞。
func TestNoSwallowedReadErrors(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var bad []string
	walkGoFiles(root, func(path string, isTest bool, src []byte) {
		if isTest {
			return
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return
		}
		if !strings.HasPrefix(rel, "cmd/") && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "adapters/") {
			return
		}
		bad = append(bad, swallowViolations(fset, rel, src)...)
	})
	for _, b := range bad {
		t.Error(b)
	}
}

// TestShellVarBraces：bash 3.2 陷阱的通用护栏——`$var` 后紧跟全角字符时变量名
// 不截断（`$log）` → 查找名为 `log）——改…` 的变量 → set -u 崩）。档案 §43④ 在
// cruise-loop.sh 踩过 5 处、靠 throwaway 模拟抓回；§57 写 pre-push/check.sh 又踩
// 3 处、靠 TestPrePushHook 真跑抓回。两次实锤 → 收成对全部 shell 资产的词法锁
// （纯词法陷阱用词法锁，不算 E6 反例）。
func TestShellVarBraces(t *testing.T) {
	root := filepath.Join("..", "..")
	targets := []string{filepath.Join(root, "adapters", "opencode", "cruise-loop.sh"), filepath.Join(root, ".githooks", "pre-push")}
	scripts, _ := filepath.Glob(filepath.Join(root, "scripts", "*.sh"))
	targets = append(targets, scripts...)
	re := regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*[^\x00-\x7f]`)
	for _, p := range targets {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if loc := re.FindStringIndex(line); loc != nil && !strings.Contains(line, "bash 3.2") {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("%s:%d: `$var` 后紧跟全角字符（bash 3.2 变量名不截断）——改 ${var}：%s", rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}
