package applog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultPathUsesDBPathParent(t *testing.T) {
	tmp := t.TempDir()

	t.Setenv("LAZYAGENT_DB_PATH", filepath.Join(tmp, "custom", "observe.db"))
	t.Setenv("LAZYAGENT_DATA_DIR", filepath.Join(tmp, "ignored"))

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}

	want := filepath.Join(tmp, "custom", fileName)
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
}

func TestLoggerWritesErrorAndPanic(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, fileName)
	logger := NewForPath(path)

	logger.Error("refresh failed", errors.New("db busy"))
	report := logger.Panic("tui crashed", "boom")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		"[ERROR] refresh failed",
		"db busy",
		"[PANIC] tui crashed",
		"panic: boom",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log file missing %q in %q", want, text)
		}
	}

	if !strings.Contains(report, "panic: boom") {
		t.Fatalf("panic report = %q", report)
	}
	if !strings.Contains(report, "TestLoggerWritesErrorAndPanic") {
		t.Fatalf("panic report missing stack trace: %q", report)
	}
}
