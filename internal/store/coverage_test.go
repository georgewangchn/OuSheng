package store

import (
	"os"
	"testing"
	"time"

	"ousheng/internal/card"
)

func TestLastCommitTime(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	c := newCard("a")
	if _, err := s.Write(c, 0, nil, "add"); err != nil {
		t.Fatal(err)
	}

	ct, err := s.LastCommitTime("a")
	if err != nil {
		t.Fatalf("LastCommitTime: %v", err)
	}
	if ct.IsZero() {
		t.Fatal("commit time is zero")
	}
	if time.Since(ct) > 10*time.Second {
		t.Fatalf("commit time too old: %v", ct)
	}
}

func TestLastCommitTimeMissingCard(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	_, err := s.LastCommitTime("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing card")
	}
}

func TestGitLog(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	c := newCard("a")
	_, _ = s.Write(c, 0, nil, "first")
	c.Version = 1
	_, _ = s.Write(c, 1, nil, "second")

	log, err := s.GitLog("a", 10)
	if err != nil {
		t.Fatalf("GitLog: %v", err)
	}
	if log == "" {
		t.Fatal("expected non-empty log")
	}
	if !containsStr(log, "second") && !containsStr(log, "first") {
		t.Fatalf("log missing commit messages: %s", log)
	}
}

func TestVerifySignaturesAllUnsigned(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	c := newCard("a")
	_, _ = s.Write(c, 0, nil, "add")

	problems, err := s.VerifySignatures()
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(problems) == 0 {
		t.Fatal("expected unsigned commit problems")
	}
	if !containsStr(problems[0], "unsigned") {
		t.Fatalf("expected 'unsigned' in problem, got: %s", problems[0])
	}
}

func TestVerifySignaturesEmpty(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	problems, err := s.VerifySignatures()
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected no problems on empty board, got: %v", problems)
	}
}

func TestOpenWithSignEnv(t *testing.T) {
	os.Setenv("OUSHENG_SIGN_COMMITS", "1")
	defer os.Unsetenv("OUSHENG_SIGN_COMMITS")
	s := Open(t.TempDir())
	if !s.SignCommits {
		t.Fatal("SignCommits should be true when env set")
	}
}

func TestOpenWithoutSignEnv(t *testing.T) {
	os.Unsetenv("OUSHENG_SIGN_COMMITS")
	s := Open(t.TempDir())
	if s.SignCommits {
		t.Fatal("SignCommits should be false when env unset")
	}
}

func TestReInitIdempotent(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := s.Init(); err != nil {
		t.Fatalf("second init: %v", err)
	}
	c := newCard("a")
	if _, err := s.Write(c, 0, nil, "add"); err != nil {
		t.Fatalf("write after re-init: %v", err)
	}
}

func TestWriteWithValidation(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	c := newCard("a")
	c.Owner = ""
	_, err := s.Write(c, 0, card.Validate, "add")
	if err == nil {
		t.Fatal("expected validation error for empty owner")
	}
}

func TestListEmpty(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	cards, err := s.List()
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected 0 cards, got %d", len(cards))
	}
}

func TestGetInvalidID(t *testing.T) {
	s := Open(t.TempDir())
	_ = s.Init()
	_, _, _, err := s.Get("UPPERCASE")
	if err == nil {
		t.Fatal("expected error for invalid id")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStrHelper(s, sub))
}

func containsStrHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
