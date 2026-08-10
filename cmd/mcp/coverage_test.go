package main

import (
	"context"
	"strings"
	"testing"
)

func TestConvergeHandlerWithStuckAfter(t *testing.T) {
	dir := setupTestBoard(t)
	_, out, err := converge(context.Background(), nil, ConvergeInput{
		Path:      dir,
		StuckAfter: "1ns",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "STUCK" {
		t.Fatalf("expected STUCK with 1ns threshold, got %s", out.Status)
	}
	found := false
	for _, b := range out.Blockers {
		if strings.HasPrefix(b, "stuck:") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stuck blocker, got %v", out.Blockers)
	}
}

func TestConvergeHandlerInvalidStuckAfter(t *testing.T) {
	dir := setupTestBoard(t)
	_, _, err := converge(context.Background(), nil, ConvergeInput{
		Path:      dir,
		StuckAfter: "not-a-duration",
	})
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestWriteBoardHandlerNewCard(t *testing.T) {
	dir := t.TempDir()
	b := newBoard(dir)
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}
	yaml := "id: new-api\nowner: backend\ntask: 新接口\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"
	_, out, err := writeBoard(context.Background(), nil, WriteBoardInput{
		Path:            dir,
		CardYAML:        yaml,
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("write new card: %v", err)
	}
	if out.ID != "new-api" || out.Version != 1 {
		t.Fatalf("expected new-api v1, got %+v", out)
	}
}

func TestWriteBoardHandlerBreakingWithoutAck(t *testing.T) {
	dir := t.TempDir()
	b := newBoard(dir)
	_ = b.Init()
	yaml := "id: breaking\nowner: backend\ntask: 破坏性\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: true\n  interface: []\n"
	_, _, err := writeBoard(context.Background(), nil, WriteBoardInput{
		Path:     dir,
		CardYAML: yaml,
	})
	if err == nil {
		t.Fatal("expected rejection for breaking without human_ack")
	}
}

func TestConvergeHandlerBadPath(t *testing.T) {
	_, _, err := converge(context.Background(), nil, ConvergeInput{Path: "/nonexistent/xyz"})
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}
