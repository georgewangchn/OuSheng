package main

import (
	"strings"
	"testing"
)

func mkUnassigned(t *testing.T, dir, id, wtype string) {
	t.Helper()
	mustRun(t, dir, "work", "create", "--id", id, "--type", wtype, "--title", id,
		"--system", "api", "--accountable", "george", "--description", "验收：x", "--actor", "george")
}

// 发布条件硬门：进行中必须有主（无主 active = "进行中"是假的）。
func TestDoingRequiresAssignee(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "api")
	mkUnassigned(t, dir, "T-1", "task")
	mustRun(t, dir, "work", "update", "T-1", "--status", "ready", "--actor", "george")

	if _, se, code := runIn(t, dir, "work", "update", "T-1", "--status", "doing", "--actor", "george"); code == 0 || !strings.Contains(se, "doing requires assignee") {
		t.Fatalf("无主进 doing 必须拒: code=%d err=%s", code, se)
	}
	mustRun(t, dir, "work", "assign", "T-1", "--assignee", "george", "--role", "dev", "--actor", "george")
	mustRun(t, dir, "work", "update", "T-1", "--status", "doing", "--actor", "george")
}

// PM 巡检面：待认领池（--unassigned）。
func TestWorkListUnassigned(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "api")
	mustRun(t, dir, "todo", "有主任务", "--system", "api") // todo 默认 assignee=george
	mkUnassigned(t, dir, "T-9", "requirement")

	out := mustRun(t, dir, "work", "list", "--unassigned")
	if !strings.Contains(out, "T-9") || strings.Contains(out, "T-001") {
		t.Fatalf("--unassigned 过滤错误:\n%s", out)
	}
}

// 四路判决（2026-09-18）：无主/信息不全信号不进 context me——注入面最小化，
// 执行者不需要知道不归他的单；只在 list/system view/converge 可见。
func TestUnassignedNotInjectedIntoContextMe(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "init")
	mustRun(t, dir, "me", "george", "--name", "George")
	mustRun(t, dir, "system", "add", "api")
	mkUnassigned(t, dir, "T-9", "requirement")

	mc := mustRun(t, dir, "context", "me", "--actor", "george")
	if strings.Contains(mc, "T-9") {
		t.Fatalf("无主单不得进 context me:\n%s", mc)
	}
	if out := mustRun(t, dir, "work", "list", "--unassigned"); !strings.Contains(out, "T-9") {
		t.Fatalf("但应在 work list --unassigned 可见:\n%s", out)
	}
	conv := mustRun(t, dir, "converge")
	if !strings.Contains(conv, "unassigned") {
		t.Fatalf("converge 应曝光待认领:\n%s", conv)
	}
}
