#!/usr/bin/env bash
# Claude post-read-code hook
# Tracks file access patterns

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
file_path=$(echo "$input" | jq -r '.file_path // empty' 2>/dev/null || echo "")

if [[ -n "$file_path" ]]; then
  echo "📖 File read: $file_path" >&2
  if [[ "$file_path" == internal/core/domain/* ]]; then
    echo "  Layer: core/domain" >&2
  elif [[ "$file_path" == internal/core/ports/* ]]; then
    echo "  Layer: core/ports" >&2
  elif [[ "$file_path" == internal/core/services/* ]]; then
    echo "  Layer: core/services" >&2
  elif [[ "$file_path" == internal/adapter/primary/* ]]; then
    echo "  Layer: adapter/primary" >&2
  elif [[ "$file_path" == internal/adapter/secondary/* ]]; then
    echo "  Layer: adapter/secondary" >&2
  fi
fi

exit 0
