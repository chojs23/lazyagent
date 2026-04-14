package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/model"
)

func TestIngestCodexFirstUserPromptSetsSessionSlug(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-1",
		"transcript_path": "/tmp/codex-1.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-1",
		"transcript_path": "/tmp/codex-1.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-1",
		"prompt":          "fix broken filtered paging\nwith a regression test",
		"timestamp":       float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "codex-1")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "fix broken filtered paging" {
		t.Fatalf("slug=%q, want first prompt line", session.Slug)
	}
	if session.Runtime != "codex" {
		t.Fatalf("runtime=%q, want codex", session.Runtime)
	}

	events, err := st.Read().ListEventsForSession(ctx, "codex-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[1].Subtype != "UserPromptSubmit" {
		t.Fatalf("subtype=%q, want UserPromptSubmit", events[1].Subtype)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(events[1].Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["prompt"] != "fix broken filtered paging\nwith a regression test" {
		t.Fatalf("prompt=%v, want original prompt preserved", payload["prompt"])
	}
}

func TestIngestCodexLaterPromptDoesNotOverwriteExistingSlug(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-1",
		"prompt":          "first codex prompt",
		"timestamp":       float64(1712700000500),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"session_id":      "codex-2",
		"transcript_path": "/tmp/codex-2.jsonl",
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"turn_id":         "turn-2",
		"prompt":          "second codex prompt",
		"timestamp":       float64(1712700001000),
	})
	if err != nil {
		t.Fatal(err)
	}

	session, err := st.Read().GetSessionByID(ctx, "codex-2")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil {
		t.Fatal("session not found")
	}
	if session.Slug != "first codex prompt" {
		t.Fatalf("slug=%q, want first codex prompt", session.Slug)
	}
}

func TestIngestCodexReplaysPatchEventsFromTranscript(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	transcriptPath := filepath.Join(t.TempDir(), "codex.jsonl")

	line := `{"type":"event_msg","timestamp":"2026-04-14T12:00:00Z","payload":{"type":"patch_apply_end","call_id":"patch-1","success":true,"stdout":"ok","changes":{"main.go":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n"}}}}`
	if err := os.WriteFile(transcriptPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-patch-1",
		"transcript_path": transcriptPath,
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := st.Read().ListEventsForSession(ctx, "codex-patch-1", model.EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}

	patch := events[1]
	if patch.ToolName != "apply_patch" {
		t.Fatalf("tool_name=%q, want apply_patch", patch.ToolName)
	}
	if patch.ToolUseID != "patch-1" {
		t.Fatalf("tool_use_id=%q, want patch-1", patch.ToolUseID)
	}
	if patch.Subtype != "PostToolUse" {
		t.Fatalf("subtype=%q, want PostToolUse", patch.Subtype)
	}
	if !strings.Contains(patch.Payload, "\"diff\":") {
		t.Fatalf("payload missing diff metadata: %q", patch.Payload)
	}
	if !strings.Contains(patch.Payload, "--- main.go") {
		t.Fatalf("payload missing unified diff: %q", patch.Payload)
	}
}

func TestIngestCodexLogsPatchReadErrors(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	logPath := filepath.Join(t.TempDir(), "lazyagent.log")
	applog.SetDefault(applog.NewForPath(logPath))
	t.Cleanup(func() {
		applog.SetDefault(nil)
	})

	_, err := IngestCodexEvent(ctx, st, map[string]any{
		"hook_event_name": "SessionStart",
		"session_id":      "codex-log-1",
		"transcript_path": filepath.Join(t.TempDir(), "missing.jsonl"),
		"cwd":             "/home/user/project",
		"model":           "gpt-5.4",
		"permission_mode": "default",
		"source":          "cli",
		"timestamp":       float64(1712700000000),
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(data)
	if !strings.Contains(logText, "Read Codex patch events failed") {
		t.Fatalf("log missing context: %q", logText)
	}
	if !strings.Contains(logText, "missing.jsonl") {
		t.Fatalf("log missing transcript path: %q", logText)
	}
}
