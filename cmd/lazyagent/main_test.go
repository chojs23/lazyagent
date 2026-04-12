package main

import (
	"bytes"
	"io"
	"os"
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
