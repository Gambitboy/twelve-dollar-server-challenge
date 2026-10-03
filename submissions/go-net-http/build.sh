#!/usr/bin/env bash
# Builds bin/feedapi. mattn/go-sqlite3 is cgo, so this is a native build (it compiles the
# bundled SQLite amalgamation, about a minute the first time).
set -euo pipefail
cd "$(dirname "$0")"
export PATH="/usr/local/go/bin:$PATH" CGO_ENABLED=1
go build -trimpath -o bin/feedapi .
