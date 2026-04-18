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

func TestInstallCodexHooksPreservesNonLazyagentHooks(t *testing.T) {
	hooksPath := filepath.Join(t.TempDir(), "hooks.json")
	initial := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{map[string]any{"type": "command", "command": "other-tool ingest"}},
				},
				map[string]any{
					"hooks": []any{map[string]any{"type": "command", "command": "lazyagent ingest --runtime codex --quiet"}},
				},
			},
		},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("marshal initial hooks: %v", err)
	}
	if err := os.WriteFile(hooksPath, data, 0o644); err != nil {
		t.Fatalf("write hooks file: %v", err)
	}

	if err := installCodexHooks(hooksPath); err != nil {
		t.Fatalf("installCodexHooks: %v", err)
	}

	data, err = os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal hooks: %v", err)
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing or wrong type: %#v", root["hooks"])
	}
	entries, ok := hooks["SessionStart"].([]any)
	if !ok {
		t.Fatalf("SessionStart hooks wrong type: %#v", hooks["SessionStart"])
	}
	if len(entries) != 2 {
		t.Fatalf("SessionStart hooks len = %d, want 2", len(entries))
	}

	var commands []string
	for _, entryRaw := range entries {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			t.Fatalf("entry wrong type: %#v", entryRaw)
		}
		hookList, ok := entry["hooks"].([]any)
		if !ok || len(hookList) == 0 {
			t.Fatalf("nested hooks missing: %#v", entry["hooks"])
		}
		hook, ok := hookList[0].(map[string]any)
		if !ok {
			t.Fatalf("hook wrong type: %#v", hookList[0])
		}
		commands = append(commands, hook["command"].(string))
	}

	if !strings.Contains(commands[0], "other-tool ingest") && !strings.Contains(commands[1], "other-tool ingest") {
		t.Fatalf("expected non-lazyagent hook to be preserved, got %v", commands)
	}

	var lazyagentCount int
	for _, cmd := range commands {
		if cmd == "lazyagent ingest --runtime codex --quiet" {
			lazyagentCount++
		}
	}
	if lazyagentCount != 1 {
		t.Fatalf("expected exactly one managed codex hook, got %d in %v", lazyagentCount, commands)
	}
}

func TestIngestRuntimeEventUnsupportedRuntime(t *testing.T) {
	_, err := ingestRuntimeEvent(t.Context(), nil, "unknown", map[string]any{})
	if err == nil {
		t.Fatal("expected unsupported runtime error")
	}
	if err.Error() != `unsupported runtime "unknown"` {
		t.Fatalf("error = %q, want %q", err.Error(), `unsupported runtime "unknown"`)
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
