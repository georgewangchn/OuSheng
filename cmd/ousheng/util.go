package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"

	gitadapter "ousheng/adapters/git"
	"ousheng/internal/model"
)

type flagSetWithDir struct {
	*flag.FlagSet
	dir *string
}

func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

// parseLoose 允许位置参数出现在 flag 任意位置（flag 包默认遇位置参数即停）。
// 借助 flag 定义表区分"flag 的值"与"位置参数"，做全量重排后标准解析。
// 例：["--open", "X", "--system", "k8s"] → ["--open", "--system", "k8s", "X"]
func parseLoose(fs *flag.FlagSet, args []string) error {
	// 收集 flag 定义：name → 是否 bool（bool 后面不跟值）
	isBool := map[string]bool{}
	known := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		known[f.Name] = true
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			isBool[f.Name] = true
		}
	})

	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if eq := strings.Index(name, "="); eq >= 0 {
				continue // -x=v 自带值
			}
			// 已知非 bool flag 且无 "="：下一个 token 是它的值，一并前置
			if known[name] && !isBool[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return fs.Parse(append(flags, positional...))
}

func projectModel(id, name string) model.Project {
	return model.Project{ID: id, Name: name}
}

// errNoRemote：git pull 无 remote / 无 tracking——单机常态，非故障。
var errNoRemote = errors.New("no remote configured")

func gitPull(dir string) (string, error) {
	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := string(out)
		// 无 remote / 无 tracking 的典型输出：归一为友好语义，不刷裸 git 报错
		for _, p := range []string{"no remote repository", "no tracking information", "no upstream"} {
			if strings.Contains(s, p) {
				return "", errNoRemote
			}
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(s))
	}
	return string(out), nil
}

// rejectExtra 拒绝多余位置参数。静默吞掉（如忘写 --system 的过滤值）比报错更危险：
// 用户会基于错误的全量结果做决策（场景测试实锤）。
func rejectExtra(fs *flagSetWithDir, stderr io.Writer) int {
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "%s: unexpected argument %q\n", fs.Name(), fs.Arg(0))
		return 2
	}
	return 0
}

func printJSON(w io.Writer, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	fmt.Fprintln(w, string(b))
}

// printYAMLish 输出人类可读 key: value 行（--file 输入原样回显的场景由调用方处理）。
func printKV(w io.Writer, k, v string) {
	fmt.Fprintf(w, "  %-18s %s\n", k, v)
}

// gitAdapterEvidence 经 git evidence adapter 验证 commit 并生成规范 evidence。
func gitAdapterEvidence(dir, commit string) (*model.Evidence, error) {
	ev, err := gitadapter.EvidenceForCommit(dir, commit)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}
