// ousheng 是 v0.3 Engineering Context Runtime CLI（v0.3 §37）。
//
// 保留 v1 兼容：board 二进制与 cards/ 工作流不受影响。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ousheng/internal/context"
	"ousheng/internal/converge"
	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/projection"
	"ousheng/internal/state/gityaml"
)

const cliVersion = "0.3.1"

// 构建元数据：车队部署时用 -ldflags 注入（-X main.buildCommit=... -X main.buildDate=...），
// 验收「这台机器跑的是哪个提交」；不带 ldflags 的本地 go build 显示 dev。
var (
	buildCommit = "dev"
	buildDate   = "dev"
)

func versionString() string {
	return fmt.Sprintf("ousheng %s (commit %s, built %s)", cliVersion, buildCommit, buildDate)
}

const usage = `ousheng — 轻量工程上下文运行时（Engineering Context Runtime）

快速开始（单人 5 分钟）:
  ousheng setup                                    交互式向导（立项+身份+系统，一步到位）
  ousheng init myproj && cd myproj                 或手动立项（名字默认=目录名）
  ousheng me george --name George                  我是谁（此后所有命令免 --actor）
  ousheng system add datax-ui                      有哪些系统
  ousheng todo "第一个任务"                          开始干活（单系统免 --system）
  ousheng work update T-001 --status doing         开工
  ousheng view kanban                              看板

多机加入（时机零）:
  ousheng join                                     首次上绳协议（新机/新窗口/re-clone 灾难恢复）
  ousheng   adapter install [--dir <code-repo>]       挂/升级 opencode 适配器（内嵌资产，幂等）
  doctor [--dir <代码仓>]                   部署标准自检（机器/座位/工作区，FAIL 带修复命令）

用法: ousheng <command> [args]

workspace:
  setup                                            交互式向导 = init + me + system add
  join                                             时机零协议：新机首次上绳（打印动线）
  init [dir] [--project-id ID --project-name NAME] 初始化 .ousheng/ 工作区（默认=目录名）
  me [<id>] [--name N]                             查看/设置默认身份（.ousheng/me）
  team add <id> [--name N --type human|agent       加协作者/AI 窗口（不动默认身份）
        --responsible-human H --role R --system S]
  repo [set <system-id> <本地代码仓路径>]           多仓拓扑：system→代码仓本机映射
  system add <id> [--name N --parent P]            声明系统
  todo <title> [--system S --version V]            建任务（自动 T-xxx ID + 全默认值）
  sync [--actor ID]                                git pull + push（收尾闭环）+ 索引刷新 + 看板/我的上下文
  converge                                         收敛检查（CONVERGED/IN_PROGRESS/BLOCKED）
  design <list|show|decide|supersede|withdraw>     共识层：整体方案（list 可按 --status/--waiting-for 过滤；decide/supersede 为 human 门；withdraw 为 owner 或 human 门）

context (查看时机: 每日启动 / 遇到问题 / 任务结束):
  context me [--actor ID] [--json]                 我的工程上下文（第一层）
  context actor <id> [--json]                      Actor 视图
  context system <id> [--json]                     System 视图

work:
  work list [--system --status --assignee --version --type --open]
  work show <id> [--json]
  work create --file wi.yaml | --id --type --title [--system --version --assignee --role --accountable --detected-by] [--actor]
  work update <id> (--file wi.yaml --expect N | --status S [--expect N]) [--actor]
  work assign <id> --assignee A --role R [--expect N] [--actor]
  bug report (--file bug.yaml | --title T ...) [--detected-by ID] [--actor]
  progress report <id> --value 0.7 --actor A --basis B [--expect N]

registry (读为主，人工编辑 YAML + git 提交):
  actor list | actor show <id>
  system list | system show <id>
  assignment list

evidence / activity:
  evidence add <work-id> --type T --locator L [--source --result --note --expect --actor]
  evidence list <work-id>
  activity list [--limit N]

view:
  view kanban | view actor <id> | view system <id> | view version --version V | view project

index:
  index rebuild                                    从 canonical YAML 重建派生索引
  index status                                     索引状态

migrate:
  migrate schema                                   v1 cards/ → .ousheng/work/（幂等）
  migrate resolve-owner <id> --assignee A [--role R --accountable H]

其他: 每个子命令支持 --dir <workspace>（默认 .）
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, versionString())
		return 0
	case "setup":
		return cmdSetup(args[1:], stdout, stderr)
	case "join":
		return cmdJoin(args[1:], stdout, stderr)
	case "adapter":
		return cmdAdapter(args[1:], stdout, stderr)
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr)
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	case "me":
		return cmdMe(args[1:], stdout, stderr)
	case "team":
		if len(args) < 2 || args[1] != "add" {
			fmt.Fprintln(stderr, "usage: ousheng team add <id> [...]")
			return 2
		}
		return cmdTeamAdd(args[2:], stdout, stderr)
	case "repo":
		return cmdRepo(args[1:], stdout, stderr)
	case "todo":
		return cmdTodo(args[1:], stdout, stderr)
	case "sync":
		return cmdSync(args[1:], stdout, stderr)
	case "converge":
		return cmdConverge(args[1:], stdout, stderr)
	case "design":
		return cmdDesign(args[1:], stdout, stderr)
	case "context":
		return cmdContext(args[1:], stdout, stderr)
	case "work":
		return cmdWork(args[1:], stdout, stderr)
	case "bug":
		return cmdBug(args[1:], stdout, stderr)
	case "progress":
		return cmdProgress(args[1:], stdout, stderr)
	case "actor":
		return cmdActor(args[1:], stdout, stderr)
	case "system":
		return cmdSystem(args[1:], stdout, stderr)
	case "assignment":
		return cmdAssignment(args[1:], stdout, stderr)
	case "evidence":
		return cmdEvidence(args[1:], stdout, stderr)
	case "activity":
		return cmdActivity(args[1:], stdout, stderr)
	case "view":
		return cmdView(args[1:], stdout, stderr)
	case "index":
		return cmdIndex(args[1:], stdout, stderr)
	case "migrate":
		return cmdMigrate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n\n", args[0])
		fmt.Fprint(stderr, usage)
		return 2
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// --- 基础命令 ---

func cmdInit(args []string, stdout, stderr io.Writer) int {
	fs := newFS("init")
	projectID := fs.String("project-id", "", "project id (lowercase-hyphen)")
	projectName := fs.String("project-name", "", "project display name")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	dirFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "dir" {
			dirFlagSet = true
		}
	})
	dir := fs.Dir()
	if fs.NArg() > 1 {
		fmt.Fprintf(stderr, "init: unexpected argument %q\n", fs.Arg(1))
		return 2
	}
	if fs.NArg() == 1 {
		if dirFlagSet {
			fmt.Fprintln(stderr, "init: pass either positional dir or --dir, not both")
			return 2
		}
		dir = fs.Arg(0)
	}
	if *projectID == "" {
		// 默认：目录名 slug（init myproj → project-id myproj）
		*projectID = slugify(filepath.Base(absDir(dir)))
		if *projectID == "" {
			fmt.Fprintln(stderr, "--project-id required")
			return 2
		}
	}
	if *projectName == "" {
		*projectName = *projectID
	}
	repo := gityaml.Open(dir)
	if err := repo.InitWorkspace(projectModel(*projectID, *projectName)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "initialized workspace at %s/.ousheng (project %s)\n", dir, *projectID)
	return 0
}

func cmdConverge(args []string, stdout, stderr io.Writer) int {
	fs := newFS("converge")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	idx, snap, err := loadSnapshot(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// 共识层输入经 kn 显式携带（v0.4：不进 Index 查询面——S5 判决）。
	kn := converge.Knowledge{Designs: snap.Designs, Architecture: snap.Architecture}
	res, err := converge.Check(idx, kn)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, res.Status)
	for _, b := range res.Blockers {
		fmt.Fprintf(stdout, "  - %s\n", b)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(stdout, "  ! %s\n", w)
	}
	if len(res.Cycle) > 0 {
		fmt.Fprintf(stdout, "  cycle: %v\n", res.Cycle)
	}
	if res.Status == converge.Blocked {
		return 1
	}
	return 0
}

func cmdSync(args []string, stdout, stderr io.Writer) int {
	fs := newFS("sync")
	actor := fs.String("actor", "", "also show my context for this actor")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	if code := rejectExtra(fs, stderr); code != 0 {
		return code
	}
	dir := fs.Dir()
	// 1. git pull（无 remote 则跳过——单机也成立）
	pullOut, pullErr := gitPull(dir)
	switch {
	case pullErr == nil:
		fmt.Fprintf(stdout, "%s", pullOut)
	case errors.Is(pullErr, errNoRemote):
		fmt.Fprintln(stdout, "pull skipped (no remote configured)")
	default:
		fmt.Fprintf(stdout, "pull skipped (%v)\n", pullErr)
	}
	// 1.5 git push：仅 pull 成功后收口（先拉齐再推，分叉态不推）。
	// 软失败——推送失败只提示，不阻塞 sync；无未推提交时静默。
	if pullErr == nil {
		if pushOut, err := gitPush(dir); err != nil {
			if !errors.Is(err, errNoRemote) {
				fmt.Fprintf(stdout, "push skipped (%v)\n", err)
			}
		} else if s := strings.TrimSpace(pushOut); s != "" && !strings.Contains(s, "Everything up-to-date") {
			fmt.Fprintf(stdout, "%s", s)
		}
	}
	// 2. 索引刷新（= rebuild；SQLite 存在时一并重建）
	if code := rebuildIndex(dir, stdout, stderr); code != 0 {
		return code
	}
	// 3. 看板概览
	c, err := context.New(gityaml.Open(dir))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	p := projection.New(c)
	sum, err := p.ProjectSummary()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	projection.RenderProjectSummary(stdout, sum)
	if *actor != "" {
		fmt.Fprintln(stdout, "--- my context ---")
		mc, err := c.GetMyContext(*actor)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		printJSON(stdout, mc)
	}
	return 0
}

// --- 共用 helpers ---

type fsCommon struct{ dir *string }

func newFS(name string) *flagSetWithDir {
	fs := &flagSetWithDir{FlagSet: newFlagSet(name)}
	fs.dir = fs.String("dir", envWorkspaceDir(), "workspace dir (default: $OUSHENG_DIR or .)")
	return fs
}

// envWorkspaceDir：--dir 未显式给出时的默认工作区。优先 OUSHENG_DIR——
// 适配器与指南用它把 CLI 指向上下文仓（2026-09-16 反馈：此前只认 cwd/--dir，
// plugin 传的 OUSHENG_DIR 形同虚设）。
func envWorkspaceDir() string {
	if d := os.Getenv("OUSHENG_DIR"); d != "" {
		return d
	}
	return "."
}

func (f *flagSetWithDir) Dir() string { return *f.dir }

func loadIndex(dir string) (index.Index, error) {
	idx, _, err := loadSnapshot(dir)
	return idx, err
}

// loadSnapshot 装载索引 + 完整快照（converge 需要共识层字段，Index 不感知——S5）。
func loadSnapshot(dir string) (index.Index, index.Snapshot, error) {
	repo := gityaml.Open(dir)
	snap, err := index.Load(repo)
	if err != nil {
		return nil, snap, err
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		return nil, snap, err
	}
	return idx, snap, nil
}

func usageErr(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	return 2
}
