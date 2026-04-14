package textutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", "alpha", "beta"); got != "alpha" {
		t.Fatalf("FirstNonEmpty() = %q, want %q", got, "alpha")
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("hello\nworld"); got != "hello" {
		t.Fatalf("FirstLine() = %q, want %q", got, "hello")
	}
}
