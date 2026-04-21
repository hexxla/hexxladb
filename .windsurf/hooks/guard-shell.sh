#!/usr/bin/env bash
# pre_run_command — runs only when hooks.json matcher matches (e.g. curl, rm, sudo).
# Asks to user to approve; adjust matcher to balance safety vs friction.
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
		continue: true,
		permission: "ask",
		user_message: "This shell command matched a project hook (network or potentially destructive). Approve only if you intend to run it.",
		agent_message: ("Review before execution: " + $cmd)
	}'
exit 0
