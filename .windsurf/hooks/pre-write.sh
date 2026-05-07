#!/usr/bin/env bash
# Pre-write validation hook
# Validates code before it's written to disk

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

# Read JSON input from stdin
input=$(cat)

# Parse file path from input
file_path=$(echo "$input" | jq -r '.tool_info.file_path // empty' 2>/dev/null || echo "")

if [[ -z "$file_path" ]]; then
  exit 0
fi

# Check if file is a Go file
if [[ "$file_path" != *.go ]]; then
  exit 0
fi

# Block writes to protected files
protected_files=(
  "go.mod"
  "go.sum"
  ".golangci.yml"
  "Makefile"
)

for protected in "${protected_files[@]}"; do
  if [[ "$(basename "$file_path")" == "$protected" ]]; then
    echo "⚠️  Protected file: $file_path"
    echo "This file should not be modified directly."
    echo "Use 'go mod tidy' for go.mod/go.sum instead."
    # Exit with code 2 to block the action
    exit 2
  fi
done

# Check if file is in internal/domain or internal/app and would violate purity
if [[ "$file_path" == internal/domain/* ]] || [[ "$file_path" == internal/app/* ]]; then
  echo "ℹ️  Writing to ${file_path%/*} - ensure no imports from internal/adapters/, internal/engine, or internal/index"
fi

exit 0
