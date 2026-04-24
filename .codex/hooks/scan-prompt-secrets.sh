#!/usr/bin/env bash
# Codex UserPromptSubmit hook — block submission for very high-confidence secret patterns.
set -euo pipefail

# Codex UserPromptSubmit input: {"prompt": "..."} via stdin
# Codex expects JSON output: {"continue": true/false, "user_message": "..."}

if ! command -v jq >/dev/null 2>&1; then
	echo '{"continue":true}'
	exit 0
fi

input=$(cat)
prompt=$(echo "$input" | jq -r '.prompt // empty')

if [[ -z "$prompt" ]]; then
	echo '{"continue":true}'
	exit 0
fi

if printf '%s' "$prompt" | grep -qE 'ghp_[a-zA-Z0-9]{36,}'; then
	jq -n '{"continue": false, "user_message": "This message looks like it contains a GitHub personal access token. Remove or redact it before sending."}'
	exit 0
fi

if printf '%s' "$prompt" | grep -qE 'AKIA[0-9A-Z]{16}'; then
	jq -n '{"continue": false, "user_message": "This message looks like it contains an AWS access key id. Redact secrets before sending."}'
	exit 0
fi

if printf '%s' "$prompt" | grep -qE '-----BEGIN [A-Z0-9 ]+PRIVATE KEY-----'; then
	jq -n '{"continue": false, "user_message": "This message looks like it contains a private key. Do not paste private keys into chat."}'
	exit 0
fi

echo '{"continue":true}'
exit 0
