package main

import "testing"

func TestVersionString(t *testing.T) {
	if got := versionString(); got != "ousheng board 0.1.0" {
		t.Fatalf("got %q", got)
	}
}
