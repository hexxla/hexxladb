#!/usr/bin/env bash
# Claude pre-write hook
# Validates code before it's written to disk

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
file_path=$(echo "$input" | jq -r '.file_path // empty' 2>/dev/null || echo "")

if [[ -z "$file_path" ]]; then
  exit 0
fi

if [[ "$file_path" != *.go ]]; then
  exit 0
fi

protected_files=("go.mod" "go.sum" ".golangci.yml" "Makefile")

for protected in "${protected_files[@]}"; do
  if [[ "$(basename "$file_path")" == "$protected" ]]; then
    echo "⚠️  Protected file: $file_path" >&2
    echo "This file should not be modified directly." >&2
    exit 2
  fi
done

if [[ "$file_path" == internal/core/domain/* ]]; then
  echo "ℹ️  Writing to core/domain/ - ensure no internal dependencies" >&2
fi

exit 0
