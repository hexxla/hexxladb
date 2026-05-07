#!/usr/bin/env bash
# Post-run command hook
# Logs command execution results for audit trail

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

# Read JSON input from stdin
input=$(cat)

# Parse command info from input
command_line=$(echo "$input" | jq -r '.tool_info.command_line // empty' 2>/dev/null || echo "")
cwd=$(echo "$input" | jq -r '.tool_info.cwd // empty' 2>/dev/null || echo "")

if [[ -n "$command_line" ]]; then
  # Log command execution (could be sent to analytics service)
  echo "🚀 Command executed: $command_line" >&2
  if [[ -n "$cwd" ]]; then
    echo "  Working directory: $cwd" >&2
  fi
fi

exit 0
