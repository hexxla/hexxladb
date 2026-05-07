#!/usr/bin/env bash
# Post-read code hook
# Tracks file access patterns for analytics

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

# Track file access (could be sent to analytics service)
echo "📖 File read: $file_path" >&2

# Track which layers are being accessed most frequently
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

exit 0
