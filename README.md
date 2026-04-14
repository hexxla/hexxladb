# HexxlaDB

**HexxlaDB** is the embedded storage engine for **[Hexxla](https://github.com/hexxla/hexxla)**: a hex-native, graph-aware, temporal, provenance-first database that treats the hexagonal lattice as a first-class addressing model (Morton-packed keys, ring walks as native range scans, distinct Edge vs Seam storage families, MVCC for `as_of`). It is a custom on-disk engine—not a third-party ordered-KV or SQL core with adapters on top. The canonical specification is **[`docs/hexxladb/HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)**.

This repository is a [hexagonal](docs/context/HEXAGONAL_ARCHITECTURE.md) Go module: **`cmd/`**, **`internal/`**, **`docs/`** (architecture handbook under **`docs/context/`**, database spec under **`docs/hexxladb/`**, **[development roadmap](docs/hexxladb/DEVELOPMENT_ROADMAP.md)**, v1 implementation checklist under **[`docs/checklist/HEXXLA_DB_V1.md`](docs/checklist/HEXXLA_DB_V1.md)**), **`scripts/`**, **`.github/`**, **`.cursor/`**. The **[composition root](docs/context/HEXAGONAL_ARCHITECTURE.md#composition-root-cmdmaingo)** pattern is documented in **`docs/context/HEXAGONAL_ARCHITECTURE.md`**. **`hexxladb.Open` / `DB.Close`** (milestone **M3**) use **`internal/engine`**: 64 KiB pages, primary file + WAL, redo replay on open — see **`internal/engine/ENGINE_FORMAT.md`**. Milestone **M4** adds a **B+ tree** (`ORDERED_STORE.md`) and **`cell/`** key encoding in **`internal/index`**. Milestone **M5** adds **`View` / `Update` / `*Tx`** with reader/writer locking — see **`docs/hexxladb/TX.md`**. Contributor setup: **[`CONTRIBUTING.md`](CONTRIBUTING.md)** · local env template: **[`.env.example`](.env.example)** · **API and format compatibility:** **[`VERSIONING.md`](VERSIONING.md)** · **spec vs shipped:** **[`docs/hexxladb/SPEC_IMPLEMENTATION_STATUS.md`](docs/hexxladb/SPEC_IMPLEMENTATION_STATUS.md)** · **gap analysis and integration plan:** **[`docs/hexxladb/SPEC_GAP_ANALYSIS_AND_INTEGRATION_PLAN.md`](docs/hexxladb/SPEC_GAP_ANALYSIS_AND_INTEGRATION_PLAN.md)** · **microbenchmarks (reference table):** **[`docs/hexxladb/BENCHMARKS.md`](docs/hexxladb/BENCHMARKS.md)** · **operations (files, backup, encryption):** **[`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)** · release notes: **[`CHANGELOG.md`](CHANGELOG.md)** (how-to and examples).

**Repository:** https://github.com/hexxla/hexxladb

## Composition root (`cmd/hexxladb`)

**[`cmd/hexxladb/main.go`](cmd/hexxladb/main.go)** is the repository’s composition root: config → logging → optional **`HEXXLA_DB_PATH`** → **`hexxladb.Open`** → **`internal/adapters/out/hexxladb`** [`Storage`](internal/adapters/out/hexxladb/storage.go) → **`app.NewWithStorage`** → a **PutCell/GetCell smoke test** on the domain port, then exit. Without **`HEXXLA_DB_PATH`**, the DB is not opened and **`app.Storage`** is nil. See **[`SPEC_IMPLEMENTATION_STATUS.md`](docs/hexxladb/SPEC_IMPLEMENTATION_STATUS.md)** for how this relates to [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md). Copy or adapt **`main`** when you integrate the library; keep rules out of `main` ([HEXAGONAL_ARCHITECTURE.md](docs/context/HEXAGONAL_ARCHITECTURE.md)).

```bash
HEXXLA_DB_PATH=/tmp/hexxla.db make run
# or: HEXXLA_DB_PATH=/tmp/hexxla.db go run ./cmd/hexxladb
```

Build a binary:

```bash
make build
./bin/hexxladb
```

### Configuration (environment)

| Variable    | Default | Meaning                                            |
| ----------- | ------- | -------------------------------------------------- |
| `LOG_LEVEL` | `INFO`  | `slog` level (`DEBUG`, `INFO`, `WARN`, `ERROR`, …) |

**Quality gates:** **`make`** or **`make ci`** (runs **`scripts/ci.sh`** — same as GitHub Actions: format, **`go vet`**, tests with **`-race`**, **`govulncheck`**, **`golangci-lint`**, **`go mod tidy`**). Dependency update PRs: **`.github/dependabot.yml`**. Optional **[pre-commit](https://pre-commit.com)** hooks in **`.pre-commit-config.yaml`** — install with **`make pre-commit-install`**.
