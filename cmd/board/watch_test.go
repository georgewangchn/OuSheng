package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ousheng/internal/board"
)

func TestWatchConvergeCancelsOnContext(t *testing.T) {
	dir := t.TempDir()
	b := board.New(dir)
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	var out, errb bytes.Buffer
	code := watchConverge(ctx, &out, &errb, dir, board.ConvergeOptions{}, 10*time.Millisecond)
	if code != 0 {
		t.Fatalf("expected exit code 0 on cancel, got %d err=%s", code, errb.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("IN_PROGRESS")) {
		t.Fatalf("expected output to contain IN_PROGRESS, got: %s", out.String())
	}
}

func TestWatchConvergeDetectsStatusChange(t *testing.T) {
	dir := t.TempDir()
	b := board.New(dir)
	_ = b.Init()

	cardFile := filepath.Join(dir, "auth.yaml")
	os.WriteFile(cardFile, []byte("id: auth\nowner: backend\ntask: t\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)
	run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &bytes.Buffer{}, &bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out, errb bytes.Buffer
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(cardFile, []byte("id: auth\nowner: backend\ntask: t\nstatus: agreed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)
		run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "1"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()

	code := watchConverge(ctx, &out, &errb, dir, board.ConvergeOptions{}, 10*time.Millisecond)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d err=%s", code, errb.String())
	}
}
