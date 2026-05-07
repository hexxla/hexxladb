# Security Rules (Always Apply)

- Never hardcode credentials, API keys, or secrets in source files
- Load secrets from environment variables or the config loader under `cmd/`
- Always wrap errors with `%w`; do not expose internal paths or stack traces to callers
- Validate all user input at adapter boundaries before passing to domain/app
- Do not log sensitive data (tokens, keys, PII)
- MCP server is localhost-only; do not expose without TLS + auth
