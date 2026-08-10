package graph

import "testing"

func TestNoCycle(t *testing.T) {
	deps := map[string][]string{"a": {"b"}, "b": {"c"}, "c": nil}
	if got := FindCycle(deps); got != nil {
		t.Fatalf("expected no cycle, got %v", got)
	}
}

func TestCycle(t *testing.T) {
	deps := map[string][]string{"a": {"b"}, "b": {"a"}}
	if got := FindCycle(deps); got == nil {
		t.Fatal("expected cycle")
	}
}
