package main

// Onboarding 命令回归：单人 5 分钟上手路径逐条锁定。
//
// 目标路径：init(默认名) → me(免后续 --actor) → system add(默认名)
//           → todo(自动 ID/默认值/隐式 assignment) → setup(管道向导)。

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runIn(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	full := append(append([]string{}, args...), "--dir", dir)
	var so, se bytes.Buffer
	code := run(full, &so, &se)
	return so.String(), se.String(), code
}

func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	so, se, code := runIn(t, dir, args...)
	if code != 0 {
		t.Fatalf("cli %v failed (code %d):\nstdout: %s\nstderr: %s", args, code, so, se)
	}
	return so
}

func TestOnboardingInitDefaults(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "My Shop") // 带空格大写 → slug my-shop
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	out := mustRun(t, proj, "init")
	if !strings.Contains(out, "my-shop") {
		t.Fatalf("project-id must default to dir slug: %s", out)
	}
	b, err := os.ReadFile(filepath.Join(proj, ".ousheng", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "id: my-shop") {
		t.Fatalf("project.yaml wrong:\n%s", b)
	}
}

func TestOnboardingMeAndDefaultActor(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")

	out := mustRun(t, dir, "me", "george", "--name", "George")
	if !strings.Contains(out, "default identity set to george") {
		t.Fatalf("me output: %s", out)
	}
	// .ousheng/me 必须被 gitignore（本机个人配置，非 canonical 状态）
	gi, err := os.ReadFile(filepath.Join(dir, ".ousheng", ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "me") {
		t.Fatalf(".ousheng/.gitignore must ignore me: %v %q", err, gi)
	}
	// 查看身份
	out = mustRun(t, dir, "me")
	if !strings.Contains(out, "george") || !strings.Contains(out, "George") {
		t.Fatalf("me (view) wrong: %s", out)
	}
	// 已存在 actor 再 me：只切换默认身份，不报错
	out = mustRun(t, dir, "me", "george")
	if !strings.Contains(out, "exists") {
		t.Fatalf("re-me should no-op: %s", out)
	}
}

func TestOnboardingSystemAdd(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	out := mustRun(t, dir, "system", "add", "datax-ui", "--name", "湖仓前端")
	if !strings.Contains(out, "added") {
		t.Fatalf("system add: %s", out)
	}
	// 默认名 = id
	mustRun(t, dir, "system", "add", "datax-api")
	list := mustRun(t, dir, "system", "list")
	if !strings.Contains(list, "datax-ui") || !strings.Contains(list, "datax-api") {
		t.Fatalf("system list: %s", list)
	}
	// 幂等：重复 add 不报错不重复
	out = mustRun(t, dir, "system", "add", "datax-ui")
	if !strings.Contains(out, "exists") {
		t.Fatalf("dup add: %s", out)
	}
	list = mustRun(t, dir, "system", "list")
	if strings.Count(list, "datax-ui") != 1 {
		t.Fatalf("dup add must not duplicate: %s", list)
	}
}

func TestOnboardingTodoFullDefaults(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "datax-ui")

	out := mustRun(t, dir, "todo", "语义模型发布页")
	if !strings.Contains(out, "T-001") {
		t.Fatalf("todo auto id: %s", out)
	}
	// work show 验证默认值链：assignee/actor=me、role=dev、accountable=唯一 human
	show := mustRun(t, dir, "work", "show", "T-001")
	for _, want := range []string{"assignee: george", "acting_role: dev", "accountable_human: george", "system: datax-ui", "type: task"} {
		if !strings.Contains(show, want) {
			t.Fatalf("todo defaults missing %q:\n%s", want, show)
		}
	}
	// 隐式 assignment 已建立
	al := mustRun(t, dir, "assignment", "list")
	if !strings.Contains(al, "george") || !strings.Contains(al, "datax-ui") {
		t.Fatalf("implicit assignment missing: %s", al)
	}
	// 第二个 todo 自增
	out = mustRun(t, dir, "todo", "第二个任务")
	if !strings.Contains(out, "T-002") {
		t.Fatalf("todo increment: %s", out)
	}
	// 免 --actor 的全链路：update/progress/evidence/context me
	mustRun(t, dir, "work", "update", "T-001", "--status", "ready")
	mustRun(t, dir, "work", "update", "T-001", "--status", "doing")
	mustRun(t, dir, "progress", "report", "T-001", "--value", "0.5")
	mustRun(t, dir, "evidence", "add", "T-001", "--type", "manual_check", "--locator", "review/x")
	ctx := mustRun(t, dir, "context", "me")
	if !strings.Contains(ctx, "T-001") {
		t.Fatalf("context me without --actor must work: %s", ctx)
	}
	// activity 归因必须是默认身份 george（非空 actor）
	acts := mustRun(t, dir, "activity", "list")
	if !strings.Contains(acts, "george") {
		t.Fatalf("activity must attribute to default actor:\n%s", acts)
	}
}

