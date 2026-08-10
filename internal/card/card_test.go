package card

import "testing"

func TestDecodeRoundTrip(t *testing.T) {
	in := []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n")
	c, err := Decode(in)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.ID != "a" || c.Status != Proposed || c.Contract.Kind != "http" {
		t.Fatalf("bad decode: %+v", c)
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	in := []byte("id: a\nowner: b\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\nsecret_memory: leak\n")
	if _, err := Decode(in); err == nil {
		t.Fatal("expected error on unknown field")
	}
}
