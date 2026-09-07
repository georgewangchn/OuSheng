package card

import "testing"

func TestValidatePartialEvidenceRejected(t *testing.T) {
	c := Card{
		ID: "partial-ev", Owner: "o", Task: "t", Status: Verified, Version: 1,
		Contract: Contract{Kind: "http", Breaking: false, Interface: []any{}},
		Evidence: &Evidence{
			Probe:          "test ran",
			PassedAtCommit: "abc123",
			By:             "",
		},
	}
	if err := Validate(c, mustEncode(t, c)); err == nil {
		t.Error("verified with empty evidence.By should be rejected")
	}
}

func TestValidatePartialHumanAckRejected(t *testing.T) {
	c := Card{
		ID: "partial-ack", Owner: "o", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: true, Interface: []any{}},
		HumanAck: &HumanAck{Approver: "alice", AtVersion: 0},
	}
	if err := Validate(c, mustEncode(t, c)); err != nil {
		t.Errorf("breaking with approver should pass even if at_version=0: %v", err)
	}
}

func TestValidateHumanAckWhitespaceApproverRejected(t *testing.T) {
	c := Card{
		ID: "ws-ack", Owner: "o", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: true, Interface: []any{}},
		HumanAck: &HumanAck{Approver: "   ", AtVersion: 1},
	}
	if err := Validate(c, mustEncode(t, c)); err == nil {
		t.Error("breaking with whitespace-only approver should be rejected")
	}
}
