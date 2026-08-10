package store

import (
	"testing"

	"ousheng/internal/card"
)

func newCard(id string) card.Card {
	return card.Card{ID: id, Owner: "backend", Task: "t", Status: card.Proposed, Version: 1,
		Contract: card.Contract{Kind: "http", Breaking: false, Interface: []any{}}}
}

func TestInitAndCommitAndList(t *testing.T) {
	s := Open(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	c := newCard("a")
	raw, _ := card.Encode(c)
	if err := s.commit("a", raw, "add a"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("bad list: %+v", got)
	}
	_, _, ok, err := s.Get("a")
	if err != nil || !ok {
		t.Fatalf("get a: ok=%v err=%v", ok, err)
	}
}
