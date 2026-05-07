#!/usr/bin/env bash
# Claude pre-mcp-tool-use hook
# Provides Mosaic MCP tool guidance

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
mcp_server_name=$(echo "$input" | jq -r '.server_name // empty' 2>/dev/null || echo "")

if [[ "$mcp_server_name" == "mosaic" ]]; then
  echo "🧩 Mosaic MCP tool guidance: Use intelligent read/write patterns" >&2
fi

exit 0
