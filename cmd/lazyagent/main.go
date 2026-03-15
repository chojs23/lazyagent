package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chojs23/lazyagent/internal/app"
	"github.com/chojs23/lazyagent/internal/config"
	"github.com/chojs23/lazyagent/internal/store"
	"github.com/chojs23/lazyagent/internal/tui"
)

//go:embed opencode_plugin.ts
var openCodePluginTS string

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	cmd := "tui"
	if len(os.Args) >= 2 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "ingest":
		return runIngest(st, os.Args[2:])
	case "init":
		if len(os.Args) < 3 {
			fmt.Println("Usage: lazyagent init <claude|opencode>")
			return nil
		}
		return runInit(os.Args[2])
	case "health":
		return runHealth(st, cfg.DBPath)
	case "tui":
		return tui.Run(st, cfg.RefreshInterval)
	default:
		printUsage()
		return nil
	}
}

func runIngest(st *store.Store, args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	runtime := fs.String("runtime", "claude", "event runtime")
	slug := fs.String("project-slug", "", "project slug override")
	if err := fs.Parse(args); err != nil {
		return err
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	switch *runtime {
	case "claude":
		result, err := app.IngestClaudeEvent(context.Background(), st, payload, *slug)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"status": "ok", "meta": result})
	case "opencode":
		result, err := app.IngestOpenCodeEvent(context.Background(), st, payload, *slug)
		if err != nil {
			return err
		}
		return writeJSON(map[string]any{"status": "ok", "meta": result})
	default:
		return fmt.Errorf("unsupported runtime %q", *runtime)
	}
}

func runHealth(st *store.Store, dbPath string) error {
	if err := st.HealthCheck(context.Background()); err != nil {
		return err
	}
	return writeJSON(map[string]any{"ok": true, "db_path": dbPath})
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
	default:
		return fmt.Errorf("unsupported runtime %q (use claude or opencode)", runtime)
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
		json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	events := []string{"PreToolUse", "PostToolUse", "SessionStart", "SessionEnd", "Stop", "SubagentStop", "Notification", "UserPromptSubmit"}
	added := 0
	skipped := 0

	for _, event := range events {
		if hasLazyagentHook(hooks[event]) {
			skipped++
			continue
		}
		entry := map[string]any{
			"matcher": "",
			"hooks":   []any{map[string]any{"type": "command", "command": hookCmd}},
		}
		existing, _ := hooks[event].([]any)
		hooks[event] = append(existing, entry)
		added++
	}

	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		return err
	}

	if added > 0 {
		fmt.Printf("Claude hooks configured in %s (%d added, %d skipped)\n", settingsPath, added, skipped)
	} else {
		fmt.Printf("All hooks already configured in %s (%d skipped)\n", settingsPath, skipped)
	}
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
	if _, err := os.Stat(pluginPath); err == nil {
		fmt.Printf("OpenCode plugin already exists at %s (skipped)\n", pluginPath)
		return nil
	}
	if err := os.WriteFile(pluginPath, []byte(openCodePluginTS), 0o644); err != nil {
		return err
	}

	fmt.Printf("OpenCode plugin installed at %s\n", pluginPath)
	return nil
}

func hasLazyagentHook(eventEntry any) bool {
	entries, ok := eventEntry.([]any)
	if !ok {
		return false
	}
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		hooks, _ := entry["hooks"].([]any)
		for _, h := range hooks {
			hook, _ := h.(map[string]any)
			cmd, _ := hook["command"].(string)
			if strings.Contains(cmd, "lazyagent") {
				return true
			}
		}
	}
	return false
}

func printUsage() {
	fmt.Println("lazyagent <command>")
	fmt.Println("Commands:")
	fmt.Println("  init <claude|opencode>                         Setup hooks/plugin for runtime")
	fmt.Println("  ingest --runtime claude [--project-slug slug]  Read hook payload from stdin")
	fmt.Println("  health                                         Check SQLite access")
	fmt.Println("  tui                                            Open the terminal UI")
}
