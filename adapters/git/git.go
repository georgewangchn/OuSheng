// Package git 是 External Evidence Adapter（v0.3 §41 第一版：Git）。
//
// 职责严格限定：观察外部事实 → 转成 Typed Evidence → 交给调用方关联 WorkItem。
// 不直接修改工程状态。
package git

import (
	"fmt"
	"os/exec"
	"strings"

	"ousheng/internal/model"
)

// CommitExists 验证 commit 可解析（rev-parse <sha>^{commit}）。
// 区分两类失败：rev-parse 非零退出（commit 不存在）→ false,nil；
// git 不可执行 / 目录异常 → 返回错误，不伪装成"不存在"。
func CommitExists(repoDir, commit string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", commit+"^{commit}")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, fmt.Errorf("git rev-parse in %s: %w", repoDir, err)
	}
	return true, nil
}

// Subject 返回 commit 的第一行描述。
func Subject(repoDir, commit string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--format=%s", commit)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log %s: %v: %s", commit, err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

// EvidenceForCommit 把一个已验证存在的 commit 规范化为 typed evidence。
func EvidenceForCommit(repoDir, commit string) (model.Evidence, error) {
	ok, err := CommitExists(repoDir, commit)
	if err != nil {
		return model.Evidence{}, err
	}
	if !ok {
		return model.Evidence{}, fmt.Errorf("commit %s not found in %s", commit, repoDir)
	}
	ev := model.Evidence{
		Type:       model.EvidenceGitCommit,
		Source:     "git",
		Locator:    commit,
		ObservedAt: model.Now(),
	}
	if subj, err := Subject(repoDir, commit); err == nil && subj != "" {
		ev.Note = subj
	}
	return ev, nil
}

// LatestCommit 返回 HEAD 的短 hash。
func LatestCommit(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
