package board

import (
	"testing"

	"ousheng/internal/card"
)

func TestDeprecateNotFound(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	_, _, err := b.Deprecate("nonexistent", 0)
	if err == nil {
		t.Fatal("expected error for missing card")
	}
}

func TestDeprecateSuccess(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	c, _ = b.WriteBoard(c, 0)
	c, _ = transitionTo(b, "auth", card.Agreed, c.Version)
	c, _ = transitionTo(b, "auth", card.Live, c.Version)
	_, _ = transitionTo(b, "auth", card.Verified, c.Version)

	written, dependents, err := b.Deprecate("auth", 4)
	if err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	if written.Status != card.Deprecated {
		t.Fatalf("expected deprecated status, got %s", written.Status)
	}
	if written.Version != 5 {
		t.Fatalf("expected version 5, got %d", written.Version)
	}
	if dependents != nil && len(dependents) != 0 {
		t.Fatalf("expected no dependents, got %v", dependents)
	}
}

func TestDeprecateWithDependents(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	dep := realisticCard("auth")
	dep, _ = b.WriteBoard(dep, 0)
	dep, _ = transitionTo(b, "auth", card.Agreed, dep.Version)
	dep, _ = transitionTo(b, "auth", card.Live, dep.Version)
	_, _ = transitionTo(b, "auth", card.Verified, dep.Version)

	consumer := realisticCard("frontend")
	consumer.DependsOn = []string{"auth"}
	consumer, _ = b.WriteBoard(consumer, 0)

	_, dependents, err := b.Deprecate("auth", 4)
	if err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	if len(dependents) != 1 || dependents[0] != "frontend" {
		t.Fatalf("expected dependent [frontend], got %v", dependents)
	}
}

func TestDeprecateCASConflict(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	_, _ = b.WriteBoard(c, 0)

	_, _, err := b.Deprecate("auth", 99)
	if err == nil {
		t.Fatal("expected CAS conflict")
	}
}

func TestContextNotFound(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	_, _, err := b.Context("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing card")
	}
}

func TestContextSuccess(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	_, _ = b.WriteBoard(c, 0)

	c, log, err := b.Context("auth")
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	if c.ID != "auth" {
		t.Fatalf("wrong card id: %s", c.ID)
	}
	if log == "" {
		t.Fatal("expected non-empty git log")
	}
}

func TestVerifySignaturesEmpty(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	problems, err := b.VerifySignatures()
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected no problems on empty board, got %v", problems)
	}
}

func TestVerifySignaturesUnsigned(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	_, _ = b.WriteBoard(c, 0)
	problems, _ := b.VerifySignatures()
	if len(problems) == 0 {
		t.Fatal("expected unsigned commit problems")
	}
}

func TestConvergeEmptyBoard(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	res, err := b.Converge()
	if err != nil {
		t.Fatalf("converge empty: %v", err)
	}
	if res.Status != StatusInProgress {
		t.Fatalf("empty board should be IN_PROGRESS, got %s", res.Status)
	}
}

func TestScopeKindFilter(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	c.Contract.Kind = "http"
	_, _ = b.WriteBoard(c, 0)
	c2 := realisticCard("db")
	c2.Contract.Kind = "lib"
	_, _ = b.WriteBoard(c2, 0)

	httpCards, _ := b.ReadBoard(Scope{Kind: "http"})
	if len(httpCards) != 1 || httpCards[0].ID != "auth" {
		t.Fatalf("kind filter failed: %+v", httpCards)
	}

	libCards, _ := b.ReadBoard(Scope{Kind: "lib"})
	if len(libCards) != 1 || libCards[0].ID != "db" {
		t.Fatalf("kind filter failed: %+v", libCards)
	}
}

func TestWriteBoardNewCardMustBeProposed(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	c.Status = card.Live
	_, err := b.WriteBoard(c, 0)
	if err == nil {
		t.Fatal("expected error: new card must start in proposed")
	}
}

func TestConvergeWithStuckAfterAndDeprecated(t *testing.T) {
	b := New(t.TempDir())
	_ = b.Init()
	c := realisticCard("auth")
	c, _ = b.WriteBoard(c, 0)
	c, _ = transitionTo(b, "auth", card.Agreed, c.Version)
	c, _ = transitionTo(b, "auth", card.Live, c.Version)
	c, _ = transitionTo(b, "auth", card.Verified, c.Version)
	_, _, _ = b.Deprecate("auth", c.Version)

	res, err := b.ConvergeWithOpts(ConvergeOptions{StuckAfter: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, blk := range res.Blockers {
		if containsStuck(blk) {
			t.Fatalf("deprecated card should not be stuck: %s", blk)
		}
	}
}
