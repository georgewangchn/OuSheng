package govern

// hook_test.go —— pre-push 钩子的行为锁（真跑钩子脚本 + 桩 go/桩剧本）。
//
// 锁三件事：①触及 state 面的推送 → 四剧本被调用，剧本失败 → 推送中止（exit 1）；
// ②不触 state 面的推送 → 只 vet、不跑剧本；③vet 失败 → 推送中止。
// 不用「源码含 grep -q state」那种字符串锁（E6）。

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrePushHook(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 不可用")
	}
	hook, err := os.ReadFile(filepath.Join("..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name       string
		touch      string // 提交触及的文件
		vetFail    bool
		scriptFail bool
		wantVet    bool
		wantRuns   int // 期望被调用的剧本数
		wantExit   bool
	}{
		{"触 state 面 → 四剧本", "internal/state/gityaml/x.go", false, false, true, 4, false},
		{"触 state 面且剧本失败 → 首个失败即中止", "internal/workspace/w.go", false, true, true, 1, true},
		{"不触 state 面 → 只 vet", "docs/x.md", false, false, true, 0, false},
		{"vet 失败 → 中止", "cmd/ousheng/x.go", true, false, true, 0, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := t.TempDir()
			git := func(args ...string) {
				t.Helper()
				if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			git("init", "-q", "-b", "main")
			git("config", "user.email", "t@t")
			git("config", "user.name", "t")

			// 钩子 + 四个桩剧本（记调用次数；按需失败）。桩 go：记 vet；按需失败。
			os.MkdirAll(filepath.Join(repo, ".githooks"), 0o755)
			os.WriteFile(filepath.Join(repo, ".githooks", "pre-push"), hook, 0o755)
			os.MkdirAll(filepath.Join(repo, "scripts"), 0o755)
			callLog := filepath.Join(repo, "calls.log")
			for _, s := range []string{"dogfood-s7.sh", "dogfood-s7-chaos.sh", "dogfood-s7-chaos2.sh", "dogfood-s7-design.sh"} {
				os.WriteFile(filepath.Join(repo, "scripts", s),
					[]byte("#!/bin/sh\necho script >> \""+callLog+"\"\n[ \"${STUB_SCRIPT_FAIL:-0}\" = \"1\" ] && exit 1\nexit 0\n"), 0o755)
			}
			bin := filepath.Join(repo, "bin")
			os.MkdirAll(bin, 0o755)
			os.WriteFile(filepath.Join(bin, "go"),
				[]byte("#!/bin/sh\necho vet >> \""+callLog+"\"\n[ \"${STUB_VET_FAIL:-0}\" = \"1\" ] && exit 1\nexit 0\n"), 0o755)

			git("remote", "add", "origin", "https://example.invalid/x.git")
			os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644)
			git("add", "-A")
			git("commit", "-qm", "base")
			baseSha := shaOf(t, repo, "HEAD")
			git("update-ref", "refs/remotes/origin/main", baseSha)

			// 被推送的提交。
			os.MkdirAll(filepath.Dir(filepath.Join(repo, c.touch)), 0o755)
			os.WriteFile(filepath.Join(repo, c.touch), []byte("x\n"), 0o644)
			git("add", "-A")
			git("commit", "-qm", "change")
			headSha := shaOf(t, repo, "HEAD")

			vetFail, scriptFail := "0", "0"
			if c.vetFail {
				vetFail = "1"
			}
			if c.scriptFail {
				scriptFail = "1"
			}
			cmd := exec.Command("bash", filepath.Join(repo, ".githooks", "pre-push"), "origin", "https://example.invalid/x.git")
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"STUB_VET_FAIL="+vetFail, "STUB_SCRIPT_FAIL="+scriptFail)
			// git 对 pre-push 的 stdin：本地 ref 本地 sha 远端 ref 远端 sha。
			cmd.Stdin = bytes.NewBufferString("refs/heads/main " + headSha + " refs/heads/main " + baseSha + "\n")
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			if (err != nil) != c.wantExit {
				t.Fatalf("退出态错（wantExit=%v）: %v\n%s", c.wantExit, err, out.String())
			}
			if c.wantExit && !strings.Contains(out.String(), "FATAL") {
				t.Fatalf("中止必须响亮说明原因:\n%s", out.String())
			}
			calls, _ := os.ReadFile(callLog)
			if got := strings.Count(string(calls), "vet"); (got > 0) != c.wantVet {
				t.Fatalf("vet 调用态错: %q", calls)
			}
			if got := strings.Count(string(calls), "script"); got != c.wantRuns {
				t.Fatalf("剧本调用数错（want %d）: %d\n%s", c.wantRuns, got, calls)
			}
		})
	}
}

func shaOf(t *testing.T, repo, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
