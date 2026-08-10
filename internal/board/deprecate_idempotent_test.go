package board

import "testing"

func TestDeprecateAlreadyDeprecatedIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	_ = b.Init()

	c := realisticCard("dep-idempotent")
	c, _ = b.WriteBoard(c, 0)

	c, _, _ = b.Deprecate("dep-idempotent", 1)
	if c.Status != "deprecated" {
		t.Fatalf("first deprecate: expected deprecated, got %s", c.Status)
	}

	c2, _, err := b.Deprecate("dep-idempotent", c.Version)
	if err != nil {
		t.Fatalf("second deprecate should be idempotent, got: %v", err)
	}
	if c2.Status != "deprecated" {
		t.Fatalf("second deprecate: expected deprecated, got %s", c2.Status)
	}
	if c2.Version != c.Version+1 {
		t.Fatalf("second deprecate should bump version, got %d want %d", c2.Version, c.Version+1)
	}
}
