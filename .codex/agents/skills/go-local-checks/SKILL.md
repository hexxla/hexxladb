---
name: go-local-checks
description: Run standard local Go quality checks for this repo. Use when validating changes, before commits, or when the user asks about tests, fmt, vet, or lint.
---

# Go local checks

## Canonical command

Use **`make ci`** or **`./scripts/ci.sh`** — same steps as GitHub Actions (format check, `go vet`, `go test -race`, **govulncheck**, **golangci-lint**, `go mod tidy` + git check when `go.mod` is tracked or `CI=true`). Optionally install **Git pre-commit** hooks via **`make pre-commit-install`** (see **`.pre-commit-config.yaml`**) for checks on every commit; still run **`make ci`** before pushing.

## Tooling model (modern Go)

- **Type checking:** Go does not use a separate "type checker" tool. The **compiler** type-checks when you run `go build`, `go test`, or `go vet`. **`staticcheck**` (via golangci-lint) adds deep static analysis beyond the compiler.
- **Linting:** **[golangci-lint](https://golangci-lint.run/) v2** with `.golangci.yml` — **`linters.default: standard`** (govet, staticcheck, errcheck, ineffassign, unused) **plus** enabled linters such as **errorlint**, **gosec**, **gocritic**, **revive**, **bodyclose**, **misspell**, **modernize**, **nolintlint**, **copyloopvar**. See [linters](https://golangci-lint.run/docs/linters/) and the config file.
- **Formatting:** CI uses **`gofmt -l`** in `scripts/ci.sh`. `.golangci.yml` enables **`gofmt`** (with **`simplify: true`**) and **`goimports`** with **`local-prefixes`** set to this module so **`golangci-lint fmt ./...`** sorts imports and groups `github.com/hexxla/...` after third-party code ([formatter settings](https://golangci-lint.run/docs/formatters/configuration/)). Run **`make fmt`** for plain `gofmt -w`, or **`golangci-lint fmt ./...`** for goimports + gofmt together.

## IDE hooks (present)

IDE-specific hooks require **`jq`** on `PATH`. They run **`gofmt -w`** after Go writes, warn on stderr for common secret patterns in edited files, gate some shell commands, and block prompts that look like pasted tokens. This does **not** replace secret scanning in CI (**gitleaks**, etc.).

Hooks are configured in `.codex/hooks.json`.

## Shell: `golangci-lint` not found (Fish)

`go install …` puts binaries in **`(go env GOPATH)/bin`**. That directory must be on **`PATH`** in **every** new shell.

- A one-off **`set -gx PATH …`** only applies to the **current** session; it is **not** saved.
- Add this to **`~/.config/fish/config.fish`** so it runs on login/interactive startup:

```fish
if command -q go
    fish_add_path (go env GOPATH)/bin
end
```

**Fish 3.2+** provides **`fish_add_path`**, which prepends the path and avoids duplicates. If your Fish is older, use:

```fish
set -gx PATH (go env GOPATH)/bin $PATH
```

inside **`config.fish`** (not typed manually each time).

After editing **`config.fish`**, run **`source ~/.config/fish/config.fish`** or open a new terminal, then **`command -v golangci-lint`**.

## Makefile targets

| Target             | Purpose                         |
| ------------------ | ------------------------------- |
| `make ci`          | Full pipeline (see above)       |
| `make test`        | `go test -race ./...`           |
| `make vet`         | `go vet ./...`                  |
| `make fmt`         | `gofmt -w` on all `.go` files   |
| `make lint`        | `golangci-lint run ./...`       |
| `make mod-tidy`    | `go mod tidy`                   |
| `make govulncheck` | vulnerability scan only         |
| `make clean`       | remove `bin/` from `make build` |
