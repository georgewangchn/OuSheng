package model

import (
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
		{"lowercase id", func(w *WorkItem) { w.ID = "bug-017" }},
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
