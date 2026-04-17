package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTranscriptTokens(t *testing.T) {
	// Create a minimal JSONL transcript file.
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		`{"type":"permission-mode","permissionMode":"default","sessionId":"test-1"}`,
		`{"type":"user","message":{"role":"user","content":"hello"},"sessionId":"test-1"}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":300}},"sessionId":"test-1"}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-sonnet-4-6-20250514","content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"git status && ls -la"}},{"type":"tool_use","id":"tu2","name":"Read","input":{"file_path":"/tmp/x"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_read_input_tokens":1000,"cache_creation_input_tokens":0}},"sessionId":"test-1"}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"tool_use","id":"tu3","name":"Edit","input":{"file_path":"/tmp/y","old_string":"a","new_string":"b"}}],"usage":{"input_tokens":300,"output_tokens":100,"cache_read_input_tokens":500,"cache_creation_input_tokens":0}},"sessionId":"test-1"}`,
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := ReadTranscriptTokens(path)
	if err != nil {
		t.Fatalf("ReadTranscriptTokens: %v", err)
	}

	if summary.APICalls != 3 {
		t.Errorf("APICalls = %d, want 3", summary.APICalls)
	}
	if summary.Tokens.InputTokens != 900 {
		t.Errorf("InputTokens = %d, want 900", summary.Tokens.InputTokens)
	}
	if summary.Tokens.OutputTokens != 350 {
		t.Errorf("OutputTokens = %d, want 350", summary.Tokens.OutputTokens)
	}
	if summary.Tokens.CacheReadTokens != 1700 {
		t.Errorf("CacheReadTokens = %d, want 1700", summary.Tokens.CacheReadTokens)
	}
	if summary.Tokens.CacheCreationTokens != 300 {
		t.Errorf("CacheCreationTokens = %d, want 300", summary.Tokens.CacheCreationTokens)
	}

	// Model breakdown.
	if len(summary.ModelBreakdown) != 2 {
		t.Errorf("ModelBreakdown has %d entries, want 2", len(summary.ModelBreakdown))
	}
	opus := summary.ModelBreakdown["claude-opus-4-6"]
	if opus == nil {
		t.Fatal("missing claude-opus-4-6 in ModelBreakdown")
	}
	if opus.Calls != 2 {
		t.Errorf("opus calls = %d, want 2", opus.Calls)
	}
	sonnet := summary.ModelBreakdown["claude-sonnet-4-6"]
	if sonnet == nil {
		t.Fatal("missing claude-sonnet-4-6 in ModelBreakdown")
	}
	if sonnet.Calls != 1 {
		t.Errorf("sonnet calls = %d, want 1", sonnet.Calls)
	}

	// Tool breakdown.
	if summary.ToolBreakdown["Bash"] == nil || summary.ToolBreakdown["Bash"].Calls != 1 {
		t.Errorf("Bash tool calls = %v, want 1", summary.ToolBreakdown["Bash"])
	}
	if summary.ToolBreakdown["Read"] == nil || summary.ToolBreakdown["Read"].Calls != 1 {
		t.Errorf("Read tool calls = %v, want 1", summary.ToolBreakdown["Read"])
	}
	if summary.ToolBreakdown["Edit"] == nil || summary.ToolBreakdown["Edit"].Calls != 1 {
		t.Errorf("Edit tool calls = %v, want 1", summary.ToolBreakdown["Edit"])
	}

	// Bash breakdown: "git status && ls -la" should extract "git" and "ls".
	if summary.BashBreakdown["git"] == nil || summary.BashBreakdown["git"].Calls != 1 {
		t.Errorf("git bash calls = %v, want 1", summary.BashBreakdown["git"])
	}
	if summary.BashBreakdown["ls"] == nil || summary.BashBreakdown["ls"].Calls != 1 {
		t.Errorf("ls bash calls = %v, want 1", summary.BashBreakdown["ls"])
	}
}

func TestExtractCommandNames(t *testing.T) {
	tests := []struct {
		cmd  string
		want []string
	}{
		{"git status", []string{"git"}},
		{"git add . && git commit -m 'msg'", []string{"git", "git"}},
		{"FOO=bar npm run build", []string{"npm"}},
		{"cd /tmp", nil},
		{"echo 'hello' | grep hello", []string{"echo", "grep"}},
		{"ls -la; cat foo.txt", []string{"ls", "cat"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := ExtractCommandNames(tt.cmd)
		if len(got) != len(tt.want) {
			t.Errorf("ExtractCommandNames(%q) = %v, want %v", tt.cmd, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ExtractCommandNames(%q)[%d] = %q, want %q", tt.cmd, i, got[i], tt.want[i])
			}
		}
	}
}

func TestShortModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"claude-sonnet-4-6-20250514", "claude-sonnet-4-6"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := shortModelName(tt.input)
		if got != tt.want {
			t.Errorf("shortModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
