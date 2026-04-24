#!/usr/bin/env bash
# Codex PostToolUse hook — run gofmt on edited Go files (file is already on disk).
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
	exit 0
fi

input=$(cat)
# Codex PostToolUse provides file_path via JSON input
file_path=$(echo "$input" | jq -r '.file_path // empty')

[[ -n "$file_path" ]] || exit 0
[[ "$file_path" == *.go ]] || exit 0
[[ -f "$file_path" ]] || exit 0

if ! command -v gofmt >/dev/null 2>&1; then
	exit 0
fi

# gofmt -w in place; ignore errors (e.g. invalid syntax mid-edit)
# Claude input: {"file_path": "..."} via stdin
gofmt -w "$file_path" 2>/dev/null || true
exit 0
