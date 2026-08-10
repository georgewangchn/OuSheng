package store

import "testing"

func TestGitLogWithZeroReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	out, err := s.GitLog("nonexistent", 0)
	if err != nil {
		t.Fatalf("GitLog n=0 should not error, got: %v", err)
	}
	if out != "" {
		t.Fatalf("GitLog n=0 should return empty, got: %q", out)
	}
}

func TestGitLogWithNegativeReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	out, err := s.GitLog("nonexistent", -1)
	if err != nil {
		t.Fatalf("GitLog n=-1 should not error, got: %v", err)
	}
	if out != "" {
		t.Fatalf("GitLog n=-1 should return empty, got: %q", out)
	}
}
