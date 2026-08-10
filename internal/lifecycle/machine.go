package lifecycle

import "ousheng/internal/card"

var edges = map[card.Status]map[card.Status]bool{
	card.Proposed: {card.Agreed: true, card.Deprecated: true},
	card.Agreed:   {card.Live: true, card.Deprecated: true},
	card.Live:     {card.Verified: true, card.Deprecated: true},
	card.Verified: {card.Live: true, card.Deprecated: true},
}

func CanTransition(from, to card.Status) bool {
	if from == to {
		return true
	}
	return edges[from][to]
}
