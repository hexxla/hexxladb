# Contributing

## Prerequisites

- **Go** toolchain matching the **`go`** line in [`go.mod`](go.mod) (currently **Go 1.27.0**; [Go toolchains](https://go.dev/doc/toolchain) can select that release automatically).
- **[Task](https://taskfile.dev)** v3 (CI currently pins **v3.53.1**) for repository build and validation workflows.
- Repository analyzers require no global installation. [`scripts/tool.sh`](scripts/tool.sh)
  runs reviewed versions of golangci-lint, Gosec, govulncheck, gocyclo, and
  gocognit under the minimum Go 1.27.0 toolchain. The first run needs network;
  subsequent runs use the Go module and toolchain caches.
- Optional: **[pre-commit](https://pre-commit.com)** for Git hooks ([`.pre-commit-config.yaml`](.pre-commit-config.yaml)).

## Clone and run

```bash
git clone <your-fork-or-upstream-url>
cd hexxladb
go test ./...
task demo
```

Copy [`.env.example`](.env.example) to `.env` if you use a local env file (the binary does not load `.env` by itself — export variables in your shell or use a process manager).

## Branches and pull requests

- **`main`** is the integration branch: keep it **CI-green** (`task ci`) before you push.
- **Feature work:** branch from up-to-date **`main`** (`feat/…`, `fix/…`). Open a **pull request** into `main` for review and merge.
- **Spikes / experiments:** use a dedicated branch (e.g. `spike/…`); merge to `main` only when the team wants that code and docs on the default branch. After a spike is merged, continue new work from **`main`**, not from the old spike tip (unless you explicitly depend on unmerged commits).

## Quality gates

**`task ci`** is the single pre-push gate: it runs **[`scripts/ci.sh`](scripts/ci.sh)** — the same script **GitHub Actions** invokes through Task (see [`.github/workflows/ci.yml`](.github/workflows/ci.yml)). You do **not** need to run the script separately unless you are debugging one step. A bare **`task`** (no task name) runs the same pipeline.

```bash
task
# same as: task ci
```

That includes: **`gofmt -l`**, **`go vet ./...`**, architecture boundaries,
**`go test -race ./...`**, pinned **govulncheck**, **golangci-lint**, standalone
**Gosec**, cyclomatic/cognitive/CRAP analysis, and **`go mod tidy`** (with a
clean `go.mod` / `go.sum` check when **`CI=true`**). No analyzer is skipped when
it is absent from `PATH`.

Shortcuts:

| Command             | Purpose                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `task test`         | Tests with `-race`                                                                                                                                                                                                                                                                                                                                                                                          |
| `task vet`          | `go vet` only                                                                                                                                                                                                                                                                                                                                                                                               |
| `task govulncheck`  | Vulnerability scan only (also part of `task ci`)                                                                                                                                                                                                                                                                                                                                                            |
| `task lint`         | Pinned golangci-lint through `scripts/tool.sh`                                                                                                                                                                                                                                                                                                                                                              |
| `task fmt`          | `gofmt -w` on module `.go` files                                                                                                                                                                                                                                                                                                                                                                            |
| `task clean`        | Remove `bin/` (from `task build`)                                                                                                                                                                                                                                                                                                                                                                           |
| `task help`         | List Taskfile tasks                                                                                                                                                                                                                                                                                                                                                                                         |
| `task integration`  | Optional **`//go:build integration`** tests (`-race -parallel=1`); not part of default CI. **`parallel=1`** keeps subprocess [**crash / SIGKILL** tests](crash_ordering_integration_test.go) serial — without it, **`t.Parallel`** scale tests plus five concurrent crash children can thrash **`-race`** and look stuck. Matches [`.github/workflows/integration.yml`](.github/workflows/integration.yml). |
| `task stress`       | Optional **`//go:build stress`** tests (very large `PutCell` counts; **`TMPDIR`** defaults to repo `./.tmp`; **not** CI)                                                                                                                                                                                                                                                                                    |
| `task bench`        | Benchmarks (`go test -bench=. -benchmem ./...`); **not** run in default CI                                                                                                                                                                                                                                                                                                                                  |
| `task bench-stress` | Longer **`BenchmarkAPI_*`** (preload 512 / 2k / 10k per sub-bench); **not** in CI                                                                                                                                                                                                                                                                                                                           |
| `task fuzz`         | Short fuzz smoke on internal decoders (~2s per target); **not** in default CI                                                                                                                                                                                                                                                                                                                               |

## Benchmarks and fuzzing

- **`task bench`** runs all benchmarks in the module. For a single package: `go test -bench=. -benchmem ./internal/lattice`.
- **`task fuzz`** runs a **short** smoke (`-fuzztime=2s` per target) on [`internal/record`](internal/record) and [`internal/engine`](internal/engine). For longer or corpus-updating runs, use e.g. `go test ./internal/record -fuzz=FuzzDecodeCell -fuzztime=30s` (see [Go fuzzing](https://go.dev/doc/security/fuzz/)).

Compatibility expectations for releases and on-disk format: **[`VERSIONING.md`](VERSIONING.md)**.

## Roadmap and out-of-scope boundaries

Technical backlog, explicit non-goals, and **documented vs implemented** audit notes live in **[`docs/ROADMAP.md`](docs/ROADMAP.md)**. Operator-facing retention/soak/incident guidance is in **[`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)**.

## Where tests live

| Package                                  | Role                                                                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| [`internal/domain`](internal/domain)     | Pure logic; table-driven tests, no I/O.                                                                                                           |
| [`internal/app`](internal/app)           | Use cases + ports; tests use fakes or stub implementations until adapters exist.                                                                  |
| [`internal/adapters`](internal/adapters) | Add **`in/`** / **`out/`** packages when you add transports or infrastructure (see [`internal/adapters/README.md`](internal/adapters/README.md)). |

Prefer **table-driven** tests and **`t.Parallel()`** where data is independent. **Durability / heavier tests** use **`//go:build integration`** (see [`db_durability_test.go`](db_durability_test.go) vs [`durability_integration_test.go`](durability_integration_test.go)); run them with **`task integration`**. Default **`task ci`** does **not** include the integration tag so PRs stay fast.

**Crash ordering (`SIGKILL`):** [`crash_ordering_integration_test.go`](crash_ordering_integration_test.go) spawns **`TestIntegration_crashChild`** subprocesses blocked on named barriers (`HEXXLADB_TEST_CRASH_AT`). Use **`task integration`** or **`go test -race -parallel=1 -tags=integration ./...`** (not a high **`go test -parallel`**) locally so these do not overlap each other.

**Scale integration:** [`scale_integration_test.go`](scale_integration_test.go) writes **~10k** `PutCell` rows ( **~1k** when **`go test -short`** is set) plus secondary index checks and reopen verification. Expect **seconds** of runtime and **tens of MB** temp disk under `-race`; run before release or when changing the btree / secondaries.

**Stress (tag `stress`):** [`stress_integration_test.go`](stress_integration_test.go) defaults to **100k** cells (override **`HEXXLA_STRESS_CELLS`**, max **500k**). **`task stress`** creates **`./.tmp`** and sets **`TMPDIR`** there (same as **`task bench`**), unless you override **`TMPDIR`** — avoids filling root **`/tmp`**. Expect **minutes**, **hundreds of MB** temp disk, and heavy I/O; not included in **`task integration`**.

## Architecture

Follow **[`docs/architecture/HEXAGONAL_ARCHITECTURE.md`](docs/architecture/HEXAGONAL_ARCHITECTURE.md)** — dependency direction is **`cmd` → `internal/adapters` → `internal/app` / `internal/domain`**. Adapters must not define business rules. **`golangci-lint`** enables **`depguard`**: **`internal/domain`** and **`internal/app`** must not import **`internal/adapters`** (see [`.golangci.yml`](.golangci.yml)).

## Supply chain (repo automation)

- **Dependabot** — [`.github/dependabot.yml`](.github/dependabot.yml) opens weekly PRs for **`gomod`** dependency updates (tune schedule and limits there).
- **Secret scanning** — enable **secret scanning** (and push protection if your plan allows) in the repository or organization **Settings → Code security**. That is the primary guard against committed credentials; editor hooks are a convenience, not a substitute.

## Editor and agent assets

The repository tracks shared settings for **VS Code** in [`.vscode/`](.vscode) and **Zed** in [`.zed/`](.zed). Both editors expose the same repository-native commands from [`Taskfile.yml`](Taskfile.yml); contributors remain free to use any editor.

Repository-specific Codex skills belong in [`.agents/skills/`](.agents/skills). Each skill is a directory containing a `SKILL.md`; add one only after a repeated HexxlaDB workflow has demonstrated that project-specific guidance is useful.
