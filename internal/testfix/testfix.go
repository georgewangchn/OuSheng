// Package testfix 为跨包测试提供 fixture 工作区装载。
// 把 fixtures/lakehouse/* 复制到临时目录的 .ousheng/ 下并初始化 git，
// 得到一个可直接打开的 canonical workspace。
package testfix

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Setup 返回一个已就绪的 workspace 根目录（含 .ousheng 骨架 + git 仓库）。
func Setup(t testing.TB) string {
	t.Helper()
	dst := t.TempDir()
	if err := CopyWorkspace(dst); err != nil {
		t.Fatal(err)
	}
	return dst
}

// CopyWorkspace 把 fixtures/lakehouse 复制到 dir/.ousheng 并 git init + commit。
// 相对路径基于调用包位置不可靠，改从环境变量或固定查找：优先 OUSHENG_FIXTURES，
// 否则从当前工作目录向上查找 fixtures/lakehouse（go test 的 cwd 是包目录）。
func CopyWorkspace(dir string) error {
	src, err := findFixtures()
	if err != nil {
		return err
	}
	ousheng := filepath.Join(dir, ".ousheng")
	if err := copyDir(src, ousheng); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(ousheng, "cache"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(ousheng, "cache", ".gitignore"), []byte("*\n!.gitignore\n"), 0o644); err != nil {
		return err
	}
	if out, err := git(dir, "init", "-b", "main"); err != nil {
		if _, err2 := git(dir, "init"); err2 != nil {
			return err
		}
		_ = out
	}
	for _, c := range [][2]string{{"user.email", "board@ousheng.local"}, {"user.name", "ousheng"}} {
		if _, err := git(dir, "config", c[0], c[1]); err != nil {
			return err
		}
	}
	if _, err := git(dir, "add", ".ousheng"); err != nil {
		return err
	}
	if _, err := git(dir, "commit", "-m", "test: fixture workspace"); err != nil {
		return err
	}
	return nil
}

func findFixtures() (string, error) {
	if p := os.Getenv("OUSHENG_FIXTURES"); p != "" {
		return p, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "fixtures", "lakehouse")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", os.ErrNotExist
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// StripWork 删除所有 work 文件（构造空看板场景）。
func StripWork(t testing.TB, dir string) {
	t.Helper()
	work := filepath.Join(dir, ".ousheng", "work")
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			if err := os.Remove(filepath.Join(work, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := git(dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	_ = gitCommitAllowEmpty(dir)
}

func gitCommitAllowEmpty(dir string) error {
	cmd := exec.Command("git", "commit", "-m", "test: strip work")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "nothing to commit") {
		return err
	}
	return nil
}
