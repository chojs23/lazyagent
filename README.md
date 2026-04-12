# lazyagent

<img width="1613" height="848" alt="lazyagent" src="https://github.com/user-attachments/assets/8ba701ac-fec9-4bc3-8a66-a213b33eed24" />

`lazyagent` is a terminal TUI app for watching what Claude, Codex, and OpenCode sessions are doing.

It collects runtime events, stores them in SQLite, and shows them in a structured interface so you can inspect projects, sessions, agents, subagents, tools, prompts, outputs, and status changes without digging through raw hook payloads.

The TUI is built for day to day observability. You can see which session belongs to which project, which agent or subagent is active, what tool ran, and what happened next.

## Installation

### Homebrew

Install from the Homebrew tap with:

```bash
brew tap chojs23/homebrew-tap
brew install --cask lazyagent
```

### Go install

```bash
go install github.com/chojs23/lazyagent/cmd/lazyagent@latest
```

### Nix flake

Run directly:

```bash
nix run github:chojs23/lazyagent
```

Install into your profile:

```bash
nix profile install github:chojs23/lazyagent
```

### Build from source

```bash
go build -o ./bin/lazyagent ./cmd/lazyagent
```

## Claude, Codex, and OpenCode setup

`lazyagent` is usually used through runtime hooks and plugins.

### Claude

```bash
lazyagent init claude
```

This updates:

```text
~/.claude/settings.json
```

It registers `lazyagent ingest --runtime claude` for these Claude hook events:

- `PreToolUse`
- `PostToolUse`
- `SessionStart`
- `SessionEnd`
- `Stop`
- `SubagentStop`
- `Notification`
- `UserPromptSubmit`

Existing non `lazyagent` hooks are preserved.

### Codex

```bash
lazyagent init codex
```

This updates:

```text
~/.codex/config.toml
~/.codex/hooks.json
```

It enables `features.codex_hooks = true` and registers `lazyagent ingest --runtime codex --quiet` for supported Codex hook events.

### OpenCode

```bash
lazyagent init opencode
```

This writes the OpenCode plugin to:

```text
~/.config/opencode/plugins/lazyagent.ts
```

Set environment variables for the plugin if you want:

- `LAZYAGENT_BIN` to point at a specific `lazyagent` binary
- `LAZYAGENT_PROJECT_SLUG` to override project slug detection

## Build and test

Build the Go binary:

```bash
go build -o ./bin/lazyagent ./cmd/lazyagent
```

Run the Go test suite:

```bash
go test ./...
```

If you need to work on the maintained OpenCode plugin source directly:

```bash
cd plugins/opencode
npm install
npm run build
```

The shipping plugin is embedded into the Go binary, so keep the maintained source and embedded copy in sync when you change it.

## Keybindings

Lazyagent has five panes:

1. Projects and root sessions
2. Session summary
3. Agents and subagents
4. Events
5. Event detail

Main keys:

- `tab`, `shift+tab` move between panes
- `1`, `2`, `3`, `4`, `5` jump to a specific pane
- `j`, `k` move through lists
- `g`, `G` jump to top or bottom
- `ctrl+u`, `ctrl+d` move by half a page
- `enter`, `space` select the current item
- `/` opens search
- `t` cycles event type filters
- `a` clears the current agent filter when the agent pane is focused
- `d` deletes the selected project or session from the projects pane
- `D` clears events for the selected session tree
- `F` toggles auto follow in the events pane
- `r` refreshes data
- `?` toggles help
- `q` quits

When a non panic internal app error happens, the TUI shows a small toast in the
bottom right for about 5 seconds while also writing the error to
`lazyagent.log`.

Detail pane keys:

- `J` toggles raw JSON
- `e` expands long content blocks

## Usage

Run the lazyagent:

```bash
lazyagent
```

### Commands

#### `lazyagent init <claude|opencode|codex>`

Install or refresh runtime hooks and plugins.

Examples:

```bash
lazyagent init claude
lazyagent init codex
lazyagent init opencode
```

#### `lazyagent ingest`

Read runtime event payloads from stdin and store them in the database.

This command is normally called by hooks and plugins, not manually.

Examples:

```bash
lazyagent ingest --runtime claude
lazyagent ingest --runtime opencode --project-slug my-project
lazyagent ingest --runtime codex --quiet
```

#### `lazyagent health`

Check whether the SQLite database can be opened.

```bash
lazyagent health
```

#### `lazyagent version`

Show build and release metadata.

```bash
lazyagent version
lazyagent version --json
lazyagent --version
```

## DB and log information

By default, `lazyagent` stores data under:

```text
~/.lazyagent
```

Default database path:

```text
~/.lazyagent/observe.db
```

Default log path:

```text
~/.lazyagent/lazyagent.log
```

Supported environment variables:

- `LAZYAGENT_DATA_DIR`
  - overrides the base data directory
- `LAZYAGENT_DB_PATH`
  - overrides the database path
  - when set, its parent directory also becomes the active data directory for logs

The TUI refresh interval defaults to 1 second.

## Contribution

Bug reports, feature requests, and pull requests are all welcome.

Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for contribution guidance.