func TestOnboardingTeamAdd(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "datax-ui")

	// 第二个人：human + assignment
	out := mustRun(t, dir, "team", "add", "lisi", "--name", "李四", "--role", "ui", "--system", "datax-ui")
	if !strings.Contains(out, "lisi added") || !strings.Contains(out, "assignment lisi×datax-ui") {
		t.Fatalf("team add: %s", out)
	}
	// AI 窗口：agent，responsible-human 默认 me
	out = mustRun(t, dir, "team", "add", "ui-dev", "--type", "agent", "--role", "ui", "--system", "datax-ui")
	if !strings.Contains(out, "agent") {
		t.Fatalf("team add agent: %s", out)
	}
	// 默认身份未被 team add 改动
	out = mustRun(t, dir, "me")
	if !strings.Contains(out, "george") {
		t.Fatalf("default identity must stay george: %s", out)
	}
	// registry 校验：agent 的 responsible_human = george
	b, err := os.ReadFile(filepath.Join(dir, ".ousheng", "actors", "ui-dev.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "responsible_human: george") {
		t.Fatalf("agent must default responsible_human to me:\n%s", b)
	}
	// 幂等
	out = mustRun(t, dir, "team", "add", "lisi")
	if !strings.Contains(out, "exists") {
		t.Fatalf("dup team add: %s", out)
	}
}

func TestOnboardingAckFlag(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "datax-server")
	mustRun(t, dir, "todo", "引擎切换", "--system", "datax-server")

	// breaking 契约无 ack → C2 拒绝
	yaml := `schema_version: 2
id: T-001
type: task
title: 引擎切换
system: datax-server
assignee: george
acting_role: dev
accountable_human: george
status: ready
revision: 1
contract:
  kind: lib
  status: proposed
  breaking: true
`
	swap := filepath.Join(dir, "swap.yaml")
	if err := os.WriteFile(swap, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, se, code := runIn(t, dir, "work", "update", "T-001", "--file", swap, "--expect", "1")
	if code == 0 || !strings.Contains(se, "human_ack") {
		t.Fatalf("breaking without ack must be rejected: code=%d stderr=%s", code, se)
	}
	// 同一文件 + --ack（默认身份 george）→ 放行且 ack 归因正确
	mustRun(t, dir, "work", "update", "T-001", "--file", swap, "--expect", "1", "--ack")
	show := mustRun(t, dir, "work", "show", "T-001")
	if !strings.Contains(show, "approver: george") {
		t.Fatalf("--ack must record human_ack with default actor:\n%s", show)
	}
}

