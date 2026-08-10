package card

import (
	"strings"
	"testing"
)

func TestEncodeRoundTrip(t *testing.T) {
	original := Card{
		ID:      "auth-api",
		Owner:   "backend",
		Task:    "登录接口",
		Status:  Verified,
		Version: 3,
		Contract: Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{
				map[string]any{"method": "POST", "path": "/login"},
			},
		},
		Evidence: &Evidence{
			Probe:          "probe",
			PassedAtCommit: "abc",
			By:             "frontend",
		},
	}

	encoded, err := Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.ID != original.ID || decoded.Status != original.Status || decoded.Version != original.Version {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", decoded, original)
	}
	if decoded.Evidence == nil || decoded.Evidence.Probe != "probe" {
		t.Fatalf("evidence lost in roundtrip: %+v", decoded.Evidence)
	}
}

func TestSummarize(t *testing.T) {
	c := Card{
		ID:        "auth",
		Owner:     "backend",
		Task:      "login",
		Status:    Live,
		Version:   2,
		DependsOn: []string{"db"},
	}
	s := c.Summarize()
	if s.ID != "auth" || s.Owner != "backend" || s.Task != "login" {
		t.Fatalf("summary fields wrong: %+v", s)
	}
	if s.Status != "live" || s.Version != 2 {
		t.Fatalf("summary status/version wrong: %+v", s)
	}
	if len(s.DependsOn) != 1 || s.DependsOn[0] != "db" {
		t.Fatalf("summary depends_on wrong: %+v", s.DependsOn)
	}
}

func TestSummarizeNilDependsOn(t *testing.T) {
	c := Card{ID: "x", Owner: "o", Task: "t", Status: Proposed, Version: 1}
	s := c.Summarize()
	if s.DependsOn != nil && len(s.DependsOn) != 0 {
		t.Fatalf("expected nil/empty depends_on, got %+v", s.DependsOn)
	}
}

func TestDecodeMalformedYAML(t *testing.T) {
	cases := [][]byte{
		[]byte("not: valid: yaml: {{{"),
		[]byte("id: a\nowner: b\ntask: t\nstatus: proposed\nversion: notanumber\n"),
		[]byte(""),
	}
	for i, raw := range cases {
		_, err := Decode(raw)
		if err == nil {
			t.Fatalf("case %d: expected error for malformed yaml %q", i, raw)
		}
	}
}

func TestDecodeAcceptsNegativeVersion(t *testing.T) {
	raw := []byte("id: a\nowner: b\ntask: t\nstatus: proposed\nversion: -1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n")
	c, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode should parse negative version (validate catches it): %v", err)
	}
	if c.Version != -1 {
		t.Fatalf("expected version -1, got %d", c.Version)
	}
	if err := Validate(c, raw); err == nil {
		t.Fatal("validate should reject negative version")
	}
}

func TestValidateEmptyOwner(t *testing.T) {
	c := base()
	c.Owner = "   "
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected error for whitespace-only owner")
	}
}

func TestValidateEmptyTask(t *testing.T) {
	c := base()
	c.Task = ""
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected error for empty task")
	}
}

func TestValidateTracebackRejected(t *testing.T) {
	c := base()
	raw := []byte("task: " + strings.Repeat("x", 100) + " Traceback (most recent call last): boom")
	if err := Validate(c, raw); err == nil {
		t.Fatal("expected traceback rejection")
	}
}

func TestValidateNegativeVersion(t *testing.T) {
	c := base()
	c.Version = -1
	if err := Validate(c, []byte("x")); err == nil {
		t.Fatal("expected error for negative version")
	}
}
