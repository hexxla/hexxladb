#!/usr/bin/env bash
# PreToolUse hook — runs before Bash tool when matcher matches (e.g. curl, rm, sudo).
# Asks the user to approve; adjust matcher to balance safety vs friction.
# Claude PreToolUse input: {"tool_name": "Bash", "command": "..."} via stdin
# Claude expects JSON output: {"continue": true/false, "permission": "allow/ask/deny", ...}
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
	echo '{"continue":true,"permission":"allow"}'
	exit 0
fi

input=$(cat)
command=$(echo "$input" | jq -r '.command // empty')

jq -n \
	--arg cmd "$command" \
	'{
		"continue": true,
		"permission": "ask",
		"user_message": "This shell command matched a project hook (network or potentially destructive). Approve only if you intend to run it.",
		"agent_message": ("Review before execution: " + $cmd)
	}'
exit 0
