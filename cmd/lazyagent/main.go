package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/chojs23/lazyagent/internal/app"
	"github.com/chojs23/lazyagent/internal/applog"
	"github.com/chojs23/lazyagent/internal/config"
	"github.com/chojs23/lazyagent/internal/store"
	"github.com/chojs23/lazyagent/internal/tui"
	"github.com/chojs23/lazyagent/internal/version"
)

//go:embed opencode_plugin.ts
var openCodePluginTS string

func main() {
	initLogger()
	defer func() {
		if recovered := recover(); recovered != nil {
			report := applog.Panic("lazyagent panic", recovered)
			if report == "" {
				report = fmt.Sprintf("panic: %v\n%s", recovered, strings.TrimRight(string(debug.Stack()), "\n"))
			}
			fmt.Fprintln(os.Stderr, report)
			os.Exit(2)
		}
	}()

	if err := run(); err != nil {
		applog.Error("lazyagent command failed", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initLogger() {
	logger, err := applog.NewDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazyagent logger init failed:", err)
		return
	}
	applog.SetDefault(logger)
}

func run() error {
	cmd := "tui"
	if len(os.Args) >= 2 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "version":
		return runVersion(os.Args[2:])
	case "--version", "-version", "-v":
		return runVersion(nil)
	case "init":
		if len(os.Args) < 3 {
			fmt.Println("Usage: lazyagent init <claude|opencode|codex>")
			return nil
		}
		return runInit(os.Args[2])
	case "ingest":
		// Parse flags before opening the store so that the runtime name
		// is available for error messages even when the DB fails to open.
		fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
		runtime := fs.String("runtime", "claude", "event runtime")
		quiet := fs.Bool("quiet", false, "suppress stdout output (required for Codex hooks)")
		stream := fs.Bool("stream", false, "read newline-delimited JSON from stdin continuously")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}

		_, st, err := openStore()
		if err != nil {
			return fmt.Errorf("ingest %s: %w", *runtime, err)
		}
		defer st.Close()
		return runIngestParsed(st, *runtime, *quiet, *stream)
	case "health":
		cfg, st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()
		return runHealth(st, cfg.DBPath)
	case "tui":
		cfg, st, err := openStore()
		if err != nil {
			return err
		}
		defer st.Close()
		return tui.Run(st, cfg.RefreshInterval)
	default:
		printUsage()
		return nil
	}
}

func openStore() (config.Config, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return config.Config{}, nil, err
	}

	return cfg, st, nil
}

func runIngestParsed(st *store.Store, runtime string, quiet, stream bool) error {
	if stream {
		return runIngestStream(st, runtime)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	result, err := ingestRuntimeEvent(context.Background(), st, runtime, payload)
	if err != nil {
		return fmt.Errorf("ingest %s: %w", runtime, err)
	}

	if quiet {
		return nil
	}
	return writeJSON(map[string]any{"status": "ok", "meta": result})
}

// runIngestStream reads newline-delimited JSON from stdin and processes each
// line as a separate event. The process stays alive until stdin is closed,
// avoiding repeated process-spawn and DB-open overhead when a plugin sends
// many events over the lifetime of a session.
func runIngestStream(st *store.Store, runtime string) error {
	scanner := bufio.NewScanner(os.Stdin)

	// Allow up to 1 MB per line (default 64 KB is too small for some events).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			applog.Error("stream: decode JSON line", err)
			continue
		}

		_, err := ingestRuntimeEvent(context.Background(), st, runtime, payload)
		if err != nil {
			applog.Error("stream: ingest "+runtime, err)
		}
	}

	return scanner.Err()
}

func ingestRuntimeEvent(ctx context.Context, st *store.Store, runtime string, payload map[string]any) (app.IngestResult, error) {
	switch runtime {
	case "claude":
		return app.IngestClaudeEvent(ctx, st, payload)
	case "opencode":
		return app.IngestOpenCodeEvent(ctx, st, payload)
	case "codex":
		return app.IngestCodexEvent(ctx, st, payload)
	default:
		return app.IngestResult{}, fmt.Errorf("unsupported runtime %q", runtime)
	}
}

func runHealth(st *store.Store, dbPath string) error {
	if err := st.HealthCheck(context.Background()); err != nil {
		return err
	}
	return writeJSON(map[string]any{"ok": true, "db_path": dbPath})
}

