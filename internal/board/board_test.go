package board

import (
	"testing"

	"ousheng/internal/card"
)

func mk(id string) card.Card {
	return card.Card{ID: id, Owner: "backend", Task: "t", Status: card.Proposed, Version: 1,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestWriteReadFilter(t *testing.T) {
	b := New(t.TempDir())
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.WriteBoard(mk("a"), 0); err != nil {
		t.Fatalf("write a: %v", err)
	}
	got, err := b.ReadBoard(Scope{Owner: "backend"})
	if err != nil || len(got) != 1 {
		t.Fatalf("read: %v n=%d", err, len(got))
	}
	if none, _ := b.ReadBoard(Scope{Owner: "frontend"}); len(none) != 0 {
		t.Fatalf("expected 0 for frontend, got %d", len(none))
	}
}

func TestWriteRejectsIllegalTransition(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	a, _ := b.WriteBoard(mk("a"), 0) // proposed v1
	a.Status = card.Verified         // proposed->verified illegal
	if _, err := b.WriteBoard(a, 1); err == nil {
		t.Fatal("expected illegal transition error")
	}
}

func TestWriteRejectsInvalidCard(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := mk("a")
	c.Contract.Kind = "grpc" // not in enum
	if _, err := b.WriteBoard(c, 0); err == nil {
		t.Fatal("expected validation error")
	}
}
