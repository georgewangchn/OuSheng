package main

import (
	"context"
	"strings"
	"testing"

	"ousheng/internal/card"
)

func mkCard(id string) card.Card {
	return card.Card{
		ID:      id,
		Owner:   "backend",
		Task:    "提供登录接口",
		Status:  card.Proposed,
		Version: 0,
		Contract: card.Contract{
			Kind:     "http",
			Breaking: false,
			Interface: []any{
				map[string]any{
					"method":   "POST",
					"path":     "/login",
					"behavior": "有效凭证返回 token",
				},
			},
		},
	}
}

func TestReadBoardHandler(t *testing.T) {
	dir := setupTestBoard(t)

	t.Run("all cards", func(t *testing.T) {
		_, out, err := readBoard(context.Background(), nil, ReadBoardInput{Path: dir})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Cards) != 2 {
			t.Fatalf("want 2 cards, got %d", len(out.Cards))
		}
	})

	t.Run("filter by owner", func(t *testing.T) {
		_, out, err := readBoard(context.Background(), nil, ReadBoardInput{Path: dir, Owner: "backend"})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Cards) != 1 || out.Cards[0].ID != "auth-api" {
			t.Fatalf("want 1 card auth-api, got %+v", out.Cards)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		_, out, err := readBoard(context.Background(), nil, ReadBoardInput{Path: dir, Status: "proposed"})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Cards) != 2 {
			t.Fatalf("want 2 proposed cards, got %d", len(out.Cards))
		}
	})
}

func TestWriteBoardHandler(t *testing.T) {
	dir := setupTestBoard(t)

	t.Run("update existing card", func(t *testing.T) {
		yaml := "id: auth-api\nowner: backend\ntask: 提供登录接口\nstatus: agreed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"
		_, out, err := writeBoard(context.Background(), nil, WriteBoardInput{
			Path:            dir,
			CardYAML:        yaml,
			ExpectedVersion: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if out.Version != 2 {
			t.Fatalf("want version 2, got %d", out.Version)
		}
		if out.Status != "agreed" {
			t.Fatalf("want status agreed, got %s", out.Status)
		}
	})

	t.Run("CAS conflict", func(t *testing.T) {
		yaml := "id: auth-api\nowner: backend\ntask: 提供登录接口\nstatus: agreed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"
		_, _, err := writeBoard(context.Background(), nil, WriteBoardInput{
			Path:            dir,
			CardYAML:        yaml,
			ExpectedVersion: 0,
		})
		if err == nil {
			t.Fatal("expected CAS conflict error")
		}
		if !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("expected conflict error, got: %v", err)
		}
	})

	t.Run("invalid card yaml", func(t *testing.T) {
		_, _, err := writeBoard(context.Background(), nil, WriteBoardInput{
			Path:     dir,
			CardYAML: "not: valid: yaml: {{{",
		})
		if err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestConvergeHandler(t *testing.T) {
	dir := setupTestBoard(t)

	_, out, err := converge(context.Background(), nil, ConvergeInput{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "IN_PROGRESS" {
		t.Fatalf("want IN_PROGRESS, got %s", out.Status)
	}
}

func setupTestBoard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	b := newBoard(dir)
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}
	c1 := mkCard("auth-api")
	if _, err := b.WriteBoard(c1, 0); err != nil {
		t.Fatal(err)
	}
	c2 := mkCard("user-db")
	c2.Owner = "infra"
	if _, err := b.WriteBoard(c2, 0); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadBoardHandlerBadPath(t *testing.T) {
	_, _, err := readBoard(context.Background(), nil, ReadBoardInput{Path: "/nonexistent/path/xyz"})
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}
