package store

import (
	"os"
	"path/filepath"
	"testing"

	"ousheng/internal/card"
)

func TestStoreWriteRecoversFromStaleLock(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Simulate stale lock: process crashed, lock file left behind
	lockPath := filepath.Join(dir, ".board.lock")
	if err := os.WriteFile(lockPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("create stale lock: %v", err)
	}

	// Write should recover (detect stale lock) and succeed
	c := card.Card{
		ID:      "test-card",
		Owner:   "test",
		Task:    "test task",
		Status:  card.Proposed,
		Version: 0,
		Contract: card.Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{},
		},
	}
	_, err := s.Write(c, 0, card.Validate, "test write")
	if err != nil {
		t.Fatalf("write with stale lock should recover, got: %v", err)
	}

	// Verify card was written
	_, _, ok, err := s.Get("test-card")
	if err != nil || !ok {
		t.Fatalf("card not written after stale lock recovery: ok=%v err=%v", ok, err)
	}
}
