package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chojs23/lazyagent/internal/app"
	"github.com/chojs23/lazyagent/internal/config"
	"github.com/chojs23/lazyagent/internal/store"
	"github.com/chojs23/lazyagent/internal/tui"
)

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

func printUsage() {
	fmt.Println("lazyagent <command>")
	fmt.Println("Commands:")
	fmt.Println("  ingest --runtime claude [--project-slug slug]  Read hook payload from stdin")
	fmt.Println("  health                                         Check SQLite access")
	fmt.Println("  tui                                            Open the terminal UI")
}
