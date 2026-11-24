package govern

// entry_test.go —— 入口覆盖派生审计（档案 §58 性质 P2「覆盖由构造得出」）。
//
// 实锤（§56 F4 类）：§55 的入口自检体系按清单盖了 plan / ignite / sync / doctor /
// web chip，漏了晨检真正跑的 status / report。清单是人抄的，下一轮还会漏。
// 现在默认接（fs.Dir() 首次调用即自检），例外显式登记；本测试**从代码派生**入口
// 全集（run() 派发树 → 叶子），逐一核对：叶子要么走了默认（且钩子可达），要么
// 在例外表里登记过并写了理由；例外表里的死条目同样报红。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEntryFreshnessCoverage(t *testing.T) {
	root := filepath.Join("..", "..", "cmd", "ousheng")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, root, func(fi fs.FileInfo) bool {
		return strings.HasSuffix(fi.Name(), ".go") && !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg := pkgs["main"]
	if pkg == nil {
		t.Fatal("cmd/ousheng 包解析失败")
	}
	funcs := map[string]*ast.FuncDecl{}
	handlers := map[string]bool{} // 处理函数：首参 args []string + stdout/stderr io.Writer
	var files []*ast.File
	for _, f := range pkg.Files {
		files = append(files, f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Body == nil {
				continue
			}
			funcs[fd.Name.Name] = fd
			if isHandler(fd) {
				handlers[fd.Name.Name] = true
			}
		}
	}
	exempt := parseExemptions(t, files)
	// 处理函数引用计数——孤儿 = 不可达入口或死码（派发器 run 除外）。
	refs := map[string]int{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && handlers[id.Name] {
					refs[id.Name]++
				}
			}
			return true
		})
	}
	for name := range handlers {
		if name != "run" && refs[name] == 0 {
			t.Errorf("处理函数无人调用（不可达入口或死码）：%s", name)
		}
	}

	var problems []string
	matched := map[string]bool{}
	// auditLeaf：path = 派发路径；subName = 动态 flag 集名（"pm " + args[0]）对应的 case 字面量。
	auditLeaf := func(path string, sites []fsSite, usesDir bool, subName string) {
		if len(sites) == 0 {
			if reason, ok := exempt[path]; !ok || reason == "" {
				problems = append(problems, path+"：入口不建 flag 集——若它不读工作区，在 entryFreshnessExempt 登记并写理由")
			} else {
				matched[path] = true
			}
			return
		}
		for _, s := range sites {
			name := s.name
			if s.dynamic {
				if subName == "" {
					problems = append(problems, path+"：动态 flag 集名（"+s.name+"…）无法定位到子命令——审计假设被打破")
					continue
				}
				name += subName
			}
			if reason, ok := exempt[name]; ok {
				if reason == "" {
					problems = append(problems, name+"：例外必须写理由")
				}
				matched[name] = true
				continue
			}
			// 走默认：钩子挂在 fs.Dir()——叶子必须调它，否则「默认接」是空话。
			if !usesDir {
				problems = append(problems, name+"：走默认板龄自检却不调 fs.Dir()——钩子不可达")
			}
		}
	}
	var walk func(fn, path string, depth int)
	walk = func(fn, path string, depth int) {
		if depth > 6 {
			problems = append(problems, path+"：派发树过深——审计假设被打破")
			return
		}
		fd := funcs[fn]
		if fd == nil {
			problems = append(problems, "派发器调用了不存在的函数 "+fn)
			return
		}
		sites := newFSSites(fset, fd.Body)
		fnUsesDir := callsDir(fd.Body)
		subs := switchesOnArgs0(fd.Body)
		if len(subs) == 0 {
			auditLeaf(path, sites, fnUsesDir, "")
			return
		}
		for _, sw := range subs {
			for _, st := range sw.Body.List {
				cc, ok := st.(*ast.CaseClause)
				if !ok {
					continue
				}
				sub, ok := caseString(cc)
				if !ok {
					continue
				}
				p := strings.TrimSpace(path + " " + sub)
				caseBody := (&ast.BlockStmt{List: cc.Body})
				callees := handlerCallsIn(caseBody, handlers)
				inline := newFSSites(fset, caseBody)
				switch {
				case len(inline) > 0:
					auditLeaf(p, inline, callsDir(caseBody), "")
				case len(callees) == 0:
					auditLeaf(p, sites, fnUsesDir, sub)
				}
				for _, c := range callees {
					walk(c, p, depth+1)
				}
			}
		}
	}
	walk("run", "", 0)

	for k, reason := range exempt {
		if !matched[k] {
			t.Errorf("entryFreshnessExempt 死条目或无法在代码里定位：%q（%s）", k, reason)
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
}

type fsSite struct {
	name    string // 字面量名，或动态名的前缀（如 "pm "）
	dynamic bool
}

// newFSSites：函数体 / case 体内的 newFS("…") 调用点。
func newFSSites(fset *token.FileSet, body ast.Node) []fsSite {
	var out []fsSite
	if body == nil {
		return out
	}
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 {
			return true
		}
		id, ok := c.Fun.(*ast.Ident)
		if !ok || id.Name != "newFS" {
			return true
		}
		out = append(out, nameOf(fset, c.Args[0]))
		return true
	})
	return out
}

