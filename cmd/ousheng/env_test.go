package main

// H3 环境维度四件套锁（2026-11-12，档案 §38，pm 终局拍板「一个看板」的配套）：
// systems envs 登记 + WorkItem env 字段 + work list --env 过滤 + 四路判决
// （work show / context me / work list 接入；view kanban 明文出局）。
// 默认空 = dev（诚实降级，不强制）；值域 envRe 校验（fail-closed 拒垃圾）。

import (
	"strings"
	"testing"
)

func TestSystemsEnvsRegistry(t *testing.T) {
	dir := setupEnvBoard(t)
	out := mustRun(t, dir, "system", "add", "lake2", "--env", "dev", "--env", "pro")
	if !strings.Contains(out, "lake2") {
		t.Fatalf("system add 失败:\n%s", out)
	}
	out = mustRun(t, dir, "system", "list")
	if !strings.Contains(out, "envs: dev,pro") {
		t.Fatalf("system list 应展示 envs 登记:\n%s", out)
	}
	// 垃圾 env id 拒绝（fail-closed）
	if _, se, code := runIn(t, dir, "system", "add", "bad", "--env", "Bad_Env!"); code == 0 {
		t.Fatalf("非法 env id 必须拒: %s", se)
	}
}

func TestWorkEnvFields(t *testing.T) {
	dir := setupEnvBoard(t)
	// 创建带 env 的活（deployment 型最典型）
	mustRun(t, dir, "work", "create", "--id", "REL-001", "--type", "release",
		"--title", "v4 滚动", "--system", "lake", "--version", "v4", "--env", "pro",
		"--description", "三节点滚动", "--actor", "data")
	mustRun(t, dir, "work", "assign", "REL-001", "--assignee", "data", "--role", "dev", "--actor", "data")
	mustRun(t, dir, "work", "update", "REL-001", "--status", "ready", "--actor", "data")
	mustRun(t, dir, "work", "update", "REL-001", "--status", "doing", "--actor", "data")

	// ① work show：env 全量呈现
	out := mustRun(t, dir, "work", "show", "REL-001")
	if !strings.Contains(out, "env: pro") {
		t.Fatalf("work show 应含 env:\n%s", out)
	}
	// ② context me：WorkBrief 带 env（注入面短 token，T10 合规）
	out = mustRun(t, dir, "context", "me", "--actor", "data")
	if !strings.Contains(out, "env: pro") && !strings.Contains(out, `"env": "pro"`) {
		t.Fatalf("context me 应含 env:\n%s", out)
	}
	// ③ work list --env 过滤（env 是过滤维度不是列——§38 判决）
	out = mustRun(t, dir, "work", "list", "--env", "pro")
	if !strings.Contains(out, "REL-001") {
		t.Fatalf("work list --env pro 应命中 REL-001:\n%s", out)
	}
	out = mustRun(t, dir, "work", "list", "--env", "uat")
	if strings.Contains(out, "REL-001") {
		t.Fatalf("work list --env uat 不应命中 REL-001:\n%s", out)
	}
	// 空 env = dev 语义：不带 --env 的常规单（T-001）可被 --env dev 过滤到吗？
	// 判决：env 空 = 缺省 dev，过滤语义对齐（空 env 匹配 --env dev）。
	out = mustRun(t, dir, "work", "list", "--env", "dev")
	if !strings.Contains(out, "T-001") {
		t.Fatalf("空 env 应匹配 --env dev（缺省语义）:\n%s", out)
	}
	// ④ view kanban 出局：不含 env 列（明文判决，防扫视层加列）
	out = mustRun(t, dir, "view", "kanban")
	if strings.Contains(out, "env") {
		t.Fatalf("view kanban 不应出现 env（过滤维度非列）:\n%s", out)
	}
	// work update --env 改环境
	mustRun(t, dir, "work", "update", "REL-001", "--env", "uat", "--actor", "data")
	out = mustRun(t, dir, "work", "list", "--env", "uat")
	if !strings.Contains(out, "REL-001") {
		t.Fatalf("update --env 后应可过滤:\n%s", out)
	}
	// 非法 env 值拒（fail-closed）
	if _, _, code := runIn(t, dir, "work", "update", "REL-001", "--env", "生产!", "--actor", "data"); code == 0 {
		t.Fatal("非法 env 值必须拒")
	}
}

func setupEnvBoard(t *testing.T) string {
	t.Helper()
	dir := cruiseFixture(t) // pm+data 座位、lake 系统、T-001 doing@data
	return dir
}
