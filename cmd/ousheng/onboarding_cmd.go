package main

// Onboarding 命令集（v0.3.1）：把"姓名/角色/系统"三个填空包成命令。
// 设计原则：单人场景所有协作参数可默认；落盘仍是 canonical YAML + git commit。

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ousheng/internal/model"
	"ousheng/internal/state/gityaml"
	"ousheng/internal/workspace"
)

// mePath 是本机默认身份文件（.ousheng/me，gitignored——类 git config user.name）。
func mePath(dir string) string { return filepath.Join(dir, ".ousheng", "me") }

// cliActorIDRe 与 gityaml 的 actor id 规则一致（^[a-z0-9][a-z0-9-]*$）。
var cliActorIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// defaultActor 返回本机默认 actor id；未设置返回空串。
func defaultActor(dir string) string {
	b, err := os.ReadFile(mePath(dir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// resolveActor：显式 flag > .ousheng/me 默认身份。
func resolveActor(flagVal, dir string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if id := defaultActor(dir); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no acting actor: pass --actor or run `ousheng me <id>` once")
}

// slugify 生成 project-id 风格 slug（lowercase-hyphen）。
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := true // 去掉开头连字符
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// --- ousheng me ---

// cmdMe：`ousheng me <id> [--name N]` 声明自己（human，写 .ousheng/me 默认身份）；
// `ousheng me` 查看当前身份。已存在同名 actor 时只切换默认身份。
func cmdMe(args []string, stdout, stderr io.Writer) int {
	fs := newFS("me")
	name := fs.String("name", "", "display name (default: id)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	dir := fs.Dir()
	repo := gityaml.Open(dir)
	if fs.NArg() == 0 {
		id := defaultActor(dir)
		if id == "" {
			fmt.Fprintln(stderr, "no default identity — run: ousheng me <id> --name 你的名字")
			return 1
		}
		af, err := repo.GetActor(id)
		if err != nil {
			fmt.Fprintf(stderr, "default identity %q not in registry: %v\n", id, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s (%s, %s)\n", af.Actor.ID, af.Actor.Type, af.Actor.DisplayName)
		return 0
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng me <id> [--name N]")
		return 2
	}
	id := fs.Arg(0)
	if !cliActorIDRe.MatchString(id) {
		fmt.Fprintf(stderr, "invalid actor id %q (lowercase-hyphen)\n", id)
		return 2
	}
	display := *name
	if display == "" {
		display = id
	}
	if _, err := repo.GetActor(id); err != nil {
		af := model.ActorFile{SchemaVersion: 1, Actor: model.Actor{ID: id, Type: "human", DisplayName: display}}
		if err := repo.SaveActor(af, fmt.Sprintf("registry: me %s (%s)", id, display)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "actor %s created (%s)\n", id, display)
	} else {
		fmt.Fprintf(stdout, "actor %s exists — switching default identity\n", id)
	}
	if err := os.MkdirAll(filepath.Dir(mePath(dir)), 0o755); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := os.WriteFile(mePath(dir), []byte(id+"\n"), 0o644); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "default identity set to %s\n", id)
	return 0
}

// --- ousheng system add ---

// cmdSystemAdd：`ousheng system add <id> [--name N] [--parent P]`。
func cmdSystemAdd(args []string, stdout, stderr io.Writer) int {
	fs := newFS("system add")
	name := fs.String("name", "", "display name (default: id)")
	parent := fs.String("parent", "", "parent system id")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng system add <id> [--name N] [--parent P]")
		return 2
	}
	id := fs.Arg(0)
	repo := gityaml.Open(fs.Dir())
	sys, err := repo.ListSystems()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, s := range sys {
		if s.ID == id {
			fmt.Fprintf(stdout, "system %s exists\n", id)
			return 0
		}
	}
	n := *name
	if n == "" {
		n = id
	}
	sys = append(sys, model.System{ID: id, Name: n, Parent: *parent})
	if err := repo.SaveSystems(sys, "registry: system add "+id); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "system %s added (%s)\n", id, n)
	return 0
}

// --- ousheng todo ---

var todoIDRe = regexp.MustCompile(`^T-(\d+)$`)

// cmdTodo：`ousheng todo <title> [--system S]`——单人最短开工命令。
// 默认：id=T-xxx 自增 / type=task / actor=assignee=me / role=dev（缺则自动建）
// / accountable=唯一 human / assignment 缺则隐式建立。
func cmdTodo(args []string, stdout, stderr io.Writer) int {
	fs := newFS("todo")
	systemFlag := fs.String("system", "", "system id (default: 唯一系统)")
	version := fs.String("version", "", "target version")
	assignee := fs.String("assignee", "", "assignee (default: me)")
	role := fs.String("role", "", "acting role (default: dev)")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: ousheng todo <title> [--system S] [--version V]")
		return 2
	}
	title := fs.Arg(0)
	dir := fs.Dir()
	repo := gityaml.Open(dir)

	actor, err := resolveActor("", dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// 系统：唯一系统默认
	system := *systemFlag
	if system == "" {
		sys, err := repo.ListSystems()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(sys) == 1 {
			system = sys[0].ID
		} else if len(sys) == 0 {
			fmt.Fprintln(stderr, "no systems — run: ousheng system add <id>")
			return 1
		} else {
			ids := make([]string, len(sys))
			for i, s := range sys {
				ids[i] = s.ID
			}
			fmt.Fprintf(stderr, "multiple systems — pass --system: %s\n", strings.Join(ids, ", "))
			return 2
		}
	}
	// assignee：默认 me
	asg := *assignee
	if asg == "" {
		asg = actor
	}
	// role：默认 dev，缺则自动建
	roleID := *role
	if roleID == "" {
		roleID = "dev"
	}
	roles, err := repo.ListRoles()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	hasRole := false
	for _, r := range roles {
		if r.ID == roleID {
			hasRole = true
			break
		}
	}
	if !hasRole {
		roles = append(roles, model.Role{ID: roleID, Name: roleID})
		if err := repo.SaveRoles(roles, "registry: auto role "+roleID); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	// accountable：唯一 human 默认
	actors, err := repo.ListActors()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var humans []string
	for _, af := range actors {
		if af.Actor.Type == "human" {
			humans = append(humans, af.Actor.ID)
		}
	}
	if len(humans) != 1 {
		fmt.Fprintln(stderr, "accountable human ambiguous — set --accountable or declare exactly one human via ousheng me")
		return 2
	}
	// 自动 ID：T-001 递增
	items, err := repo.ListWorkItems()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	max := 0
	for _, w := range items {
		if m := todoIDRe.FindStringSubmatch(w.ID); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	id := fmt.Sprintf("T-%03d", max+1)
	w := model.WorkItem{
		SchemaVersion: 2, ID: id, Type: model.TypeTask, Title: title,
		System: system, TargetVersion: *version,
		Assignee: asg, ActingRole: roleID, AccountableHuman: humans[0],
	}
	svc := workspace.New(repo)
	created, err := svc.Create(w, actor)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// assignment 隐式建立：assignee 在该系统尚无 assignment 时补一行 executor
	if err := ensureAssignment(repo, asg, roleID, system); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "created %s (revision %d) → ousheng work update %s --status doing\n", created.ID, created.Revision, created.ID)
	return 0
}

// ensureAssignment：actor×system 无 assignment 时补 executor 行（可解释隐式关系建立）。
func ensureAssignment(repo *gityaml.Repo, actor, role, system string) error {
	list, err := repo.ListAssignments()
	if err != nil {
		return err
	}
	for _, a := range list {
		if a.Actor == actor && a.System == system {
			return nil
		}
	}
	list = append(list, model.Assignment{Actor: actor, Role: role, System: system, Responsibility: "executor", Active: true})
	return repo.SaveAssignments(list, "registry: implicit assignment "+actor+"×"+system)
}

// --- ousheng setup ---

// cmdSetup：交互式向导 = init + me + system add 的封装。
// stdin 可管道输入（每行一答，空行取默认值）。
func cmdSetup(args []string, stdout, stderr io.Writer) int {
	fs := newFS("setup")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	repo := gityaml.Open(dir)
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt, def string) string {
		if def != "" {
			fmt.Fprintf(stdout, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(stdout, "%s: ", prompt)
		}
		line, _ := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		return line
	}

	// 1. 立项（已初始化则跳过）
	if _, err := repo.GetProject(); err != nil {
		base := filepath.Base(absDir(dir))
		name := ask("项目名称", base)
		id := slugify(name)
		if id == "" {
			id = slugify(base)
		}
		if err := repo.InitWorkspace(projectModel(id, name)); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "✓ 立项 %s (%s)\n", name, id)
	}
	// 2. 我是谁
	meID := defaultActor(dir)
	if meID == "" {
		def := slugify(os.Getenv("USER"))
		id := ask("你的用户名（id）", def)
		if !cliActorIDRe.MatchString(id) {
			fmt.Fprintln(stderr, "invalid actor id (lowercase-hyphen)")
			return 2
		}
		name := ask("显示名", id)
		af := model.ActorFile{SchemaVersion: 1, Actor: model.Actor{ID: id, Type: "human", DisplayName: name}}
		if err := repo.SaveActor(af, "registry: me "+id); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := os.WriteFile(mePath(dir), []byte(id+"\n"), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		meID = id
		fmt.Fprintf(stdout, "✓ 身份 %s (%s)\n", id, name)
	} else {
		fmt.Fprintf(stdout, "✓ 身份已设置: %s\n", meID)
	}
	// 3. 系统
	sys, err := repo.ListSystems()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(sys) == 0 {
		list := ask("系统列表（逗号分隔，如 datax-ui, datax-server）", "")
		for i, raw := range strings.Split(list, ",") {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			sid := slugify(name)
			if sid == "" {
				sid = fmt.Sprintf("sys-%d", i+1) // 中文等无法 slug 的名字：id 自动编号，原名保留为显示名
			}
			sys = append(sys, model.System{ID: sid, Name: name})
		}
		if len(sys) == 0 {
			fmt.Fprintln(stderr, "至少一个系统")
			return 2
		}
		if err := repo.SaveSystems(sys, "registry: setup systems"); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		ids := make([]string, len(sys))
		for i, s := range sys {
			ids[i] = s.ID
		}
		sort.Strings(ids)
		fmt.Fprintf(stdout, "✓ 系统 %s\n", strings.Join(ids, ", "))
	} else {
		ids := make([]string, len(sys))
		for i, s := range sys {
			ids[i] = s.ID
		}
		fmt.Fprintf(stdout, "✓ 系统已存在: %s\n", strings.Join(ids, ", "))
	}
	fmt.Fprintln(stdout, "\n开工：")
	fmt.Fprintln(stdout, "  ousheng todo \"第一个任务\"          # 建任务（单系统免 --system）")
	fmt.Fprintln(stdout, "  ousheng work update T-001 --status doing   # 开工")
	fmt.Fprintln(stdout, "  ousheng view kanban                # 看板")
	fmt.Fprintln(stdout, "  ousheng converge                   # 收敛检查")
	return 0
}

func absDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}
