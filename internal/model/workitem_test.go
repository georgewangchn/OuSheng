package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const validWorkItem = `schema_version: 2
id: BUG-017
type: bug
title: CDC checkpoint 恢复失败
system: datax-backend
target_version: v2.0
assignee: backend-agent
acting_role: backend
accountable_human: zhangsan
status: doing
revision: 7
depends_on:
  - K8S-003
related_to:
  - FEAT-CDC-001
progress:
  value: 0.7
  actor: backend-agent
  reported_at: 2026-09-07T10:30:00+08:00
  basis: implementation-checklist
evidence:
  - type: git_commit
    source: github
    locator: abc123
    observed_at: 2026-09-07T11:00:00+08:00
`

func TestDecodeWorkItemStrict(t *testing.T) {
	w, err := DecodeWorkItem([]byte(validWorkItem))
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "BUG-017" || w.Type != TypeBug || w.Status != StatusDoing {
		t.Fatalf("bad decode: %+v", w)
	}
	if w.Revision != 7 || w.TargetVersion != "v2.0" {
		t.Fatalf("bad fields: rev=%d tv=%q", w.Revision, w.TargetVersion)
	}
	if w.Progress == nil || w.Progress.Value != 0.7 || w.Progress.Basis != BasisImplementationChecklist {
		t.Fatalf("bad progress: %+v", w.Progress)
	}
	if len(w.Evidence) != 1 || w.Evidence[0].Type != EvidenceGitCommit {
		t.Fatalf("bad evidence: %+v", w.Evidence)
	}
}

func TestDecodeWorkItemRejectsUnknownFields(t *testing.T) {
	raw := validWorkItem + "bogus_field: 1\n"
	if _, err := DecodeWorkItem([]byte(raw)); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
}

func TestValidateWorkItem(t *testing.T) {
	w, err := DecodeWorkItem([]byte(validWorkItem))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkItem(w, []byte(validWorkItem)); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}
}

func TestValidateWorkItemRules(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))

	cases := []struct {
		name string
		mut  func(*WorkItem)
	}{
		{"schema_version", func(w *WorkItem) { w.SchemaVersion = 1 }},
		{"bad id", func(w *WorkItem) { w.ID = "bad id!" }},
		{"id with underscore", func(w *WorkItem) { w.ID = "BUG_017" }},
		{"bad type", func(w *WorkItem) { w.Type = WorkItemType("epic") }},
		{"empty title", func(w *WorkItem) { w.Title = " " }},
		{"bad status", func(w *WorkItem) { w.Status = WorkStatus("deploying") }},
		{"zero revision", func(w *WorkItem) { w.Revision = 0 }},
		{"bad system ref", func(w *WorkItem) { w.System = "DataX Backend!" }},
		{"progress out of range", func(w *WorkItem) {
			w.Progress = &ProgressReport{Value: 1.5, Actor: "a", ReportedAt: "2026-09-07T10:30:00+08:00", Basis: BasisManual}
		}},
		{"progress bad basis", func(w *WorkItem) {
			w.Progress = &ProgressReport{Value: 0.5, Actor: "a", ReportedAt: "2026-09-07T10:30:00+08:00", Basis: ProgressBasis("vibes")}
		}},
		{"progress bad time", func(w *WorkItem) {
			w.Progress = &ProgressReport{Value: 0.5, Actor: "a", ReportedAt: "not-a-time", Basis: BasisManual}
		}},
		{"evidence bad type", func(w *WorkItem) {
			w.Evidence = []Evidence{{Type: EvidenceType("gossip")}}
		}},
		{"contract bad kind", func(w *WorkItem) {
			w.Contract = &Contract{Kind: "grpc", Status: ContractProposed}
		}},
		{"contract bad status", func(w *WorkItem) {
			w.Contract = &Contract{Kind: "http", Status: ContractStatus("draft")}
		}},
		{"self dep", func(w *WorkItem) { w.DependsOn = []string{"BUG-017"} }},
		{"bad priority", func(w *WorkItem) { w.Priority = "urgent" }},
		{"bad due_on format", func(w *WorkItem) { w.DueOn = "2026/09/30" }},
		{"bad due_on month", func(w *WorkItem) { w.DueOn = "2026-13-01" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone := w
			tc.mut(&clone)
			if err := ValidateWorkItem(clone, []byte(validWorkItem)); err == nil {
				t.Fatalf("expected rejection for %s", tc.name)
			}
		})
	}
}

func TestValidateWorkItemPlanningFields(t *testing.T) {
	raw := validWorkItem + "priority: P1\ndue_on: 2026-09-30\ndescription: 验收标准：批量重跑入口可用\n"
	w, err := DecodeWorkItem([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if w.Priority != "P1" || w.DueOn != "2026-09-30" || w.Description == "" {
		t.Fatalf("planning fields lost: %+v", w)
	}
	if err := ValidateWorkItem(w, []byte(raw)); err != nil {
		t.Fatalf("valid planning fields rejected: %v", err)
	}
	back, err := EncodeWorkItem(w)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := DecodeWorkItem(back)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w2, w) {
		t.Fatalf("roundtrip mismatch:\n%+v\n%+v", w, w2)
	}
}

func TestValidateWorkItemSizeGate(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))
	w.Title = strings.Repeat("长", 6000) // > 8192 bytes when encoded
	raw, err := EncodeWorkItem(w)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= MaxWorkItemBytes {
		t.Fatalf("test setup: encoded %d bytes, need > %d", len(raw), MaxWorkItemBytes)
	}
	if err := ValidateWorkItem(w, raw); err == nil {
		t.Fatal("expected size rejection")
	} else if !strings.Contains(err.Error(), "骨架原则") || !strings.Contains(err.Error(), "--locator") {
		// 2026-09-21：尺寸门必须给溢出路径（fail-closed 也要给下一步）——
		// 裸报"exceeds 8192"让使用者对着冻结单无路可走（BUG-002 实测事故）。
		t.Fatalf("尺寸门报错必须带溢出路径指引（指针证据/外部文档），got: %v", err)
	}
}

