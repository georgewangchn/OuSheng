package board

import "testing"

func TestConvergeEmptyBoardIsConverged(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	if err := b.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	result, err := b.Converge()
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if result.Status != StatusConverged {
		t.Fatalf("empty board should be CONVERGED (nothing to do), got %s", result.Status)
	}
}
