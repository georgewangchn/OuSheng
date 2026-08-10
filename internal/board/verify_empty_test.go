package board

import "testing"

func TestVerifySignaturesEmptyBoardReturnsNoProblems(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	problems, err := b.VerifySignatures()
	if err != nil {
		t.Fatalf("empty board verify should not error, got: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("empty board should have 0 problems, got: %v", problems)
	}
}
