# Microbenchmarks (reference)

This document records **how** to run engine hot-path benchmarks and a **sample** result set for regression comparison. **Absolute numbers are machine-dependent** — always compare on the same hardware and Go version.

**Slide-style claims:** capture numbers on a **named** machine first (`go version`, CPU, disk, `TMPDIR`), then reuse the **coverage matrix** below to say what was measured.

**Correctness / MVCC churn (not microbench):** use **`make integration`** for MVCC sustained-update + prune coverage; see **[`OPERATIONS.md`](./OPERATIONS.md)** soak checklist.

## How to run

```bash
# make bench / make bench-stress default TMPDIR to repo-local ./.tmp
make bench
# or:
go test -count=1 -bench=. -benchmem ./internal/lattice ./internal/engine ./internal/record

# Public API: PutCell (single bench) + read/scan with sub-benchmarks cells_512 … cells_10000 (or cells_50000 with extreme):
go test -count=1 -bench=BenchmarkAPI -benchmem ./.

# Default read/scan sub-benchmarks use preload 512 and 2000 only. Add 10k: HEXXLA_BENCH_PRELOAD=all. Add 50k (needs lots of disk under $TMPDIR):
HEXXLA_BENCH_PRELOAD=all TMPDIR=$(pwd)/.tmp go test -count=1 -bench=BenchmarkAPI -benchmem ./.
HEXXLA_BENCH_PRELOAD=extreme TMPDIR=$(pwd)/.tmp go test -count=1 -bench=BenchmarkAPI -benchmem ./.   # adds cells_50000; needs lots of disk

# Longer API read/scan sub-benchmarks (500ms per sub-name; 512 / 2k / 10k only — not in CI):
make bench-stress

# One preload size, e.g. 10k tree only:
go test -count=1 -bench='BenchmarkAPI_GetCell/cells_10000' -benchmem -benchtime=1s ./.
```

See [`api_bench_test.go`](../../api_bench_test.go) — includes baseline **`BenchmarkAPI_PutCell`**, MVCC variant **`BenchmarkAPI_PutCell_MVCC`**, encrypted read **`BenchmarkAPI_GetCell_Encrypted`**, combined **`BenchmarkAPI_GetCell_MVCC_Encrypted`**, and mixed reader/writer **`BenchmarkAPI_ViewUpdateContention`**.

Fuzz smoke (not timed like benchmarks):

```bash
make fuzz
```

See also [CONTRIBUTING.md](../../CONTRIBUTING.md).

## Benchmark coverage matrix (API area → benchmark)

| API area | Benchmark(s) | Notes |
| -------- | ------------ | ----- |
| Lattice Pack/Unpack/Distance | `internal/lattice` `BenchmarkPack`, etc. | Hot path |
| Record encode/decode cell | `internal/record` `BenchmarkEncodeDecodeCell` | |
| Engine B+ tree Get/Put/AscendRange | `internal/engine` `BenchmarkBTree*` | |
| `PutCell` (one insert per iter, growing DB) | `BenchmarkAPI_PutCell` | |
| `PutCell` with MVCC enabled | `BenchmarkAPI_PutCell_MVCC` | |
| `GetCell` after preload | `BenchmarkAPI_GetCell/cells_*` | |
| `GetCell` encrypted | `BenchmarkAPI_GetCell_Encrypted/cells_*` | |
| `GetCell` MVCC + encrypted | `BenchmarkAPI_GetCell_MVCC_Encrypted/cells_*` | |
| Concurrent View + Update | `BenchmarkAPI_ViewUpdateContention` | See interpretation below |
| `AscendCellsBySource` full scan | `BenchmarkAPI_AscendCellsBySource/cells_*` | |
| `LoadContext` neighborhood | `BenchmarkAPI_LoadContext/cells_*` | |
| `LoadContextAt` (validity filter) | `BenchmarkAPI_LoadContextAt/cells_*` | |
| `WalkRing` / `WalkRingAt` | `BenchmarkAPI_WalkRing* / cells_*` | |
| Seams, facets, edges, changelog | — | Exercise via [`examples/storage_walkthrough`](../../examples/storage_walkthrough/main.go) |

## Sample output (one machine)

Captured with: `go test -count=1 -bench=. -benchmem` on **linux/amd64**, Go toolchain from [go.mod](../../go.mod). Benchmark names include `GOMAXPROCS` suffix (e.g. `-32`).

### Lattice ([`internal/lattice`](../../internal/lattice))

| Benchmark     | ns/op | B/op | allocs/op |
| ------------- | ----- | ---- | --------- |
| Pack          | 159   | 0    | 0         |
| Unpack        | 165   | 0    | 0         |
| PackedCompare | 1.02  | 0    | 0         |
| Distance      | 0.76  | 0    | 0         |

### Engine B+ tree ([`internal/engine`](../../internal/engine))

Tree setup: **500** keys for `Get`; **100** keys + update key for `PutUpdate`; **200** keys for `AscendRange` (see [`btree_bench_test.go`](../../internal/engine/btree_bench_test.go)).

| Benchmark        | ns/op  | B/op   | allocs/op |
| ---------------- | ------ | ------ | --------- |
| BTreeGet         | 72,425 | 69,127 | 75        |
| BTreePutUpdate   | 299,163 | 407,042 | 67        |
| BTreeAscendRange | 189,159| 74,837 | 281       |

