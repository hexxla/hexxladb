# HexxlaDB

**HexxlaDB** is the embedded storage engine for **[Hexxla](https://github.com/hexxla/hexxla)**: a hex-native, graph-aware, temporal, provenance-first database that treats the hexagonal lattice as a first-class addressing model (Morton-packed keys, ring walks as native range scans, distinct Edge vs Seam storage families, MVCC for `as_of`). It is a custom on-disk engine—not a third-party ordered-KV or SQL core with adapters on top. The canonical specification is **[`docs/hexxladb/HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)**.

This repository is a [hexagonal](docs/context/HEXAGONAL_ARCHITECTURE.md) Go module: **`cmd/`**, **`internal/`**, **`docs/`** (architecture handbook under **`docs/context/`**, database spec under **`docs/hexxladb/`**, **[development roadmap](docs/hexxladb/DEVELOPMENT_ROADMAP.md)**, v1 implementation checklist under **[`docs/checklist/HEXXLA_DB_V1.md`](docs/checklist/HEXXLA_DB_V1.md)**), **`scripts/`**, **`.github/`**, **`.cursor/`**. The **[composition root](docs/context/HEXAGONAL_ARCHITECTURE.md#composition-root-cmdmaingo)** pattern is documented in **`docs/context/HEXAGONAL_ARCHITECTURE.md`**. **`hexxladb.Open` / `DB.Close`** (milestone **M3**) use **`internal/engine`**: 64 KiB pages, primary file + WAL, redo replay on open — see **`internal/engine/ENGINE_FORMAT.md`**. Milestone **M4** adds a **B+ tree** (`ORDERED_STORE.md`) and **`cell/`** key encoding in **`internal/index`**. Milestone **M5** adds **`View` / `Update` / `*Tx`** with reader/writer locking — see **`docs/hexxladb/TX.md`**. Contributor setup: **[`CONTRIBUTING.md`](CONTRIBUTING.md)** · local env template: **[`.env.example`](.env.example)** · **API and format compatibility:** **[`VERSIONING.md`](VERSIONING.md)** · **HEXXLA readiness roadmap:** **[`docs/hexxladb/HEXXLA_READINESS_ROADMAP.md`](docs/hexxladb/HEXXLA_READINESS_ROADMAP.md)** · **microbenchmarks (reference table):** **[`docs/hexxladb/BENCHMARKS.md`](docs/hexxladb/BENCHMARKS.md)** · **marketing / claim capture (fill-in tables):** **[`docs/hexxladb/MARKETING_BENCHMARKS.md`](docs/hexxladb/MARKETING_BENCHMARKS.md)** · **operations (files, backup, encryption):** **[`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)** · release notes: **[`CHANGELOG.md`](CHANGELOG.md)** (how-to and examples).

**Repository:** [github.com/hexxla/hexxladb](https://github.com/hexxla/hexxladb)

## Composition root (`cmd/hexxladb`)

**[`cmd/hexxladb/main.go`](cmd/hexxladb/main.go)** is the repository’s composition root: config → logging → optional DB path → **`hexxladb.Open`**. With a path set, it wires **`internal/adapters/out/hexxladb`** [`Storage`](internal/adapters/out/hexxladb/storage.go) → **`app.NewWithStorage`** and runs a **PutCell/GetCell smoke test** on the domain port. **`-demo`** skips that wiring and instead writes a **grid of cells**, scans by source, **closes and reopens** the file, and verifies reads (public **`Tx`** API only — easy to copy into your app). Without **`HEXXLA_DB_PATH`** and without **`-path`**, the DB is not opened (unless **`-demo`** supplies **`-path`**). See **[`HEXXLA_READINESS_ROADMAP.md`](docs/hexxladb/HEXXLA_READINESS_ROADMAP.md)** for readiness and remaining integration work against [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md). Copy or adapt **`main`** when you integrate the library; keep rules out of `main` ([HEXAGONAL_ARCHITECTURE.md](docs/context/HEXAGONAL_ARCHITECTURE.md)).

Smoke test (domain port, single cell):

```bash
HEXXLA_DB_PATH=.tmp/hexxla.db make run
# or: HEXXLA_DB_PATH=.tmp/hexxla.db go run ./cmd/hexxladb
```

Multi-cell demo (grid **`PutCell`**, **`AscendCellsBySource`**, reopen **`GetCell`**):

```bash
go run ./cmd/hexxladb -demo -path .tmp/hexxla-demo.db -cells 500
# equivalent: HEXXLA_DB_PATH=.tmp/hexxla-demo.db go run ./cmd/hexxladb -demo -cells 500
```

**`domain.Storage` tour** (cells, secondaries, rings, context, facets, edges, seams — same port the Hexxla adapter implements):

```bash
go run ./examples/storage_walkthrough -path .tmp/hexxla-walkthrough.db
```

Build a binary:

```bash
make build
./bin/hexxladb -demo -path .tmp/hexxla-demo.db
```

### Flags (`cmd/hexxladb`)

- `-demo`: run the grid write / scan / reopen demo (requires **`-path`** or **`HEXXLA_DB_PATH`**).
- `-path`: database file path (**overrides** **`HEXXLA_DB_PATH`** when non-empty).
- `-cells`: cells to write with **`-demo`** (default **500**, max **50000**).

### Configuration (environment)

- `LOG_LEVEL` (default: `INFO`): `slog` level (`DEBUG`, `INFO`, `WARN`, `ERROR`, ...).
- `HEXXLA_DB_PATH` (default: empty): default DB path when **`-path`** is not set.

**Quality gates:** **`make`** or **`make ci`** (runs **`scripts/ci.sh`** — same as GitHub Actions: format, **`go vet`**, tests with **`-race`**, **`govulncheck`**, **`golangci-lint`**, **`go mod tidy`**). Dependency update PRs: **`.github/dependabot.yml`**. Optional **[pre-commit](https://pre-commit.com)** hooks in **`.pre-commit-config.yaml`** — install with **`make pre-commit-install`**.
