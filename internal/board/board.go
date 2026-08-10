package board

import (
	"fmt"

	"ousheng/internal/card"
	"ousheng/internal/lifecycle"
	"ousheng/internal/store"
)

type Board struct{ store *store.Store }

func New(dir string) *Board { return &Board{store: store.Open(dir)} }

func (b *Board) Init() error { return b.store.Init() }

type Scope struct{ ID, Owner, Status, Kind string }

func (s Scope) match(c card.Card) bool {
	if s.ID != "" && c.ID != s.ID {
		return false
	}
	if s.Owner != "" && c.Owner != s.Owner {
		return false
	}
	if s.Status != "" && string(c.Status) != s.Status {
		return false
	}
	if s.Kind != "" && c.Contract.Kind != s.Kind {
		return false
	}
	return true
}

func (b *Board) ReadBoard(s Scope) ([]card.Card, error) {
	all, err := b.store.List()
	if err != nil {
		return nil, err
	}
	var out []card.Card
	for _, c := range all {
		if s.match(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (b *Board) WriteBoard(c card.Card, expectedVersion int) (card.Card, error) {
	prev, _, ok, err := b.store.Get(c.ID)
	if err != nil {
		return card.Card{}, err
	}
	if ok {
		if !lifecycle.CanTransition(prev.Status, c.Status) {
			return card.Card{}, fmt.Errorf("illegal transition %s->%s", prev.Status, c.Status)
		}
	} else if c.Status != card.Proposed {
		if c.Status == "" {
			return card.Card{}, fmt.Errorf("new card must start in proposed (status field is empty)")
		}
		return card.Card{}, fmt.Errorf("new card must start in proposed (got %s)", c.Status)
	}
	msg := fmt.Sprintf("board: write %s -> %s", c.ID, c.Status)
	return b.store.Write(c, expectedVersion, card.Validate, msg)
}

func (b *Board) Deprecate(id string, expectedVersion int) (card.Card, []string, error) {
	cur, _, ok, err := b.store.Get(id)
	if err != nil {
		return card.Card{}, nil, err
	}
	if !ok {
		return card.Card{}, nil, fmt.Errorf("card %s not found", id)
	}
	cur.Status = card.Deprecated
	written, err := b.WriteBoard(cur, expectedVersion)
	if err != nil {
		return card.Card{}, nil, err
	}
	all, err := b.ReadBoard(Scope{})
	if err != nil {
		return written, nil, nil
	}
	var dependents []string
	for _, c := range all {
		for _, d := range c.DependsOn {
			if d == id {
				dependents = append(dependents, c.ID)
				break
			}
		}
	}
	return written, dependents, nil
}

func (b *Board) Context(id string) (card.Card, string, error) {
	c, _, ok, err := b.store.Get(id)
	if err != nil {
		return card.Card{}, "", err
	}
	if !ok {
		return card.Card{}, "", fmt.Errorf("card %s not found", id)
	}
	log, err := b.store.GitLog(id, 10)
	if err != nil {
		return c, "", nil
	}
	return c, log, nil
}

func (b *Board) VerifySignatures() ([]string, error) {
	return b.store.VerifySignatures()
}
