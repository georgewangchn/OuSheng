package board

import (
	"ousheng/internal/card"
	"ousheng/internal/graph"
)

type ConvergenceStatus string

const (
	StatusConverged   ConvergenceStatus = "CONVERGED"
	StatusInProgress  ConvergenceStatus = "IN_PROGRESS"
	StatusStuck       ConvergenceStatus = "STUCK"
)

type Convergence struct {
	Status   ConvergenceStatus
	Blockers []string
	Cycle    []string
}

func (b *Board) Converge() (Convergence, error) {
	cards, err := b.ReadBoard(Scope{})
	if err != nil {
		return Convergence{}, err
	}
	byID := map[string]card.Card{}
	deps := map[string][]string{}
	for _, c := range cards {
		byID[c.ID] = c
		deps[c.ID] = c.DependsOn
	}
	if cyc := graph.FindCycle(deps); cyc != nil {
		return Convergence{Status: StatusStuck, Cycle: cyc}, nil
	}

	var blockers []string
	allVerified := true
	hasProposed := false
	for _, c := range cards {
		if c.Status == card.Proposed {
			hasProposed = true
		}
		if c.Status != card.Verified {
			allVerified = false
		}
		if c.Status == card.Verified {
			if c.Evidence == nil {
				blockers = append(blockers, c.ID+": verified without evidence")
			}
			for _, d := range c.DependsOn {
				dep, ok := byID[d]
				if !ok {
					blockers = append(blockers, c.ID+": dangling dependency "+d)
					continue
				}
				if dep.Status == card.Deprecated {
					blockers = append(blockers, c.ID+": broken, depends on deprecated "+d)
				}
			}
		}
	}
	if len(blockers) > 0 {
		return Convergence{Status: StatusStuck, Blockers: blockers}, nil
	}
	if len(cards) > 0 && allVerified && !hasProposed {
		return Convergence{Status: StatusConverged}, nil
	}
	return Convergence{Status: StatusInProgress}, nil
}
