package store

import (
	"errors"
	"testing"
)

func TestWriteBumpsVersion(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	c := newCard("a")
	w, err := s.Write(c, 0, nil, "add")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.Version != 1 {
		t.Fatalf("want version 1, got %d", w.Version)
	}
}

func TestWriteConflictOnStaleExpected(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	c := newCard("a")
	_, _ = s.Write(c, 0, nil, "add")      // now version 1
	_, err := s.Write(c, 0, nil, "again") // stale expected
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}
