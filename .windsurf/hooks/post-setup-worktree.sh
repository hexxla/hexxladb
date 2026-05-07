#!/usr/bin/env bash
# Post-setup worktree hook
# Initializes worktree with necessary files and configurations

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
cd "$ROOT"

# Read JSON input from stdin
input=$(cat)

# Parse worktree info from input
worktree_path=$(echo "$input" | jq -r '.tool_info.worktree_path // empty' 2>/dev/null || echo "")
root_workspace_path=$(echo "$input" | jq -r '.tool_info.root_workspace_path // empty' 2>/dev/null || echo "")

if [[ -z "$worktree_path" ]]; then
  exit 0
fi

echo "🌳 Worktree created: $worktree_path" >&2

# Copy .env.example to .env if it exists
if [[ -f "$root_workspace_path/.env.example" ]]; then
  if [[ ! -f "$worktree_path/.env" ]]; then
    cp "$root_workspace_path/.env.example" "$worktree_path/.env"
    echo "  ✓ Copied .env.example to .env" >&2
  fi
fi

# Copy other untracked configuration files if needed
for config_file in "config/local.yaml" "config/dev.yaml"; do
  if [[ -f "$root_workspace_path/$config_file" ]]; then
    if [[ ! -f "$worktree_path/$config_file" ]]; then
      mkdir -p "$(dirname "$worktree_path/$config_file")"
      cp "$root_workspace_path/$config_file" "$worktree_path/$config_file"
      echo "  ✓ Copied $config_file" >&2
    fi
  fi
done

exit 0
