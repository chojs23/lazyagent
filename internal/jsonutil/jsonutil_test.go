package jsonutil

import (
	"testing"
	"time"
)

func TestTimestampMillisSupportsRFC3339String(t *testing.T) {
	ts := "2024-04-09T12:34:56.789Z"
	want := time.Date(2024, time.April, 9, 12, 34, 56, 789000000, time.UTC).UnixMilli()

	if got := TimestampMillis(ts); got != want {
		t.Fatalf("TimestampMillis(%q) = %d, want %d", ts, got, want)
	}
}

func TestTimestampMillisSupportsInt(t *testing.T) {
	if got := TimestampMillis(1712700000000); got != 1712700000000 {
		t.Fatalf("TimestampMillis(int) = %d, want %d", got, 1712700000000)
	}
}

func TestGetStringFormatsCommonJSONValues(t *testing.T) {
	payload := map[string]any{
		"intlike": float64(42),
		"float":   3.5,
		"bool":    true,
		"object":  map[string]any{"ok": true},
	}

	if got := GetString(payload, "intlike"); got != "42" {
		t.Fatalf("GetString(intlike) = %q, want %q", got, "42")
	}
	if got := GetString(payload, "float"); got != "3.5" {
		t.Fatalf("GetString(float) = %q, want %q", got, "3.5")
	}
	if got := GetString(payload, "bool"); got != "true" {
		t.Fatalf("GetString(bool) = %q, want %q", got, "true")
	}
	if got := GetString(payload, "object"); got != `{"ok":true}` {
		t.Fatalf("GetString(object) = %q, want %q", got, `{"ok":true}`)
	}
}
