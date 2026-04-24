#!/usr/bin/env bash
# PostToolUse hook — warn on high-signal secret patterns in the edited file (stderr only; does not block).
# Complement with git-secrets, gitleaks in CI, or partner integrations.
# Claude PostToolUse input: {"file_path": "..."} via stdin
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
	exit 0
fi

input=$(cat)
file_path=$(echo "$input" | jq -r '.file_path // empty')

[[ -n "$file_path" ]] || exit 0
[[ -f "$file_path" ]] || exit 0

# Skip binary-ish files; grep -I ignores binary without scanning
if ! grep -q . "$file_path" 2>/dev/null; then
	exit 0
fi

warn() {
	echo "claude hook (secrets): $*" >&2
}

# High-signal patterns only (reduce noise vs scanning whole repo on every edit).
if grep -I -qE 'AKIA[0-9A-Z]{16}' "$file_path" 2>/dev/null; then
	warn "possible AWS access key id in: $file_path"
fi
if grep -I -qE 'gh[pousr]_[A-Za-z0-9_]{20,}' "$file_path" 2>/dev/null; then
	warn "possible GitHub token in: $file_path"
fi
if grep -I -qE '-----BEGIN (RSA |OPENSSH |EC )?PRIVATE KEY-----' "$file_path" 2>/dev/null; then
	warn "PEM / private key material in: $file_path"
fi
if grep -I -qE 'xox[baprs]-[0-9A-Za-z-]{10,}' "$file_path" 2>/dev/null; then
	warn "possible Slack token in: $file_path"
fi

exit 0
