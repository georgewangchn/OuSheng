package card

import (
	"strings"
	"testing"
)

func base() Card {
	return Card{ID: "a", Owner: "backend", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestValidateOK(t *testing.T) {
	c := base()
	if err := Validate(c, []byte("id: a")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestValidateBadID(t *testing.T) {
	c := base()
	c.ID = "Bad_ID"
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected id error")
	}
}

func TestValidateVerifiedNeedsEvidence(t *testing.T) {
	c := base()
	c.Status = Verified
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected evidence required")
	}
}

func TestValidateBreakingNeedsHumanAck(t *testing.T) {
	c := base()
	c.Contract.Breaking = true
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected human_ack required")
	}
}

func TestValidateBreakingNeedsCompleteHumanAck(t *testing.T) {
	c := base()
	c.Contract.Breaking = true
	c.HumanAck = &HumanAck{Approver: "", AtVersion: 1} // empty approver
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected human_ack.approver required")
	}
}

func TestValidateRejectsCodeBlock(t *testing.T) {
	c := base()
	raw := []byte("task: |\n  " + strings.Repeat("`", 3) + "go\n  leak\n")
	if err := Validate(c, raw); err == nil {
		t.Fatal("expected forbidden-content error")
	}
}

func TestValidateRejectsOversize(t *testing.T) {
	c := base()
	if err := Validate(c, make([]byte, MaxCardBytes+1)); err == nil {
		t.Fatal("expected size error")
	}
}
