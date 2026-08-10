package board

import (
	"strings"
	"testing"

	"ousheng/internal/card"
)

func TestConvergeBrokenDepAnyStatusIsStuck(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	auth := card.Card{
		ID: "auth-api", Owner: "b", Task: "auth", Status: card.Proposed, Version: 0,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}},
	}
	auth, _ = b.WriteBoard(auth, 0)

	frontend := card.Card{
		ID: "frontend", Owner: "f", Task: "fe", Status: card.Proposed, Version: 0,
		DependsOn: []string{"auth-api"},
		Contract:  card.Contract{Kind: "http", Breaking: false, Interface: []any{}},
	}
	frontend, _ = b.WriteBoard(frontend, 0)

	auth.Status = card.Deprecated
	auth, _ = b.WriteBoard(auth, 1)

	result, err := b.Converge()
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if result.Status != StatusStuck {
		t.Fatalf("proposed card depending on deprecated should be STUCK, got %s (blockers: %v)", result.Status, result.Blockers)
	}
	found := false
	for _, b := range result.Blockers {
		if strings.Contains(b, "frontend") && strings.Contains(b, "deprecated") {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocker should mention frontend + deprecated, got: %v", result.Blockers)
	}
}
