package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSessionMetaThreadSpawn(t *testing.T) {
	content := `{"timestamp":"2026-04-12T10:19:58.649Z","type":"session_meta","payload":{"id":"019d8134-8f08-71d3-b158-852f96d7a2f5","forked_from_id":"019d8133-9c57-7741-8024-107194f4be77","timestamp":"2026-04-12T10:19:58.626Z","cwd":"/home/user/project","originator":"codex-tui","cli_version":"0.120.0","source":{"subagent":{"thread_spawn":{"parent_thread_id":"019d8133-9c57-7741-8024-107194f4be77","depth":1,"agent_path":null,"agent_nickname":"Cicero","agent_role":"code_mapper"}}},"agent_nickname":"Cicero","agent_role":"code_mapper","model_provider":"openai"}}
{"timestamp":"2026-04-12T10:20:00.000Z","type":"response_item","payload":{}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "019d8133-9c57-7741-8024-107194f4be77" {
		t.Fatalf("got parentSessionID=%q", meta.ParentSessionID)
	}
	if meta.AgentNickname != "Cicero" {
		t.Fatalf("got agentNickname=%q", meta.AgentNickname)
	}
	if meta.AgentRole != "code_mapper" {
		t.Fatalf("got agentRole=%q", meta.AgentRole)
	}
}

func TestReadSessionMetaReviewSubagent(t *testing.T) {
	content := `{"timestamp":"2026-02-28T13:35:05.061Z","type":"session_meta","payload":{"id":"019ca475-b112-7ef3-8ceb-417cac25cd5b","forked_from_id":"019ca475-a9b7-7272-b373-bb9f2221efbe","timestamp":"2026-02-28T13:35:02.420Z","cwd":"/home/user/project","originator":"codex_cli_rs","cli_version":"0.106.0","source":{"subagent":"review"},"model_provider":"openai"}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "019ca475-a9b7-7272-b373-bb9f2221efbe" {
		t.Fatalf("got parentSessionID=%q", meta.ParentSessionID)
	}
	if meta.AgentNickname != "" {
		t.Fatalf("got agentNickname=%q, expected empty", meta.AgentNickname)
	}
	if meta.AgentRole != "" {
		t.Fatalf("got agentRole=%q, expected empty", meta.AgentRole)
	}
}

func TestReadSessionMetaRootSession(t *testing.T) {
	content := `{"timestamp":"2026-02-28T08:26:40.646Z","type":"session_meta","payload":{"id":"019ca35b-6460-7892-b6be-3c399d927ada","timestamp":"2026-02-28T08:26:40.632Z","cwd":"/home/user/project","originator":"codex_cli_rs","cli_version":"0.98.0","source":"cli","model_provider":"openai"}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "" {
		t.Fatalf("root session should have empty parentSessionID, got %q", meta.ParentSessionID)
	}
	if meta.AgentNickname != "" {
		t.Fatalf("root session should have empty agentNickname, got %q", meta.AgentNickname)
	}
}

func TestReadSessionMetaMissingFile(t *testing.T) {
	meta := ReadSessionMeta("/nonexistent/path/to/file.jsonl")
	if meta.ParentSessionID != "" {
		t.Fatalf("missing file should return empty meta, got parentSessionID=%q", meta.ParentSessionID)
	}
}

func TestReadSessionMetaEmptyPath(t *testing.T) {
	meta := ReadSessionMeta("")
	if meta.ParentSessionID != "" {
		t.Fatalf("empty path should return empty meta, got parentSessionID=%q", meta.ParentSessionID)
	}
}

func TestReadSessionMetaEmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	meta := ReadSessionMeta(path)
	if meta.ParentSessionID != "" {
		t.Fatalf("empty file should return empty meta, got parentSessionID=%q", meta.ParentSessionID)
	}
}

func TestReadSessionMetaInvalidJSON(t *testing.T) {
	path := writeTempFile(t, "not valid json\n")
	meta := ReadSessionMeta(path)
	if meta.ParentSessionID != "" {
		t.Fatalf("invalid JSON should return empty meta, got parentSessionID=%q", meta.ParentSessionID)
	}
}

func TestReadSessionMetaWrongType(t *testing.T) {
	content := `{"timestamp":"2026-04-12T10:20:00.000Z","type":"response_item","payload":{"forked_from_id":"should-be-ignored"}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)
	if meta.ParentSessionID != "" {
		t.Fatalf("non session_meta type should return empty meta, got parentSessionID=%q", meta.ParentSessionID)
	}
}

func TestReadSessionMetaFallbackToThreadSpawn(t *testing.T) {
	// Older Codex versions may omit forked_from_id at top level but still
	// have parent_thread_id nested in source.subagent.thread_spawn.
	content := `{"timestamp":"2026-01-15T10:00:00.000Z","type":"session_meta","payload":{"id":"019a0001-0000-0000-0000-000000000001","timestamp":"2026-01-15T10:00:00.000Z","cwd":"/tmp","originator":"codex_cli_rs","cli_version":"0.95.0","source":{"subagent":{"thread_spawn":{"parent_thread_id":"019a0000-0000-0000-0000-000000000000","depth":1,"agent_path":null,"agent_nickname":"Darwin","agent_role":"explorer"}}}}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "019a0000-0000-0000-0000-000000000000" {
		t.Fatalf("expected fallback to thread_spawn parent_thread_id, got %q", meta.ParentSessionID)
	}
	if meta.AgentNickname != "Darwin" {
		t.Fatalf("expected fallback to thread_spawn agent_nickname, got %q", meta.AgentNickname)
	}
	if meta.AgentRole != "explorer" {
		t.Fatalf("expected fallback to thread_spawn agent_role, got %q", meta.AgentRole)
	}
}

func TestReadSessionMetaTopLevelTakesPrecedence(t *testing.T) {
	// When both top-level and nested fields exist, top-level wins.
	content := `{"timestamp":"2026-04-12T10:00:00.000Z","type":"session_meta","payload":{"id":"019d0001-0000-0000-0000-000000000001","forked_from_id":"parent-top","timestamp":"2026-04-12T10:00:00.000Z","cwd":"/tmp","originator":"codex-tui","cli_version":"0.120.0","agent_nickname":"TopNick","agent_role":"top_role","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent-nested","depth":1,"agent_nickname":"NestedNick","agent_role":"nested_role"}}}}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "parent-top" {
		t.Fatalf("top-level forked_from_id should take precedence, got %q", meta.ParentSessionID)
	}
	if meta.AgentNickname != "TopNick" {
		t.Fatalf("top-level agent_nickname should take precedence, got %q", meta.AgentNickname)
	}
	if meta.AgentRole != "top_role" {
		t.Fatalf("top-level agent_role should take precedence, got %q", meta.AgentRole)
	}
}

func TestReadSessionMetaSourceStringNotObject(t *testing.T) {
	// Root sessions have source as a plain string like "cli".
	// The fallback should not crash on non-object source.
	content := `{"timestamp":"2026-02-28T08:26:40.646Z","type":"session_meta","payload":{"id":"019ca35b-6460-7892-b6be-3c399d927ada","timestamp":"2026-02-28T08:26:40.632Z","cwd":"/tmp","originator":"codex_cli_rs","cli_version":"0.98.0","source":"cli"}}
`
	path := writeTempFile(t, content)
	meta := ReadSessionMeta(path)

	if meta.ParentSessionID != "" {
		t.Fatalf("string source should not produce parentSessionID, got %q", meta.ParentSessionID)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
