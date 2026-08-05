package ptr

import "testing"

func TestTo(t *testing.T) {
	p := To(42)
	if p == nil || *p != 42 {
		t.Fatalf("To(42) = %v, want a pointer to 42", p)
	}

	// Each call returns a pointer to its own copy.
	a, b := To("x"), To("x")
	if a == b {
		t.Fatal("two calls returned the same pointer")
	}
	*a = "changed"
	if *b != "x" {
		t.Fatal("pointers share storage")
	}
}
