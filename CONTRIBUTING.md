# Contributing

## Prerequisites

- **Go** toolchain matching the **`go`** and **`toolchain`** lines in [`go.mod`](go.mod) (currently **Go 1.26.2**; the **`toolchain`** directive selects that release via [Go toolchains](https://go.dev/doc/toolchain)). See also [`docs/context/MODERN_GO.md`](docs/context/MODERN_GO.md).
- Optional but recommended: **`golangci-lint`** v2 (same major as [`.golangci.yml`](.golangci.yml)) for `make lint` and full [`scripts/ci.sh`](scripts/ci.sh).
- **`govulncheck`** is invoked via **`go run golang.org/x/vuln/cmd/govulncheck@latest`** inside [`scripts/ci.sh`](scripts/ci.sh) — no separate install; needs network on first run (module cache afterward).
- Optional: **[pre-commit](https://pre-commit.com)** for Git hooks ([`.pre-commit-config.yaml`](.pre-commit-config.yaml)).

## Clone and run

```bash
git clone <your-fork-or-upstream-url>
cd go-hexagonal-architecture-template
go test ./...
make run
```

Copy [`.env.example`](.env.example) to `.env` if you use a local env file (the app does not load `.env` by itself — export variables in your shell or use a process manager).

## Quality gates

**`make ci`** is the single pre-push gate: it runs **[`scripts/ci.sh`](scripts/ci.sh)** — the same script **GitHub Actions** invokes (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)). You do **not** need to run the script separately unless you are debugging one step. A bare **`make`** (no target) runs the same pipeline.

```bash
make
# same as: make ci
```

That includes: **`gofmt -l`**, **`go vet ./...`**, **`go test -race ./...`**, **`govulncheck ./...`** (via **`go run`** as above), **`golangci-lint run`**, and **`go mod tidy`** (with a clean `go.mod` / `go.sum` check when **`CI=true`**).

Shortcuts:

| Command | Purpose |
| --- | --- |
| `make test` | Tests with `-race` |
| `make vet` | `go vet` only |
| `make govulncheck` | Vulnerability scan only (also part of `make ci`) |
| `make lint` | golangci-lint (requires binary on `PATH`) |
| `make fmt` | `gofmt -w` on module `.go` files |
| `make clean` | Remove `bin/` (from `make build`) |
| `make help` | List Makefile targets |

## Where tests live

| Package | Role |
| --- | --- |
| [`internal/domain`](internal/domain) | Pure logic; table-driven tests, no I/O. |
| [`internal/app`](internal/app) | Use cases + ports; tests use [`internal/adapters/out/memory`](internal/adapters/out/memory) or fakes. |
| [`internal/adapters/in/http`](internal/adapters/in/http) | HTTP mapping; uses **`net/http/httptest`**. |

Prefer **table-driven** tests and **`t.Parallel()`** where data is independent. Integration tests behind **`//go:build integration`** are optional if you add them later.

## Architecture

Follow **[`docs/context/HEXAGONAL_ARCHITECTURE.md`](docs/context/HEXAGONAL_ARCHITECTURE.md)** — dependency direction is **`cmd` → `internal/adapters` → `internal/app` / `internal/domain`**. Adapters must not define business rules. **`golangci-lint`** enables **`depguard`**: **`internal/domain`** and **`internal/app`** must not import **`internal/adapters`** (see [`.golangci.yml`](.golangci.yml)).

## Supply chain (repo automation)

- **Dependabot** — [`.github/dependabot.yml`](.github/dependabot.yml) opens weekly PRs for **`gomod`** dependency updates (tune schedule and limits there).
- **Secret scanning** — enable **secret scanning** (and push protection if your plan allows) in the repository or organization **Settings → Code security**. That is the primary guard against committed credentials; editor hooks are a convenience, not a substitute.

## Cursor / editor assets

This repo **tracks** **`.cursor/`** (rules, skills, optional hook scripts referenced by **`.cursor/hooks.json`**) so forks get the same agent and editor guidance. It is **not** listed in **`.gitignore`**.
