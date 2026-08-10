package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIInitWriteReadConverge(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer

	if code := run([]string{"init", dir}, &out, &errb); code != 0 {
		t.Fatalf("init code=%d err=%s", code, errb.String())
	}

	cardFile := filepath.Join(dir, "a.yaml")
	os.WriteFile(cardFile, []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 1\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)

	out.Reset()
	errb.Reset()
	if code := run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &out, &errb); code != 0 {
		t.Fatalf("write code=%d err=%s", code, errb.String())
	}

	out.Reset()
	if code := run([]string{"read", "--dir", dir, "--owner", "backend"}, &out, &errb); code != 0 {
		t.Fatalf("read code=%d", code)
	}
	if !strings.Contains(out.String(), "id: a") {
		t.Fatalf("read output missing card: %s", out.String())
	}

	out.Reset()
	if code := run([]string{"converge", "--dir", dir}, &out, &errb); code != 0 {
		t.Fatalf("converge code=%d", code)
	}
	if !strings.Contains(out.String(), "IN_PROGRESS") {
		t.Fatalf("converge output: %s", out.String())
	}
}
