#!/usr/bin/env bash
# Targeted post-write hook for individual file changes
# Runs formatters and checks on the specific file that was written

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

# Only process Go files
if [[ "$file_path" != *.go ]]; then
  exit 0
fi

# Format the specific file
gofmt -l -s -w "$file_path" 2>/dev/null || true
goimports -l -w "$file_path" 2>/dev/null || true

# Run go vet on the specific package
pkg_dir=$(dirname "$file_path")
go vet "$pkg_dir" 2>/dev/null || true

# Run architecture guardrail on the specific file
./scripts/check-hex-boundaries.sh 2>/dev/null || true

exit 0
