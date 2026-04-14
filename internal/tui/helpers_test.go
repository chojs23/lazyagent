package tui

import "testing"

func TestTruncateLeavesShortUnicodeStringUntouched(t *testing.T) {
	input := "안녕🙂"
	if got := truncate(input, 4); got != input {
		t.Fatalf("truncate(%q, 4) = %q, want %q", input, got, input)
	}
}

func TestTruncateCutsByRunes(t *testing.T) {
	input := "가나다라마"
	if got := truncate(input, 4); got != "가..." {
		t.Fatalf("truncate(%q, 4) = %q, want %q", input, got, "가...")
	}
}

func TestTruncateHandlesEmojiWithoutBreakingUTF8(t *testing.T) {
	input := "🙂🙂🙂🙂"
	if got := truncate(input, 3); got != "🙂🙂🙂" {
		t.Fatalf("truncate(%q, 3) = %q, want %q", input, got, "🙂🙂🙂")
	}
}
