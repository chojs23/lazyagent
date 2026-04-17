package codex

import (
	"strings"
	"testing"
)

func TestReadTranscriptTokensUsesCumulativeDeltasPerModel(t *testing.T) {
	content := strings.Join([]string{
		`{"timestamp":"2026-04-12T10:19:58.000Z","type":"turn_context","payload":{"model":"gpt-5.4"}}`,
		`{"timestamp":"2026-04-12T10:20:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":40,"cached_input_tokens":10}}}}`,
		`{"timestamp":"2026-04-12T10:20:01.000Z","type":"response_item","payload":{"type":"function_call","name":"shell_command","arguments":"{\"command\":\"git status && ls\"}"}}`,
		`{"timestamp":"2026-04-12T10:20:02.000Z","type":"response_item","payload":{"type":"function_call","name":"read_file","arguments":"{\"path\":\"/tmp/x\"}"}}`,
		`{"timestamp":"2026-04-12T10:20:03.000Z","type":"turn_context","payload":{"model":"gpt-4.1-mini"}}`,
		`{"timestamp":"2026-04-12T10:20:04.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":70,"cached_input_tokens":30}}}}`,
		`{"timestamp":"2026-04-12T10:20:05.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"output_tokens":70,"cached_input_tokens":30}}}}`,
	}, "\n") + "\n"

	path := writeTempFile(t, content)
	summary, err := ReadTranscriptTokens(path)
	if err != nil {
		t.Fatalf("ReadTranscriptTokens: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}

	if summary.APICalls != 2 {
		t.Fatalf("APICalls = %d, want 2", summary.APICalls)
	}
	if summary.Tokens.InputTokens != 160 {
		t.Fatalf("InputTokens = %d, want 160", summary.Tokens.InputTokens)
	}
	if summary.Tokens.OutputTokens != 70 {
		t.Fatalf("OutputTokens = %d, want 70", summary.Tokens.OutputTokens)
	}
	if summary.Tokens.CacheReadTokens != 30 {
		t.Fatalf("CacheReadTokens = %d, want 30", summary.Tokens.CacheReadTokens)
	}

	first := summary.ModelBreakdown["gpt-5.4"]
	if first == nil {
		t.Fatal("missing gpt-5.4 model breakdown")
	}
	if first.Calls != 1 {
		t.Fatalf("gpt-5.4 calls = %d, want 1", first.Calls)
	}
	if first.Tokens.InputTokens != 100 || first.Tokens.OutputTokens != 40 || first.Tokens.CacheReadTokens != 10 {
		t.Fatalf("gpt-5.4 tokens = %+v, want input=100 output=40 cache=10", first.Tokens)
	}

	second := summary.ModelBreakdown["gpt-4.1-mini"]
	if second == nil {
		t.Fatal("missing gpt-4.1-mini model breakdown")
	}
	if second.Calls != 1 {
		t.Fatalf("gpt-4.1-mini calls = %d, want 1", second.Calls)
	}
	if second.Tokens.InputTokens != 60 || second.Tokens.OutputTokens != 30 || second.Tokens.CacheReadTokens != 20 {
		t.Fatalf("gpt-4.1-mini tokens = %+v, want input=60 output=30 cache=20", second.Tokens)
	}

	if summary.ToolBreakdown["shell_command"] == nil || summary.ToolBreakdown["shell_command"].Calls != 1 {
		t.Fatalf("shell_command tool calls = %v, want 1", summary.ToolBreakdown["shell_command"])
	}
	if summary.ToolBreakdown["read_file"] == nil || summary.ToolBreakdown["read_file"].Calls != 1 {
		t.Fatalf("read_file tool calls = %v, want 1", summary.ToolBreakdown["read_file"])
	}
	if summary.BashBreakdown["git"] == nil || summary.BashBreakdown["git"].Calls != 1 {
		t.Fatalf("git bash calls = %v, want 1", summary.BashBreakdown["git"])
	}
	if summary.BashBreakdown["ls"] == nil || summary.BashBreakdown["ls"].Calls != 1 {
		t.Fatalf("ls bash calls = %v, want 1", summary.BashBreakdown["ls"])
	}
}

func TestReadTranscriptTokensFallsBackToLastTokenUsage(t *testing.T) {
	content := strings.Join([]string{
		`{"timestamp":"2026-04-12T10:19:58.000Z","type":"turn_context","payload":{"model":"gpt-5.4-mini"}}`,
		`{"timestamp":"2026-04-12T10:20:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":25,"output_tokens":10,"cached_input_tokens":5}}}}`,
	}, "\n") + "\n"

	path := writeTempFile(t, content)
	summary, err := ReadTranscriptTokens(path)
	if err != nil {
		t.Fatalf("ReadTranscriptTokens: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}

	if summary.APICalls != 1 {
		t.Fatalf("APICalls = %d, want 1", summary.APICalls)
	}
	if summary.Tokens.InputTokens != 25 || summary.Tokens.OutputTokens != 10 || summary.Tokens.CacheReadTokens != 5 {
		t.Fatalf("tokens = %+v, want input=25 output=10 cache=5", summary.Tokens)
	}
	stats := summary.ModelBreakdown["gpt-5.4-mini"]
	if stats == nil || stats.Calls != 1 {
		t.Fatalf("model stats = %v, want one call for gpt-5.4-mini", stats)
	}
}

func TestReadTranscriptTokensHandlesMixedTotalAndLastUsage(t *testing.T) {
	content := strings.Join([]string{
		`{"timestamp":"2026-04-12T10:19:58.000Z","type":"turn_context","payload":{"model":"gpt-5.4"}}`,
		`{"timestamp":"2026-04-12T10:20:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":40,"cached_input_tokens":10}}}}`,
		`{"timestamp":"2026-04-12T10:20:01.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":20,"output_tokens":5,"cached_input_tokens":0}}}}`,
		`{"timestamp":"2026-04-12T10:20:02.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"output_tokens":45,"cached_input_tokens":10}}}}`,
	}, "\n") + "\n"

	path := writeTempFile(t, content)
	summary, err := ReadTranscriptTokens(path)
	if err != nil {
		t.Fatalf("ReadTranscriptTokens: %v", err)
	}
	if summary == nil {
		t.Fatal("summary should not be nil")
	}

	if summary.APICalls != 2 {
		t.Fatalf("APICalls = %d, want 2", summary.APICalls)
	}
	if summary.Tokens.InputTokens != 120 {
		t.Fatalf("InputTokens = %d, want 120", summary.Tokens.InputTokens)
	}
	if summary.Tokens.OutputTokens != 45 {
		t.Fatalf("OutputTokens = %d, want 45", summary.Tokens.OutputTokens)
	}
	if summary.Tokens.CacheReadTokens != 10 {
		t.Fatalf("CacheReadTokens = %d, want 10", summary.Tokens.CacheReadTokens)
	}

	stats := summary.ModelBreakdown["gpt-5.4"]
	if stats == nil {
		t.Fatal("missing gpt-5.4 model breakdown")
	}
	if stats.Calls != 2 {
		t.Fatalf("gpt-5.4 calls = %d, want 2", stats.Calls)
	}
}
