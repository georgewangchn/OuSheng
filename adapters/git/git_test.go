package git

import (
	"strings"
	"testing"

	"ousheng/internal/model"
	"ousheng/internal/testfix"
)

func TestEvidenceForCommit(t *testing.T) {
	dir := testfix.Setup(t)
	head, err := LatestCommit(dir)
	if err != nil {
		t.Fatal(err)
	}
	if head == "" {
		t.Fatal("no head commit")
	}
	ev, err := EvidenceForCommit(dir, head)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != model.EvidenceGitCommit || ev.Source != "git" || ev.Locator != head {
		t.Fatalf("evidence wrong: %+v", ev)
	}
	if ev.ObservedAt == "" || ev.Note == "" {
		t.Fatalf("observed_at / note should be filled: %+v", ev)
	}
}

func TestEvidenceForMissingCommit(t *testing.T) {
	dir := testfix.Setup(t)
	if _, err := EvidenceForCommit(dir, "deadbeef00"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}