### Record ([`internal/record`](../../internal/record))

| Benchmark        | ns/op | B/op | allocs/op |
| ---------------- | ----- | ---- | --------- |
| EncodeDecodeCell | 621   | 152  | 7         |

### Public API ([`api_bench_test.go`](../../api_bench_test.go))

Read/scan benchmarks preload **N** cells (same `source/` and `time/` secondaries). Sub-names **`cells_10000`** require **`HEXXLA_BENCH_PRELOAD=all`**; **`cells_50000`** requires **`HEXXLA_BENCH_PRELOAD=extreme`** (heavy disk use). **`make bench-stress`** sets **`all`** only (not **extreme**), so it stays runnable on typical `/tmp` sizes. **`PutCell`** benchmark grows a fresh DB one insert per iteration (unique grid coords). **Numbers vary widely** with disk and `GOMAXPROCS` — capture your own table after `make bench-stress` or a filtered `-bench=…/cells_…`.

- `BenchmarkAPI_PutCell`: one `Update` + `PutCell` per iteration.
- `BenchmarkAPI_PutCell_MVCC`: same workload with `Options.EnableMVCC=true`.
- `BenchmarkAPI_GetCell/cells_N`: hot-key `GetCell` after **N**-cell preload.
- `BenchmarkAPI_GetCell_Encrypted/cells_N`: `GetCell` with AES-XTS enabled.
- `BenchmarkAPI_GetCell_MVCC_Encrypted/cells_N`: `EnableMVCC` + `EncryptionKey` on the same DB (read path).
- `BenchmarkAPI_ViewUpdateContention`: parallel `View` (`GetCell`) vs occasional `Update` (`Tx.Put` on a tiny side key).
- `BenchmarkAPI_AscendCellsBySource/cells_N`: full `source/` scan over **N** rows.
- `BenchmarkAPI_LoadContext/cells_N`: `LoadContext` ring walk at center `(0,0)` with **N** cells in DB.
- `BenchmarkAPI_LoadContextAt/cells_N`: same with **[`record.ValidAt`](../../internal/record/validity.go)** at a fixed `asOf`.
- `BenchmarkAPI_WalkRing/cells_N`: one **`WalkRing`** at ring **2** (12 positions); reports **`ring_cells`**.
- `BenchmarkAPI_WalkRingAt/cells_N`: **`WalkRingAt`** with validity filter; reports **`ring_cells`**.

Each read/scan sub-benchmark reports an extra **`cells`** metric (preload row count). Ring benchmarks also report **`ring_cells`**.

## Interpretation

- **Lattice** paths are sub-microsecond to ~160 ns/op with **zero heap allocs** on the measured loops — consistent with stdlib-only hot paths ([`HEXXLA_DB.md`](./HEXXLA_DB.md) asks for benchmark validation of Morton locality claims; these measure **packing**, not end-to-end I/O).
- **B+ tree** figures include **durability** (WAL append + fsync path in `Put`); `Get`/`AscendRange` reflect disk-backed reads through pooled page buffers (`Engine.readPagePooled` / `release` pattern in the btree).
- Re-run after changes to [`internal/engine/btree.go`](../../internal/engine/btree.go), [`internal/lattice/packed.go`](../../internal/lattice/packed.go), record codecs, or [`primitives.go`](../../primitives.go) / [`cell_secondary.go`](../../cell_secondary.go) when tuning performance.
- **API** benchmarks include **WAL + fsync** behavior on `PutCell` paths; compare on the same filesystem when tracking regressions.

## Benchmark coverage gaps (encrypted, MVCC, contention)

Use this when evaluating scale confidence beyond default CI ([`ADOPTION.md`](./ADOPTION.md)).

| Scenario | Covered in [`api_bench_test.go`](../../api_bench_test.go) | Gap / note |
|----------|-------------------------------------------------------------|------------|
| MVCC write path | `BenchmarkAPI_PutCell_MVCC` | Single-threaded; DB remains single-writer—multi-writer load belongs in **app** tests. |
| Encrypted read after preload | `BenchmarkAPI_GetCell_Encrypted/cells_*` | — |
| Concurrent **View** + **Update** | `BenchmarkAPI_ViewUpdateContention` | Writer is **`Tx.Put`** on a raw key (not `PutCell`); models mutex + WAL contention, not MVCC churn. |
| MVCC + encryption same DB | `BenchmarkAPI_GetCell_MVCC_Encrypted/cells_*` | Write-heavy combined scenario still covered separately (`PutCell_MVCC` vs encrypt-only reads). |

**Archive:** after `make bench-stress`, append a dated row to your internal sheet with `go version`, CPU, disk, `TMPDIR`, and key benchmark lines (`BenchmarkAPI_*`). Refresh when changing [`internal/engine`](../../internal/engine), [`primitives.go`](../../primitives.go), or encryption/MVCC paths.

## Release / regression capture

Before claiming a performance or readiness update, capture and archive:

- `make ci`
- `make integration`
- `make stress` (or a documented reduced profile)
- `make bench-stress`

Include environment notes (`go version`, CPU, storage type, `TMPDIR`) so regression comparisons remain reproducible.
