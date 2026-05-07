#!/usr/bin/env bash
# Claude post-run-command hook
# Logs command execution results

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
command_line=$(echo "$input" | jq -r '.command // empty' 2>/dev/null || echo "")

if [[ -n "$command_line" ]]; then
  echo "🚀 Command executed: $command_line" >&2
fi

exit 0
