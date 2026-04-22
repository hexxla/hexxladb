# Contributing

## Prerequisites

- **Go** toolchain matching the **`go`** and **`toolchain`** lines in [`go.mod`](go.mod) (currently **Go 1.26.2**; the **`toolchain`** directive selects that release via [Go toolchains](https://go.dev/doc/toolchain)). See also [`docs/context/MODERN_GO.md`](docs/context/MODERN_GO.md).
- Optional but recommended: **`golangci-lint`** v2 (same major as [`.golangci.yml`](.golangci.yml)) for `make lint` and full [`scripts/ci.sh`](scripts/ci.sh).
- **`govulncheck`** is invoked via **`go run golang.org/x/vuln/cmd/govulncheck@latest`** inside [`scripts/ci.sh`](scripts/ci.sh) — no separate install; needs network on first run (module cache afterward).
- Optional: **[pre-commit](https://pre-commit.com)** for Git hooks ([`.pre-commit-config.yaml`](.pre-commit-config.yaml)).

## Clone and run

```bash
git clone <your-fork-or-upstream-url>
cd hexxladb
go test ./...
make run
```

Copy [`.env.example`](.env.example) to `.env` if you use a local env file (the binary does not load `.env` by itself — export variables in your shell or use a process manager).

## Branches and pull requests

- **`main`** is the integration branch: keep it **CI-green** (`make ci`) before you push.
- **Feature work:** branch from up-to-date **`main`** (`feat/…`, `fix/…`). Open a **pull request** into `main` for review and merge.
- **Spikes / experiments:** use a dedicated branch (e.g. `spike/…`); merge to `main` only when the team wants that code and docs on the default branch. After a spike is merged, continue new work from **`main`**, not from the old spike tip (unless you explicitly depend on unmerged commits).

## Quality gates

**`make ci`** is the single pre-push gate: it runs **[`scripts/ci.sh`](scripts/ci.sh)** — the same script **GitHub Actions** invokes (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)). You do **not** need to run the script separately unless you are debugging one step. A bare **`make`** (no target) runs the same pipeline.

```bash
make
# same as: make ci
```

That includes: **`gofmt -l`**, **`go vet ./...`**, **`go test -race ./...`**, **`govulncheck ./...`** (via **`go run`** as above), **`golangci-lint run`**, and **`go mod tidy`** (with a clean `go.mod` / `go.sum` check when **`CI=true`**).

Shortcuts:

| Command             | Purpose                                                                                                                                             |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `make test`         | Tests with `-race`                                                                                                                                  |
| `make vet`          | `go vet` only                                                                                                                                       |
| `make govulncheck`  | Vulnerability scan only (also part of `make ci`)                                                                                                    |
| `make lint`         | golangci-lint (requires binary on `PATH`)                                                                                                           |
| `make fmt`          | `gofmt -w` on module `.go` files                                                                                                                    |
| `make clean`        | Remove `bin/` (from `make build`)                                                                                                                   |
| `make help`         | List Makefile targets                                                                                                                               |
| `make integration`  | Optional **`//go:build integration`** tests (`-race`); not part of default CI                                                                       |
| `make stress`       | Optional **`//go:build stress`** tests (very large `PutCell` counts; **`TMPDIR`** defaults to repo `./.tmp`; **not** CI)                             |
| `make bench`        | Benchmarks (`go test -bench=. -benchmem ./...`); **not** run in default CI                                                                          |
| `make bench-stress` | Longer **`BenchmarkAPI_*`** (preload 512 / 2k / 10k per sub-bench; **not** 50k — see [`BENCHMARKS.md`](docs/hexxladb/BENCHMARKS.md)); **not** in CI |
| `make fuzz`         | Short fuzz smoke on internal decoders (~2s per target); **not** in default CI                                                                       |

## Benchmarks and fuzzing

- **`make bench`** runs all benchmarks in the module. For a single package: `go test -bench=. -benchmem ./internal/lattice`.
- **`make fuzz`** runs a **short** smoke (`-fuzztime=2s` per target) on [`internal/record`](internal/record) and [`internal/engine`](internal/engine). For longer or corpus-updating runs, use e.g. `go test ./internal/record -fuzz=FuzzDecodeCell -fuzztime=30s` (see [Go fuzzing](https://go.dev/doc/security/fuzz/)).

Compatibility expectations for releases and on-disk format: **[`VERSIONING.md`](VERSIONING.md)**.

## Where tests live

| Package                                  | Role                                                                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`internal/domain`](internal/domain)     | Pure logic; table-driven tests, no I/O.                                                                                                           |
| [`internal/app`](internal/app)           | Use cases + ports; tests use fakes or stub implementations until adapters exist.                                                                  |
| [`internal/adapters`](internal/adapters) | Add **`in/`** / **`out/`** packages when you add transports or infrastructure (see [`internal/adapters/README.md`](internal/adapters/README.md)). |

Prefer **table-driven** tests and **`t.Parallel()`** where data is independent. **Durability / heavier tests** use **`//go:build integration`** (see [`db_durability_test.go`](db_durability_test.go) vs [`durability_integration_test.go`](durability_integration_test.go)); run them with **`make integration`**. Default **`make ci`** does **not** include the integration tag so PRs stay fast.

**Scale integration:** [`scale_integration_test.go`](scale_integration_test.go) writes **~10k** `PutCell` rows ( **~1k** when **`go test -short`** is set) plus secondary index checks and reopen verification. Expect **seconds** of runtime and **tens of MB** temp disk under `-race`; run before release or when changing the btree / secondaries.

**Stress (tag `stress`):** [`stress_integration_test.go`](stress_integration_test.go) defaults to **100k** cells (override **`HEXXLA_STRESS_CELLS`**, max **500k**). **`make stress`** creates **`./.tmp`** and sets **`TMPDIR`** there (same as **`make bench`**), unless you override **`TMPDIR`** — avoids filling root **`/tmp`**. Expect **minutes**, **hundreds of MB** temp disk, and heavy I/O; not included in **`make integration`**.

## Architecture

Follow **[`docs/context/HEXAGONAL_ARCHITECTURE.md`](docs/context/HEXAGONAL_ARCHITECTURE.md)** — dependency direction is **`cmd` → `internal/adapters` → `internal/app` / `internal/domain`**. Adapters must not define business rules. **`golangci-lint`** enables **`depguard`**: **`internal/domain`** and **`internal/app`** must not import **`internal/adapters`** (see [`.golangci.yml`](.golangci.yml)).

## Supply chain (repo automation)

- **Dependabot** — [`.github/dependabot.yml`](.github/dependabot.yml) opens weekly PRs for **`gomod`** dependency updates (tune schedule and limits there).
- **Secret scanning** — enable **secret scanning** (and push protection if your plan allows) in the repository or organization **Settings → Code security**. That is the primary guard against committed credentials; editor hooks are a convenience, not a substitute.

## IDE / editor assets

This repo **tracks** IDE-specific directories for both Cursor and Windsurf:

- **`.cursor/`** (rules, skills, optional hook scripts referenced by **`.cursor/hooks.json`**)
- **`.windsurf/`** (rules, skills, optional hook scripts referenced by **`.windsurf/hooks.json`**)

These directories are **not** listed in **`.gitignore`** so they're available for forks and contributors using either IDE.
