package board

import "testing"

func TestConvergeSelfDependencyIsCycle(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	c := realisticCard("self-dep")
	c.DependsOn = []string{"self-dep"}
	_, _ = b.WriteBoard(c, 0)

	result, err := b.Converge()
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if result.Status != StatusStuck {
		t.Fatalf("self-dependency should be STUCK (cycle), got %s", result.Status)
	}
	if len(result.Cycle) == 0 {
		t.Fatalf("self-dependency should produce cycle, got: %v", result)
	}
}
