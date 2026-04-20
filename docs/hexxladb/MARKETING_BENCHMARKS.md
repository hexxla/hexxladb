# Marketing-oriented benchmark claims (reference)

This document is for **communicating performance** (website, decks, comparisons). For **how to run** benchmarks and engineering interpretation, see **[`BENCHMARKS.md`](./BENCHMARKS.md)** and **[`CONTRIBUTING.md`](../../CONTRIBUTING.md)**.

**Important:** Absolute **ns/op** and **MB/s** figures are **machine-, OS-, and filesystem-dependent**. Use this file to record numbers **after** you capture them on a **named reference environment** (hardware model, SSD vs NVMe, `GOMAXPROCS`, Go version from [`go.mod`](../../go.mod)). Prefer **before/after** comparisons on the **same** machine when claiming improvements.

## How to capture numbers for this table

Template (fill in after each release or when updating claims):

```bash
go version
uname -a
# Optional: document disk type (e.g. NVMe model)

# Full public API read/scan sweep (512 / 2k / 10k preloads; default TMPDIR is repo-local ./.tmp):
make bench-stress

# Optional: include cells_50000 (many huge DBs):
# make bench-stress HEXXLA_BENCH_PRELOAD=extreme TMPDIR=/large-disk/tmp

# Lattice + engine microbenches (in-memory / engine hot paths):
make bench
```

Set **`HEXXLA_BENCH_PRELOAD=all`** for **cells_10000**, or **`extreme`** for **cells_10000** and **cells_50000** (see [`api_bench_test.go`](../../api_bench_test.go)). **`make bench-stress`** sets **`all`** only.

## Coverage matrix (API area → benchmark)

| API area | Benchmark(s) | Status |
| -------- | ------------ | ------ |
| Lattice Pack/Unpack/Distance | `internal/lattice` `BenchmarkPack`, etc. | Shipped |
| Record encode/decode cell | `internal/record` `BenchmarkEncodeDecodeCell` | Shipped |
| Engine B+ tree Get/Put/AscendRange | `internal/engine` `BenchmarkBTree*` | Shipped |
| `PutCell` (one insert per iter, growing DB) | `BenchmarkAPI_PutCell` | Shipped |
| `PutCell` with MVCC enabled | `BenchmarkAPI_PutCell_MVCC` | Shipped |
| `GetCell` after preload | `BenchmarkAPI_GetCell/cells_*` | Shipped |
| `GetCell` encrypted | `BenchmarkAPI_GetCell_Encrypted/cells_*` | Shipped |
| `AscendCellsBySource` full scan | `BenchmarkAPI_AscendCellsBySource/cells_*` | Shipped |
| `LoadContext` neighborhood | `BenchmarkAPI_LoadContext/cells_*` | Shipped |
| `LoadContextAt` (validity filter) | `BenchmarkAPI_LoadContextAt/cells_*` | Shipped |
| `WalkRing` (raw bytes per ring cell) | `BenchmarkAPI_WalkRing/cells_*` | Shipped |
| `WalkRingAt` (validity filter) | `BenchmarkAPI_WalkRingAt/cells_*` | Shipped |
| Seams, facets, edges, changelog | — | Planned (Tier 2 in [`BENCHMARKS.md`](./BENCHMARKS.md); exercise via [`examples/storage_walkthrough`](../../examples/storage_walkthrough/main.go)) |
| Raw KV `Tx.Get`/`Put`/`AscendRange` | `internal/engine` (not public API package) | Optional / engine-focused |

## Placeholder narrative table (fill with your captured runs)

Replace **`…`** with values from your environment. **Do not** treat these as guarantees.

| Claim (plain language) | Where it is measured | Example value (fill in) |
| ---------------------- | --------------------- | ----------------------- |
| Morton pack/unpack hot path | `BenchmarkPack` / `Unpack` | … ns/op |
| Single cell read after index warmup | `BenchmarkAPI_GetCell/cells_2000` | … ns/op |
| Full scan of N cells by `source/` | `BenchmarkAPI_AscendCellsBySource/cells_2000` | … ns/op (total) |
| `LoadContext` ball cap (fixed maxR/maxCells) | `BenchmarkAPI_LoadContext/cells_2000` | … ns/op |
| One hex ring walk (`WalkRing`, fixed ring) | `BenchmarkAPI_WalkRing/cells_2000` | … ns/op |
| Ring walk with validity filter | `BenchmarkAPI_WalkRingAt/cells_2000` | … ns/op |

## Suggested talking points (non-numeric)

- **Hex-native addressing:** `PackedCoord` and ring/context walks are first-class API primitives, not SQL approximations.
- **Embedded:** workloads include **WAL + fsync** on writes; quote numbers **with** storage context.
- **Roadmap:** MVCC `read_seq` snapshots are shipped (format v2 + `ViewAt(read_seq)`); wall-clock `time.Time -> read_seq` mapping remains a follow-on.
