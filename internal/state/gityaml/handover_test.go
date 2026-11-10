package gityaml

import (
	"strings"
	"testing"

	"ousheng/internal/model"
)

// newHandoverRepo：git 仓 + 已注册 agent 座位 data（SaveHandover 的注册表门）。
func newHandoverRepo(t *testing.T) *Repo {
	t.Helper()
	r := newDesignRepo(t)
	if err := r.SaveSystems([]model.System{{ID: "lake", Name: "lake"}}, "systems: lake"); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveActor(model.ActorFile{SchemaVersion: 1, Actor: model.Actor{
		ID: "data", Type: model.ActorAgent, DisplayName: "data", ResponsibleHuman: "pm",
	}}, "registry: data"); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestAttachSessionIdempotent（评审 🔴 锁）：同 sid 连续 attach 必须幂等——
// 档案 §34 反事实③ 的机制保证（plugin + runner 双 attach 是常态路径）。
// 实锤：原实现 no-op 时仍落 git commit，工作区持根级 .ousheng.lock（untracked）
// 时 git 走 "nothing added to commit but untracked files present" 变体，
// 容错串匹配漏掉 → 第二次 attach 失败。
func TestAttachSessionIdempotent(t *testing.T) {
	r := newHandoverRepo(t)
	if _, err := r.SaveHandover("lake", "data", "", "# gen1\n导航\n", "handover: save"); err != nil {
		t.Fatal(err)
	}
	if err := r.AttachSession("lake", "data", "ses_1", "attach 1"); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	// 第二次同 sid：内容零变化 → 必须 no-op 成功（不落 git、不报错）。
	if err := r.AttachSession("lake", "data", "ses_1", "attach 2 same"); err != nil {
		t.Fatalf("idempotent attach must be no-op, got: %v", err)
	}
	// 指针确实写入了第一次的值。
	hs, err := r.ListHandover()
	if err != nil || len(hs) != 1 {
		t.Fatalf("ListHandover: %v %v", hs, err)
	}
	if hs[0].Session != "ses_1" {
		t.Fatalf("session pointer = %q, want ses_1", hs[0].Session)
	}
	// 换 sid 是真变化：必须生效。
	if err := r.AttachSession("lake", "data", "ses_2", "attach 3"); err != nil {
		t.Fatalf("attach different sid: %v", err)
	}
	hs, _ = r.ListHandover()
	if hs[0].Session != "ses_2" {
		t.Fatalf("session pointer = %q, want ses_2", hs[0].Session)
	}
	// 清指针（强制换代）也是真变化。
	if err := r.AttachSession("lake", "data", "", "attach 4 clear"); err != nil {
		t.Fatalf("attach empty sid: %v", err)
	}
	hs, _ = r.ListHandover()
	if hs[0].Session != "" {
		t.Fatalf("session pointer = %q, want empty", hs[0].Session)
	}
}

// TestAttachSessionGhostSeat：不存在的座位 attach 必须失败（fail-closed）。
func TestAttachSessionGhostSeat(t *testing.T) {
	r := newHandoverRepo(t)
	if _, err := r.SaveHandover("lake", "data", "", "# gen1\n", "handover: save"); err != nil {
		t.Fatal(err)
	}
	if err := r.AttachSession("lake", "nobody", "ses_x", "ghost"); err == nil {
		t.Fatal("ghost seat attach must fail")
	} else if !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("ghost seat error: %v", err)
	}
}
