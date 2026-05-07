#!/usr/bin/env bash
# Post-MCP tool use hook
# Logs Mosaic MCP tool usage for analytics and monitoring

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

# Read JSON input from stdin
input=$(cat)

# Parse MCP tool info
mcp_server_name=$(echo "$input" | jq -r '.tool_info.mcp_server_name // empty' 2>/dev/null || echo "")
mcp_tool_name=$(echo "$input" | jq -r '.tool_info.mcp_tool_name // empty' 2>/dev/null || echo "")

if [[ -n "$mcp_server_name" && -n "$mcp_tool_name" ]]; then
  # Log the tool usage (could be sent to analytics service)
  echo "📊 MCP Tool Used: $mcp_server_name/$mcp_tool_name" >&2

  # If this is a Mosaic tool, log it for tracking
  if [[ "$mcp_server_name" == "mosaic" ]]; then
    echo "🧩 Mosaic Tool: $mcp_tool_name" >&2
  fi
fi

exit 0
