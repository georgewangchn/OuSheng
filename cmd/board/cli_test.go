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

func TestCLIDeprecate(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer

	run([]string{"init", dir}, &out, &errb)
	cardFile := filepath.Join(dir, "a.yaml")
	os.WriteFile(cardFile, []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)
	run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &out, &errb)

	out.Reset()
	errb.Reset()
	code := run([]string{"deprecate", "--dir", dir, "--expect", "1", "a"}, &out, &errb)
	if code != 0 {
		t.Fatalf("deprecate code=%d err=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "deprecated a version 2") {
		t.Fatalf("deprecate output: %s", out.String())
	}
}

func TestCLIDeprecateNotFound(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	run([]string{"init", dir}, &out, &errb)

	out.Reset()
	errb.Reset()
	code := run([]string{"deprecate", "--dir", dir, "--expect", "0", "nonexistent"}, &out, &errb)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing card")
	}
}

func TestCLIContext(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer

	run([]string{"init", dir}, &out, &errb)
	cardFile := filepath.Join(dir, "a.yaml")
	os.WriteFile(cardFile, []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)
	run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &out, &errb)

	out.Reset()
	errb.Reset()
	code := run([]string{"context", "--dir", dir, "a"}, &out, &errb)
	if code != 0 {
		t.Fatalf("context code=%d err=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "--- card ---") {
		t.Fatalf("context missing card section: %s", out.String())
	}
	if !strings.Contains(out.String(), "--- history") {
		t.Fatalf("context missing history section: %s", out.String())
	}
}

func TestCLIContextNotFound(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	run([]string{"init", dir}, &out, &errb)

	out.Reset()
	errb.Reset()
	code := run([]string{"context", "--dir", dir, "nonexistent"}, &out, &errb)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing card")
	}
}

func TestCLIVerify(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer

	run([]string{"init", dir}, &out, &errb)
	cardFile := filepath.Join(dir, "a.yaml")
	os.WriteFile(cardFile, []byte("id: a\nowner: backend\ntask: t\nstatus: proposed\nversion: 0\ncontract:\n  kind: http\n  breaking: false\n  interface: []\n"), 0o644)
	run([]string{"write", "--dir", dir, "--file", cardFile, "--expect", "0"}, &out, &errb)

	out.Reset()
	errb.Reset()
	code := run([]string{"verify", "--dir", dir}, &out, &errb)
	if code == 0 {
		t.Fatalf("expected non-zero (unsigned commits) but got 0: %s", out.String())
	}
	if !strings.Contains(out.String(), "unsigned commit") {
		t.Fatalf("verify output missing unsigned: %s", out.String())
	}
}

func TestCLIVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"version"}, &out, &errb)
	if code != 0 {
		t.Fatalf("version code=%d", code)
	}
	if !strings.Contains(out.String(), "ousheng board") {
		t.Fatalf("version output: %s", out.String())
	}
}
