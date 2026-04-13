# Contributing

Thanks for your interest in improving `lazyagent`.

Bug reports, feature requests, and pull requests are all welcome.

## Bug reports

When you report a bug, please include as much useful context as you can:

- what you expected to happen
- what actually happened
- your OS and runtime involved, such as Claude, Codex, or OpenCode
- steps to reproduce the issue
- screenshots or terminal output if they help
- relevant database or log details when available

If the problem looks related to internal errors, check:

```text
~/.lazyagent/lazyagent.log
```

and include the relevant part if you can share it safely.

## Feature requests

Feature requests are welcome.

Please describe:

- the problem you are trying to solve
- any examples, screenshots, or references that help explain the idea

## Pull requests

Pull requests are welcome.

Before opening a PR:

1. Keep the change focused and small.
2. Update documentation when behavior changes.
3. Add or update tests when needed.

You need to check that your code is properly formatted, tested, and builds successfully.

```bash
gofmt -l .
go test ./...
go build -o ./bin/lazyagent ./cmd/lazyagent
```

If you change the OpenCode plugin source in:

```text
plugins/opencode/src/index.ts
```

also keep the embedded shipping copy in sync:

```text
cmd/lazyagent/opencode_plugin.ts
```
