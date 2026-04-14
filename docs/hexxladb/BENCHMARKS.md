# Microbenchmarks (reference)

This document records **how** to run engine hot-path benchmarks and a **sample** result set for regression comparison. **Absolute numbers are machine-dependent** — always compare on the same hardware and Go version.

## How to run

```bash
make bench
# or:
go test -count=1 -bench=. -benchmem ./internal/lattice ./internal/engine ./internal/record
```

Fuzz smoke (not timed like benchmarks):

```bash
make fuzz
```

See also [CONTRIBUTING.md](../../CONTRIBUTING.md).

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

| Benchmark        | ns/op   | B/op    | allocs/op |
| ---------------- | ------- | ------- | --------- |
| BTreeGet         | 104,912 | 199,200 | 75        |
| BTreePutUpdate   | 315,195 | 535,290 | 67        |
| BTreeAscendRange | 364,461 | 663,921 | 281       |

### Record ([`internal/record`](../../internal/record))

| Benchmark        | ns/op | B/op | allocs/op |
| ---------------- | ----- | ---- | --------- |
| EncodeDecodeCell | 621   | 152  | 7         |

## Interpretation

- **Lattice** paths are sub-microsecond to ~160 ns/op with **zero heap allocs** on the measured loops — consistent with stdlib-only hot paths ([`HEXXLA_DB.md`](./HEXXLA_DB.md) asks for benchmark validation of Morton locality claims; these measure **packing**, not end-to-end I/O).
- **B+ tree** figures include **durability** (WAL append + fsync path in `Put`); `Get`/`AscendRange` still reflect real disk-backed page reads through the engine.
- Re-run after changes to [`internal/engine/btree.go`](../../internal/engine/btree.go), [`internal/lattice/packed.go`](../../internal/lattice/packed.go), or record codecs when tuning performance.
