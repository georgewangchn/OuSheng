package board

import (
	"strings"
	"testing"

	"ousheng/internal/card"
)

func TestWriteBoardMissingStatusGivesClearError(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	c := card.Card{
		ID:      "no-status",
		Owner:   "b",
		Task:    "t",
		Version: 0,
		Contract: card.Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{},
		},
	}

	_, err := b.WriteBoard(c, 0)
	if err == nil {
		t.Fatal("expected error for missing status")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Fatalf("error should mention status, got: %v", err)
	}
}
