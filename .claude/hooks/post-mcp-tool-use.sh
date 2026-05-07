#!/usr/bin/env bash
# Claude post-mcp-tool-use hook
# Logs Mosaic MCP tool usage

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
mcp_server_name=$(echo "$input" | jq -r '.server_name // empty' 2>/dev/null || echo "")
mcp_tool_name=$(echo "$input" | jq -r '.tool_name // empty' 2>/dev/null || echo "")

if [[ -n "$mcp_server_name" && -n "$mcp_tool_name" ]]; then
  echo "📊 MCP Tool Used: $mcp_server_name/$mcp_tool_name" >&2
  if [[ "$mcp_server_name" == "mosaic" ]]; then
    echo "🧩 Mosaic Tool: $mcp_tool_name" >&2
  fi
fi

exit 0