// nameOf：字面量 → 名；`"前缀" + args[0]` → 前缀（dynamic）。
func nameOf(fset *token.FileSet, e ast.Expr) fsSite {
	switch x := e.(type) {
	case *ast.BasicLit:
		s, _ := strconv.Unquote(x.Value)
		return fsSite{name: s}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			s := nameOf(fset, x.X)
			s.dynamic = true
			return s
		}
	}
	return fsSite{name: fset.Position(e.Pos()).String() + ":无法解析的 flag 集名", dynamic: true}
}

// callsDir：是否调用 `x.Dir()`（排除 filepath.Dir / path.Dir）。
func callsDir(body ast.Node) bool {
	found := false
	if body == nil {
		return false
	}
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Dir" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name != "filepath" && id.Name != "path" {
			found = true
		}
		return true
	})
	return found
}

// switchesOnArgs0：`switch args[0] {…}` 语句。
func switchesOnArgs0(body ast.Node) []*ast.SwitchStmt {
	var out []*ast.SwitchStmt
	ast.Inspect(body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		idx, ok := sw.Tag.(*ast.IndexExpr)
		if !ok {
			return true
		}
		x, ok := idx.X.(*ast.Ident)
		lit, ok2 := idx.Index.(*ast.BasicLit)
		if ok && ok2 && x.Name == "args" && lit.Value == "0" {
			out = append(out, sw)
		}
		return true
	})
	return out
}

func caseString(cc *ast.CaseClause) (string, bool) {
	if len(cc.List) != 1 {
		return "", false
	}
	lit, ok := cc.List[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, _ := strconv.Unquote(lit.Value)
	return s, true
}

// isHandler：处理函数签名——首参 `args []string`，其后有 io.Writer 形参（命令入口的
// 既定形态：cmdX(args []string, stdout, stderr io.Writer) int；命名不限 cmd 前缀）。
func isHandler(fd *ast.FuncDecl) bool {
	ps := fd.Type.Params.List
	if len(ps) < 2 { // [args []string] + [stdout, stderr io.Writer]（两个 writer 可共用一个 Field）
		return false
	}
	first := ps[0]
	if len(first.Names) == 0 || first.Names[0].Name != "args" {
		return false
	}
	arr, ok := first.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	if elt, ok := arr.Elt.(*ast.Ident); !ok || elt.Name != "string" {
		return false
	}
	writers := 0
	for _, p := range ps[1:] {
		if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Writer" {
			writers += len(p.Names)
		}
	}
	return writers >= 2
}

// handlerCallsIn：case 体内调用的处理函数（去重）。
func handlerCallsIn(body ast.Node, handlers map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := c.Fun.(*ast.Ident); ok && handlers[id.Name] && !seen[id.Name] {
			seen[id.Name] = true
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// parseExemptions：从源码读 entryFreshnessExempt 表（键 → 理由）。
func parseExemptions(t *testing.T, files []*ast.File) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if n.Name != "entryFreshnessExempt" || i >= len(vs.Values) {
						continue
					}
					cl, ok := vs.Values[i].(*ast.CompositeLit)
					if !ok {
						continue
					}
					for _, elt := range cl.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						k, _ := strconv.Unquote(kv.Key.(*ast.BasicLit).Value)
						v := ""
						if lit, ok := kv.Value.(*ast.BasicLit); ok {
							v, _ = strconv.Unquote(lit.Value)
						}
						out[k] = v
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("读不到 entryFreshnessExempt 表——它搬家了吗？审计随之更新")
	}
	return out
}
