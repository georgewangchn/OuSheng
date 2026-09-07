package card

import "testing"

func TestValidateAllContractKinds(t *testing.T) {
	kinds := []string{"http", "cli", "lib", "event"}
	for _, k := range kinds {
		c := Card{
			ID: "test-" + k, Owner: "o", Task: "t", Status: Proposed, Version: 1,
			Contract: Contract{Kind: k, Breaking: false, Interface: []any{}},
		}
		if err := Validate(c, mustEncode(t, c)); err != nil {
			t.Errorf("kind %q should be valid: %v", k, err)
		}
	}
}

func TestValidateRejectsUnknownContractKind(t *testing.T) {
	c := Card{
		ID: "bad-kind", Owner: "o", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "grpc", Breaking: false, Interface: []any{}},
	}
	if err := Validate(c, mustEncode(t, c)); err == nil {
		t.Error("unknown contract kind 'grpc' should be rejected")
	}
}

func TestValidateBreakingRequiresHumanAck(t *testing.T) {
	c := Card{
		ID: "breaking", Owner: "o", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: true, Interface: []any{}},
	}
	if err := Validate(c, mustEncode(t, c)); err == nil {
		t.Error("breaking=true without human_ack should be rejected")
	}

	c.HumanAck = &HumanAck{Approver: "alice", AtVersion: 1}
	if err := Validate(c, mustEncode(t, c)); err != nil {
		t.Errorf("breaking=true with human_ack should pass: %v", err)
	}
}

func TestValidateBreakingEmptyApproverRejected(t *testing.T) {
	c := Card{
		ID: "breaking-empty", Owner: "o", Task: "t", Status: Proposed, Version: 1,
		Contract: Contract{Kind: "http", Breaking: true, Interface: []any{}},
		HumanAck: &HumanAck{Approver: "", AtVersion: 1},
	}
	if err := Validate(c, mustEncode(t, c)); err == nil {
		t.Error("breaking=true with empty approver should be rejected")
	}
}

func TestValidateChineseCharsInAllFields(t *testing.T) {
	c := Card{
		ID:      "zh-card",
		Owner:   "后端组",
		Task:    "提供用户登录鉴权接口，支持多因素认证",
		Status:  Proposed,
		Version: 1,
		Contract: Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{
				map[string]any{
					"method":   "POST",
					"path":     "/登录",
					"behavior": "有效凭证返回令牌；无效返回401",
				},
			},
		},
	}
	if err := Validate(c, mustEncode(t, c)); err != nil {
		t.Errorf("Chinese chars should be valid: %v", err)
	}
}

func TestValidateCardNearSizeLimit(t *testing.T) {
	task := make([]byte, 8200)
	for i := range task {
		task[i] = 'x'
	}
	c := Card{
		ID: "large", Owner: "o", Status: Proposed, Version: 1,
		Task:     string(task),
		Contract: Contract{Kind: "http", Breaking: false, Interface: []any{}},
	}
	err := Validate(c, mustEncode(t, c))
	if err == nil {
		t.Error("card over 8192 bytes should be rejected by size gate")
	}
}

func mustEncode(t *testing.T, c Card) []byte {
	b, err := Encode(c)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}
