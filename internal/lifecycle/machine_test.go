package lifecycle

import (
	"testing"

	"ousheng/internal/card"
)

func TestLegalTransitions(t *testing.T) {
	ok := [][2]card.Status{
		{card.Proposed, card.Agreed}, {card.Agreed, card.Live},
		{card.Live, card.Verified}, {card.Verified, card.Live},
		{card.Live, card.Deprecated}, {card.Verified, card.Deprecated},
		{card.Live, card.Live},
	}
	for _, e := range ok {
		if !CanTransition(e[0], e[1]) {
			t.Errorf("expected %s->%s legal", e[0], e[1])
		}
	}
}

func TestIllegalTransitions(t *testing.T) {
	bad := [][2]card.Status{
		{card.Proposed, card.Verified}, {card.Deprecated, card.Live},
		{card.Proposed, card.Live},
	}
	for _, e := range bad {
		if CanTransition(e[0], e[1]) {
			t.Errorf("expected %s->%s illegal", e[0], e[1])
		}
	}
}
