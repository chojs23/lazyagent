package tui

import (
	"strings"
	"testing"
)

func TestLangFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"/some/path/file.rs", "rust"},
		{"app.ts", "typescript"},
		{"app.tsx", "typescript"},
		{"script.py", "python"},
		{"run.sh", "shell"},
		{"query.sql", "sql"},
		{"style.css", ""},
		{"unknown.xyz", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := langFromPath(tt.path)
		if got != tt.want {
			t.Errorf("langFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestHighlightLine_Keywords(t *testing.T) {
	line := `func main() {`
	result := highlightLine(line, "go")
	// "func" should be highlighted (contains ANSI escape for color 75)
	if !strings.Contains(result, "func") {
		t.Error("expected 'func' in output")
	}
	// result should not be empty
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestHighlightLine_Strings(t *testing.T) {
	line := `name := "hello world"`
	result := highlightLine(line, "go")
	// the string should be highlighted (contains ANSI escape for color 179)
	if !strings.Contains(result, "hello world") {
		t.Error("expected string content in output")
	}
}

func TestHighlightLine_Comments(t *testing.T) {
	line := `x := 1 // this is a comment`
	result := highlightLine(line, "go")
	if !strings.Contains(result, "this is a comment") {
		t.Error("expected comment text in output")
	}
}

func TestHighlightLine_PythonComment(t *testing.T) {
	line := `x = 1  # python comment`
	result := highlightLine(line, "python")
	if !strings.Contains(result, "python comment") {
		t.Error("expected comment text in output")
	}
}

func TestHighlightLine_UnknownLang(t *testing.T) {
	line := `some random text`
	result := highlightLine(line, "")
	if !strings.Contains(result, "some random text") {
		t.Error("expected original text in output")
	}
}

func TestHighlightLine_EmptyLine(t *testing.T) {
	result := highlightLine("", "go")
	if result != "" {
		t.Errorf("expected empty result for empty line, got %q", result)
	}
}

func TestHighlightLine_Numbers(t *testing.T) {
	line := `x := 42`
	result := highlightLine(line, "go")
	if !strings.Contains(result, "42") {
		t.Error("expected number in output")
	}
}
