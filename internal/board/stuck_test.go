package board

import (
	"strings"
	"testing"
	"time"

	"ousheng/internal/card"
)

func TestConvergeStuckTimePredicate(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	c := realisticCard("stuck-card")
	_, _ = b.WriteBoard(c, 0) // proposed, just created

	// Guarantee elapsed time exceeds threshold despite clock skew.
	time.Sleep(10 * time.Millisecond)

	t.Run("not stuck with zero threshold (disabled)", func(t *testing.T) {
		res, err := b.ConvergeWithOpts(ConvergeOptions{StuckAfter: 0})
		if err != nil {
			t.Fatal(err)
		}
		for _, blk := range res.Blockers {
			if containsStuck(blk) {
				t.Fatalf("unexpected stuck blocker with disabled threshold: %s", blk)
			}
		}
	})

	t.Run("stuck with 1ns threshold", func(t *testing.T) {
		res, err := b.ConvergeWithOpts(ConvergeOptions{StuckAfter: 1 * time.Nanosecond})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, blk := range res.Blockers {
			if containsStuck(blk) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected stuck blocker, got %+v", res)
		}
	})

	t.Run("verified cards not stuck", func(t *testing.T) {
		dir2 := t.TempDir()
		b2 := New(dir2)
		_ = b2.Init()
		c := realisticCard("verified-card")
		c, _ = b2.WriteBoard(c, 0)
		c, _ = transitionTo(b2, "verified-card", card.Agreed, c.Version)
		c, _ = transitionTo(b2, "verified-card", card.Live, c.Version)
		_, _ = transitionTo(b2, "verified-card", card.Verified, c.Version)

		res, _ := b2.ConvergeWithOpts(ConvergeOptions{StuckAfter: 1 * time.Nanosecond})
		if res.Status != StatusConverged {
			t.Fatalf("verified should not be stuck even with 1ns threshold: %s %+v", res.Status, res)
		}
	})
}

func containsStuck(s string) bool {
	return strings.HasPrefix(s, "stuck:")
}
