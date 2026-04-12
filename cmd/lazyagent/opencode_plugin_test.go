package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedOpenCodePluginMatchesSource(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path")
	}

	sourcePath := filepath.Join(filepath.Dir(file), "..", "..", "plugins", "opencode", "src", "index.ts")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source plugin: %v", err)
	}

	if !bytes.Equal([]byte(openCodePluginTS), source) {
		t.Fatalf("embedded OpenCode plugin drifted from %s; keep cmd/lazyagent/opencode_plugin.ts in sync with the maintained source plugin", sourcePath)
	}
}
