#!/usr/bin/env bash
# Claude pre-run-command hook
# Blocks dangerous commands before execution

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

input=$(cat)
command_line=$(echo "$input" | jq -r '.command // empty' 2>/dev/null || echo "")

if [[ -z "$command_line" ]]; then
  exit 0
fi

dangerous_patterns=("rm -rf /" "rm -rf /home" "rm -rf /usr" "rm -rf /etc" "rm -rf /var" "dd if=" ":(){:|:&};:" "> /dev/sd" "mkfs." "git push --force" "git push -f" "chmod -R 777 /" "chown -R")

for pattern in "${dangerous_patterns[@]}"; do
  if [[ "$command_line" == *"$pattern"* ]]; then
    echo "⛔  Blocked dangerous command: $command_line" >&2
    exit 2
  fi
done

warning_patterns=("rm -rf" "git push" "docker rm" "kubectl delete")

for pattern in "${warning_patterns[@]}"; do
  if [[ "$command_line" == *"$pattern"* ]]; then
    echo "⚠️  Warning: Potentially destructive command: $command_line" >&2
  fi
done

exit 0
