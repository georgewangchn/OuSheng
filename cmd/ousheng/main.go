// ousheng 是 v0.3 Engineering Context Runtime CLI（v0.3 §37）。
//
// 保留 v1 兼容：board 二进制与 cards/ 工作流不受影响。
package main

import (
	"fmt"
	"io"
	"os"

	"ousheng/internal/converge"
	"ousheng/internal/context"
	"ousheng/internal/index"
	"ousheng/internal/index/memory"
	"ousheng/internal/projection"
	"ousheng/internal/state/gityaml"
)

const cliVersion = "0.3.0"

func versionString() string { return "ousheng " + cliVersion }

const usage = `ousheng — 轻量工程上下文运行时（Engineering Context Runtime）

用法: ousheng <command> [args]

workspace:
  init [dir] --project-id ID --project-name NAME   初始化 .ousheng/ 工作区
  sync [--actor ID]                                git pull + 索引刷新 + 看板/我的上下文
  converge                                         收敛检查（CONVERGED/IN_PROGRESS/BLOCKED）

context (查看时机: 每日启动 / 遇到问题 / 任务结束):
  context me --actor ID [--json]                   我的工程上下文（第一层）
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
  view kanban | view system <id> | view version --version V | view project

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
	case "init":
		return cmdInit(args[1:], stdout, stderr)
	case "sync":
		return cmdSync(args[1:], stdout, stderr)
	case "converge":
		return cmdConverge(args[1:], stdout, stderr)
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
	if *projectID == "" {
		fmt.Fprintln(stderr, "--project-id required")
		return 2
	}
	repo := gityaml.Open(fs.Dir())
	if err := repo.InitWorkspace(projectModel(*projectID, *projectName)); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "initialized workspace at %s/.ousheng (project %s)\n", fs.Dir(), *projectID)
	return 0
}

func cmdConverge(args []string, stdout, stderr io.Writer) int {
	fs := newFS("converge")
	if err := parseLoose(fs.FlagSet, args); err != nil {
		return usageErr(stderr, err)
	}
	idx, err := loadIndex(fs.Dir())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	res, err := converge.Check(idx)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, res.Status)
	for _, b := range res.Blockers {
		fmt.Fprintf(stdout, "  - %s\n", b)
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
	dir := fs.Dir()
	// 1. git pull（无 remote 则跳过——单机也成立）
	if out, err := gitPull(dir); err != nil {
		fmt.Fprintf(stdout, "pull skipped (%v)\n", err)
	} else {
		fmt.Fprintf(stdout, "%s", out)
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
	fs.dir = fs.String("dir", ".", "workspace dir")
	return fs
}

func (f *flagSetWithDir) Dir() string { return *f.dir }

func loadIndex(dir string) (index.Index, error) {
	repo := gityaml.Open(dir)
	snap, err := index.Load(repo)
	if err != nil {
		return nil, err
	}
	idx := memory.New()
	if err := idx.Rebuild(snap); err != nil {
		return nil, err
	}
	return idx, nil
}

func usageErr(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	return 2
}
