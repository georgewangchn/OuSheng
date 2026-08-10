package store

import (
	"sync"
	"sync/atomic"
	"testing"

	"ousheng/internal/card"
)

func TestConcurrentWritesSameCardCAS(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	base := card.Card{
		ID: "concurrent", Owner: "o", Task: "t", Status: card.Proposed, Version: 0,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}},
	}
	_, err := s.Write(base, 0, card.Validate, "initial")
	if err != nil {
		t.Fatalf("initial write: %v", err)
	}

	var wg sync.WaitGroup
	var wins int32
	var conflicts int32
	var otherErrs int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := base
			c.Status = card.Agreed
			c.Version = 0
			_, werr := s.Write(c, 1, card.Validate, "concurrent write")
			if werr == nil {
				atomic.AddInt32(&wins, 1)
			} else if werr == ErrConflict {
				atomic.AddInt32(&conflicts, 1)
			} else {
				atomic.AddInt32(&otherErrs, 1)
				t.Errorf("unexpected error: %v", werr)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("expected exactly 1 win, got wins=%d conflicts=%d otherErrs=%d", wins, conflicts, otherErrs)
	}
	if conflicts != 9 {
		t.Fatalf("expected 9 conflicts, got wins=%d conflicts=%d otherErrs=%d", wins, conflicts, otherErrs)
	}
	if otherErrs != 0 {
		t.Fatalf("expected 0 other errors, got wins=%d conflicts=%d otherErrs=%d", wins, conflicts, otherErrs)
	}

	final, _, _, err := s.Get("concurrent")
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if final.Version != 2 {
		t.Fatalf("expected version 2, got %d", final.Version)
	}
}
