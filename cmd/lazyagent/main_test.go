package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	internalversion "github.com/chojs23/lazyagent/internal/version"
)

func TestRunVersionJSONOutputsReleaseMetadata(t *testing.T) {
	originalVersion := internalversion.Version
	originalCommit := internalversion.Commit
	originalBuildDate := internalversion.BuildDate
	internalversion.Version = "v1.2.3"
	internalversion.Commit = "abcdef0123456789"
	internalversion.BuildDate = "2026-04-12T10:00:00Z"
	t.Cleanup(func() {
		internalversion.Version = originalVersion
		internalversion.Commit = originalCommit
		internalversion.BuildDate = originalBuildDate
	})

	output := captureStdout(t, func() {
		if err := runVersion([]string{"--json"}); err != nil {
			t.Fatalf("runVersion: %v", err)
		}
	})

	for _, want := range []string{"\"version\": \"v1.2.3\"", "\"commit\": \"abcdef0123456789\"", "\"build_date\": \"2026-04-12T10:00:00Z\""} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in %q", want, output)
		}
	}
}

func TestPrintUsageIncludesVersionCommand(t *testing.T) {
	output := captureStdout(t, printUsage)
	if !strings.Contains(output, "version [--json]") {
		t.Fatalf("expected version command in usage, got %q", output)
	}
}

func TestInitClaudeRegistersPostToolUseFailureHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := initClaude(); err != nil {
		t.Fatalf("initClaude: %v", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing or wrong type: %#v", settings["hooks"])
	}

	entries, ok := hooks["PostToolUseFailure"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("PostToolUseFailure hook missing: %#v", hooks["PostToolUseFailure"])
	}

	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("hook entry has wrong type: %#v", entries[0])
	}

	hookList, ok := entry["hooks"].([]any)
	if !ok || len(hookList) == 0 {
		t.Fatalf("nested hooks missing: %#v", entry["hooks"])
	}

	hook, ok := hookList[0].(map[string]any)
	if !ok {
		t.Fatalf("nested hook has wrong type: %#v", hookList[0])
	}

	if got := hook["command"]; got != "lazyagent ingest --runtime claude" {
		t.Fatalf("command = %#v, want %q", got, "lazyagent ingest --runtime claude")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = writer

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdout = originalStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}

	return buf.String()
}