func TestOnboardingMultiRepoEvidence(t *testing.T) {
	// 多仓拓扑：上下文仓 A + 独立代码仓 B；git_commit 证据必须验证 B 里的 commit
	ws := t.TempDir()
	mustRun(t, ws, "init")
	mustRun(t, ws, "me", "george")
	mustRun(t, ws, "system", "add", "datax-ui")
	mustRun(t, ws, "todo", "前端任务")
	mustRun(t, ws, "work", "update", "T-001", "--status", "ready")
	mustRun(t, ws, "work", "update", "T-001", "--status", "doing")

	// 代码仓 B：独立 git 仓 + 一个真实 commit
	code := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = code
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	runGit("config", "user.email", "t@t")
	runGit("config", "user.name", "t")
	os.WriteFile(filepath.Join(code, "a.txt"), []byte("v1"), 0o644)
	runGit("add", "a.txt")
	runGit("commit", "-qm", "feat: 首页改版")
	hashOut, err := exec.Command("git", "-C", code, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.TrimSpace(string(hashOut))

	// 未映射：commit 不在上下文仓 → 验证失败
	_, se, code2 := runIn(t, ws, "evidence", "add", "T-001", "--type", "git_commit", "--locator", hash)
	if code2 == 0 || !strings.Contains(se, "") {
		t.Fatalf("unmapped git_commit should fail in workspace repo: code=%d stderr=%s", code2, se)
	}

	// 映射后：在代码仓 B 验证成功
	mustRun(t, ws, "repo", "set", "datax-ui", code)
	out := mustRun(t, ws, "evidence", "add", "T-001", "--type", "git_commit", "--locator", hash)
	if !strings.Contains(out, "evidence added") {
		t.Fatalf("mapped git_commit must verify in code repo: %s", out)
	}
	// evidence 的 note 回填了 commit subject
	show := mustRun(t, ws, "work", "show", "T-001")
	if !strings.Contains(show, "feat: 首页改版") {
		t.Fatalf("evidence must carry commit subject:\n%s", show)
	}
	// repos.yaml 已被 gitignore
	gi, err := os.ReadFile(filepath.Join(ws, ".ousheng", ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "repos.yaml") {
		t.Fatalf("repos.yaml must be gitignored: %v", err)
	}
	// repo 查看映射
	list := mustRun(t, ws, "repo")
	if !strings.Contains(list, "datax-ui") {
		t.Fatalf("repo list: %s", list)
	}
}

func TestOnboardingTodoMultiSystemRequiresFlag(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george")
	mustRun(t, dir, "system", "add", "a-sys")
	mustRun(t, dir, "system", "add", "b-sys")
	_, se, code := runIn(t, dir, "todo", "任务")
	if code == 0 || !strings.Contains(se, "--system") {
		t.Fatalf("multi-system todo must demand --system: code=%d stderr=%s", code, se)
	}
	mustRun(t, dir, "todo", "任务", "--system", "a-sys")
}

func TestOnboardingSetupWizard(t *testing.T) {
	dir := t.TempDir()
	// 管道输入：项目名（空=默认）、用户名、显示名、系统列表
	stdin := "我的平台\ngeorge\nGeorge\ndatax-ui, datax-api\n"
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(stdin)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = old }()

	so, se, code := runIn(t, dir, "setup")
	if code != 0 {
		t.Fatalf("setup failed: %s", se)
	}
	for _, want := range []string{"立项 我的平台", "身份 george", "系统 datax-api, datax-ui", "ousheng todo"} {
		if !strings.Contains(so, want) {
			t.Fatalf("setup output missing %q:\n%s", want, so)
		}
	}
	// setup 后立即可 todo
	out := mustRun(t, dir, "todo", "第一个任务", "--system", "datax-ui")
	if !strings.Contains(out, "T-001") {
		t.Fatalf("todo after setup: %s", out)
	}
	// 幂等：再跑 setup 全部跳过（无新输入）
	so2, _, code2 := runIn(t, dir, "setup")
	if code2 != 0 {
		t.Fatalf("setup rerun failed: %s", so2)
	}
	if !strings.Contains(so2, "已设置") && !strings.Contains(so2, "已存在") {
		t.Fatalf("setup rerun should skip: %s", so2)
	}
}

func TestOnboardingSetupChineseSystemNames(t *testing.T) {
	dir := t.TempDir()
	stdin := "智能商城\nlisi\n李四\n前端, 服务端\n"
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(stdin)
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = old }()

	so, _, code := runIn(t, dir, "setup")
	if code != 0 {
		t.Fatalf("setup failed: %s", so)
	}
	// 中文系统名无法 slug → id 自动编号 sys-N，原名保留为显示名
	list := mustRun(t, dir, "system", "list")
	if !strings.Contains(list, "sys-1") || !strings.Contains(list, "前端") ||
		!strings.Contains(list, "sys-2") || !strings.Contains(list, "服务端") {
		t.Fatalf("chinese system names must keep name + auto id:\n%s", list)
	}
	// 中文项目名 slug 为空 → 回退目录名
	b, err := os.ReadFile(filepath.Join(dir, ".ousheng", "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "name: 智能商城") {
		t.Fatalf("project name must keep chinese:\n%s", b)
	}
}