func TestValidateWorkItemContractVerifiedNeedsEvidence(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))
	w.Contract = &Contract{Kind: "http", Status: ContractVerified}
	w.Evidence = nil
	if err := ValidateWorkItem(w, []byte(validWorkItem)); err == nil {
		t.Fatal("verified contract without evidence must be rejected")
	}
	w.Evidence = []Evidence{{Type: EvidenceTestResult, Source: "pytest", Locator: "CDC-017", Result: "passed"}}
	if err := ValidateWorkItem(w, []byte(validWorkItem)); err != nil {
		t.Fatalf("verified contract with evidence rejected: %v", err)
	}
}

func TestValidateWorkItemBreakingNeedsHumanAck(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))
	w.Contract = &Contract{Kind: "http", Status: ContractLive, Breaking: true}
	if err := ValidateWorkItem(w, []byte(validWorkItem)); err == nil {
		t.Fatal("breaking contract without human_ack must be rejected")
	}
	w.HumanAck = &HumanAck{Approver: "zhangsan", At: "2026-09-07T10:00:00+08:00"}
	if err := ValidateWorkItem(w, []byte(validWorkItem)); err != nil {
		t.Fatalf("breaking contract with human_ack rejected: %v", err)
	}
}

func TestWorkItemRoundTrip(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))
	raw, err := EncodeWorkItem(w)
	if err != nil {
		t.Fatal(err)
	}
	w2, err := DecodeWorkItem(raw)
	if err != nil {
		t.Fatal(err)
	}
	if w2.ID != w.ID || w2.Revision != w.Revision || len(w2.Evidence) != len(w.Evidence) {
		t.Fatalf("round trip mismatch: %+v vs %+v", w, w2)
	}
}

func TestWorkItemSummarize(t *testing.T) {
	w, _ := DecodeWorkItem([]byte(validWorkItem))
	s := w.Summarize()
	if s.ID != "BUG-017" || s.System != "datax-backend" || s.Assignee != "backend-agent" {
		t.Fatalf("bad summary: %+v", s)
	}
}

// TestValidateOversizedGrowth（档案 §57）：超限卡只拒「增长型写」——
// ①新增 evidence 条目拒；②编码增长超余量拒；③收缩/等长/状态位微变放行。
// 卡点不变式：卡状态合法性，不卡合法写——此前顶格卡连 status→done（revision
// 进位 +1 字节）都被拒 = 写冻结死锁（真板 T-193 实锤）。§18 门值不动。
func TestValidateOversizedGrowth(t *testing.T) {
	cur, _ := DecodeWorkItem([]byte(validWorkItem))
	cur.Title = strings.Repeat("长", 3000)
	curRaw, _ := EncodeWorkItem(cur)
	if len(curRaw) <= MaxWorkItemBytes {
		t.Fatalf("test setup: cur must be oversized, got %d", len(curRaw))
	}
	// ① 新增 evidence：拒（哪怕只多一条极短的）。
	next := cur
	next.Evidence = append(append([]Evidence{}, cur.Evidence...), Evidence{Type: "manual_check", Locator: "x"})
	nextRaw, _ := EncodeWorkItem(next)
	var oe OversizedError
	if err := ValidateOversizedGrowth(cur, next, curRaw, nextRaw); !errors.As(err, &oe) {
		t.Fatalf("超限卡新增 evidence 必须拒（骨架原则）: %v", err)
	}
	// ② 增长超余量（改长描述）：拒，报错带下一步。
	next = cur
	next.Title = cur.Title + strings.Repeat("长", 100)
	nextRaw, _ = EncodeWorkItem(next)
	if err := ValidateOversizedGrowth(cur, next, curRaw, nextRaw); err == nil || !strings.Contains(err.Error(), "瘦身") {
		t.Fatalf("超限卡继续增长必须拒并指引瘦身: %v", err)
	}
	// ③ 状态位微变（关单 + revision 进位）：放行。
	next = cur
	next.Status = StatusDone
	next.Revision = cur.Revision + 1
	if next.Revision < 10 {
		next.Revision = 100 // 保证进位多 1-2 字节
	}
	nextRaw, _ = EncodeWorkItem(next)
	if err := ValidateOversizedGrowth(cur, next, curRaw, nextRaw); err != nil {
		t.Fatalf("超限卡的状态合法写（关单）必须放行——写冻结是卡点不变式的破口: %v", err)
	}
	// ④ 瘦身：放行（这是解冻的唯一正道，不能被门卡住）。
	next = cur
	next.Title = "短"
	nextRaw, _ = EncodeWorkItem(next)
	if err := ValidateOversizedGrowth(cur, next, curRaw, nextRaw); err != nil {
		t.Fatalf("瘦身写必须放行: %v", err)
	}
	// 尺寸门本体仍是可识别的类型化错误（更新路径据此分流）。
	if err := ValidateWorkItem(cur, curRaw); !errors.As(err, &oe) || !strings.Contains(err.Error(), "骨架原则") {
		t.Fatalf("尺寸门错误应可被 errors.As 识别且保留溢出路径文案: %v", err)
	}
}
