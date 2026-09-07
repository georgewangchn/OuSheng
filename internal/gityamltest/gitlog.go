// Package gityamltest 提供测试用 git 辅助（读取日志等）。
package gityamltest

import (
	"fmt"
	"os/exec"
	"strings"
)

// GitLog 在 dir 内执行 git log 并返回输出。
func GitLog(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"log"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("git log error: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
