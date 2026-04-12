#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
BIN="$SCRIPT_DIR/../bin/lazyagent"

if [ -x "$BIN" ]; then
	exec "$BIN" ingest --runtime claude
fi

printf '%s\n' "lazyagent binary not found at $BIN" >&2
printf '%s\n' "Build first: go build -o ./bin/lazyagent ./cmd/lazyagent" >&2
exit 1
