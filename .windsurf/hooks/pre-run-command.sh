#!/usr/bin/env bash
# Pre-run command hook
# Blocks dangerous commands before execution

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

# Read JSON input from stdin
input=$(cat)

# Parse command line from input
command_line=$(echo "$input" | jq -r '.tool_info.command_line // empty' 2>/dev/null || echo "")

if [[ -z "$command_line" ]]; then
  exit 0
fi

# List of dangerous commands to block
dangerous_patterns=(
  "rm -rf /"
  "rm -rf /home"
  "rm -rf /usr"
  "rm -rf /etc"
  "rm -rf /var"
  "dd if="
  ":(){:|:&};:"
  "> /dev/sd"
  "mkfs."
  "git push --force"
  "git push -f"
  "chmod -R 777 /"
  "chown -R"
)

# Check if command matches any dangerous pattern
for pattern in "${dangerous_patterns[@]}"; do
  if [[ "$command_line" == *"$pattern"* ]]; then
    echo "⛔  Blocked dangerous command: $command_line"
    echo "Pattern matched: $pattern"
    # Exit with code 2 to block the action
    exit 2
  fi
done

# Warn about potentially destructive commands
warning_patterns=(
  "rm -rf"
  "git push"
  "docker rm"
  "kubectl delete"
)

for pattern in "${warning_patterns[@]}"; do
  if [[ "$command_line" == *"$pattern"* ]]; then
    echo "⚠️  Warning: Potentially destructive command: $command_line"
    echo "Pattern: $pattern"
    # Don't block, just warn
  fi
done

exit 0