func runVersion(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "emit version metadata as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info := version.Current()
	if *jsonOutput {
		return writeJSON(info)
	}

	fmt.Printf("lazyagent %s\n", info.Version)
	if info.Commit != "" {
		fmt.Printf("commit: %s\n", info.Commit)
	}
	if info.BuildDate != "" {
		fmt.Printf("built: %s\n", info.BuildDate)
	}

	return nil
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runInit(runtime string) error {
	switch runtime {
	case "claude":
		return initClaude()
	case "opencode":
		return initOpenCode()
	case "codex":
		return initCodex()
	default:
		return fmt.Errorf("unsupported runtime %q (use claude, opencode, or codex)", runtime)
	}
}

func initClaude() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	settingsPath := filepath.Join(dir, "settings.json")
	hookCmd := "lazyagent ingest --runtime claude"

	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	events := []string{"PreToolUse", "PostToolUse", "PostToolUseFailure", "SessionStart", "SessionEnd", "Stop", "SubagentStop", "Notification", "UserPromptSubmit"}
	installManagedCommandHooks(hooks, events, hookCmd, true)

	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return err
	}

	fmt.Printf("Claude hooks configured in %s (%d events)\n", settingsPath, len(events))
	return nil
}

func initOpenCode() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "opencode", "plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	pluginPath := filepath.Join(dir, "lazyagent.ts")
	if err := os.WriteFile(pluginPath, []byte(openCodePluginTS), 0o644); err != nil {
		return err
	}

	fmt.Printf("OpenCode plugin installed at %s\n", pluginPath)
	return nil
}

func removeLazyagentHooks(eventEntry any) []any {
	entries, ok := eventEntry.([]any)
	if !ok {
		return nil
	}
	var kept []any
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		hooks, _ := entry["hooks"].([]any)
		isLazyagent := false
		for _, h := range hooks {
			hook, _ := h.(map[string]any)
			cmd, _ := hook["command"].(string)
			if strings.Contains(cmd, "lazyagent") {
				isLazyagent = true
				break
			}
		}
		if !isLazyagent {
			kept = append(kept, e)
		}
	}
	return kept
}

func installManagedCommandHooks(hooks map[string]any, events []string, hookCmd string, includeMatcher bool) {
	for _, event := range events {
		existing := removeLazyagentHooks(hooks[event])
		hooks[event] = append(existing, managedCommandHookEntry(hookCmd, includeMatcher))
	}
}

func managedCommandHookEntry(command string, includeMatcher bool) map[string]any {
	entry := map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
	}
	if includeMatcher {
		entry["matcher"] = ""
	}
	return entry
}

func initCodex() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if err := ensureCodexHooksEnabled(filepath.Join(dir, "config.toml")); err != nil {
		return err
	}

	if err := installCodexHooks(filepath.Join(dir, "hooks.json")); err != nil {
		return err
	}

	return nil
}

// ensureCodexHooksEnabled reads ~/.codex/config.toml and sets
// features.codex_hooks = true, preserving all other config.
func ensureCodexHooksEnabled(configPath string) error {
	var config map[string]any

	if data, err := os.ReadFile(configPath); err == nil {
		if _, err := toml.Decode(string(data), &config); err != nil {
			return fmt.Errorf("parse %s: %w", configPath, err)
		}
	}
	if config == nil {
		config = map[string]any{}
	}

	features, _ := config["features"].(map[string]any)
	if features == nil {
		features = map[string]any{}
	}
	features["codex_hooks"] = true
	config["features"] = features

	f, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(config); err != nil {
		return fmt.Errorf("encode %s: %w", configPath, err)
	}

	fmt.Printf("Codex hooks enabled in %s\n", configPath)
	return nil
}

// installCodexHooks reads ~/.codex/hooks.json and registers lazyagent hooks
// for all supported Codex events. Existing non-lazyagent hooks are preserved.
// Re-running is idempotent: prior lazyagent entries are removed before adding.
func installCodexHooks(hooksPath string) error {
	hookCmd := "lazyagent ingest --runtime codex --quiet"
	events := []string{"SessionStart", "PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"}

	var root map[string]any
	if data, err := os.ReadFile(hooksPath); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse %s: %w", hooksPath, err)
		}
	}
	if root == nil {
		root = map[string]any{}
	}

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	installManagedCommandHooks(hooks, events, hookCmd, false)

	root["hooks"] = hooks

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(hooksPath, data, 0o644); err != nil {
		return err
	}

	fmt.Printf("Codex hooks configured in %s (%d events)\n", hooksPath, len(events))
	return nil
}

func printUsage() {
	fmt.Println("lazyagent <command>")
	fmt.Println("Commands:")
	fmt.Println("  init <claude|opencode|codex>                    Setup hooks/plugin for runtime")
	fmt.Println("  ingest --runtime claude                        Read hook payload from stdin")
	fmt.Println("         --runtime codex --quiet                 Ingest Codex hook (silent)")
	fmt.Println("  health                                         Check SQLite access")
	fmt.Println("  tui                                            Open the terminal UI")
	fmt.Println("  version [--json]                               Show build and release metadata")
}
