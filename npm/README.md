# lazyagent

Terminal TUI for observing Claude, Codex, and OpenCode sessions.

This package is a thin installer for the prebuilt `lazyagent` Go binary.
On install it downloads the matching release archive from GitHub, verifies
the SHA-256 checksum, and places the binary under the package directory.

## Install

```bash
npm install -g @chojs23/lazyagent
```

Or run without installing:

```bash
npx @chojs23/lazyagent
```

## Supported platforms

- macOS (x64, arm64)
- Linux (x64, arm64)
- Windows (x64, arm64)

## Environment

- `LAZYAGENT_SKIP_DOWNLOAD=1` skips the postinstall download. Use this in
  environments where network access is restricted; you must then place a
  `lazyagent` binary in the package's `vendor/` directory yourself.

## Links

- Source and docs: https://github.com/chojs23/lazyagent
- Issues: https://github.com/chojs23/lazyagent/issues

## License

MIT
