package main

import (
	"encoding/json"
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

// parseLoose 允许位置参数出现在 flag 之前（flag 包默认遇位置参数即停）。
// 做法：把首个 flag 之前的位置参数整体移到末尾，再标准解析。
// 例：["K8S-003", "--status", "ready"] → ["--status", "ready", "K8S-003"]
func parseLoose(fs *flag.FlagSet, args []string) error {
	var head []string
	rest := args
	for len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		head = append(head, rest[0])
		rest = rest[1:]
	}
	return fs.Parse(append(append([]string{}, rest...), head...))
}

func projectModel(id, name string) model.Project {
	return model.Project{ID: id, Name: name}
}

func gitPull(dir string) (string, error) {
	cmd := exec.Command("git", "pull", "--ff-only")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
