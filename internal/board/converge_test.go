package board

import (
	"testing"

	"ousheng/internal/card"
)

func verified(id string) card.Card {
	return card.Card{ID: id, Owner: "o", Task: "t", Status: card.Verified, Version: 1,
		Contract: card.Contract{Kind: "http", Interface: []any{}},
		Evidence: &card.Evidence{Probe: "p", PassedAtCommit: "c", By: "frontend"}}
}

func TestConvergeConverged(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	// proposed -> agreed -> live -> verified
	c := mk("a")
	c, _ = b.WriteBoard(c, 0)
	c.Status = card.Agreed
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Live
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Verified
	c.Evidence = &card.Evidence{Probe: "p", PassedAtCommit: "x", By: "frontend"}
	if _, err := b.WriteBoard(c, c.Version); err != nil {
		t.Fatalf("to verified: %v", err)
	}
	got, err := b.Converge()
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "CONVERGED" {
		t.Fatalf("want CONVERGED, got %s %+v", got.Status, got)
	}
}

func TestConvergeInProgressWhenProposed(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	_, _ = b.WriteBoard(mk("a"), 0) // proposed
	got, _ := b.Converge()
	if got.Status != "IN_PROGRESS" {
		t.Fatalf("want IN_PROGRESS, got %s", got.Status)
	}
}

func TestConvergeStuckOnDanglingDep(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := mk("a")
	c, _ = b.WriteBoard(c, 0)
	c.Status = card.Agreed
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Live
	c, _ = b.WriteBoard(c, c.Version)
	c.Status = card.Verified
	c.DependsOn = []string{"ghost"}
	c.Evidence = &card.Evidence{Probe: "p", PassedAtCommit: "x", By: "frontend"}
	if _, err := b.WriteBoard(c, c.Version); err != nil {
		t.Fatalf("to verified: %v", err)
	}
	got, _ := b.Converge()
	if got.Status != "STUCK" {
		t.Fatalf("want STUCK for dangling dep, got %s", got.Status)
	}
}
