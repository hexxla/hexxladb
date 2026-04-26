---
description: LEAN quick wins — remove orphaned internal/config package, add missing benchmarks
branch: feat/lean-quick-wins
status: complete
---

## Context

Two quick wins from the LEAN architecture audit. Both are low-risk, no API changes.

---

## Item 1 — Delete `internal/config` (orphaned package)

**Finding:** `internal/config/config.go` (29 lines) is imported by zero packages. `cmd/tui/main.go`
hardcodes `slog.LevelWarn` directly and never calls `config.Load()`. The package is dead weight.

**Action:** Delete `internal/config/config.go` and the directory. No callers to update.

**Risk:** None. Compile will confirm no references.

**Step:**

1. Delete `internal/config/config.go`
2. `go build ./...` — confirm clean
3. `make ci` — confirm clean

---

## Item 2 — Add missing benchmarks

**Finding:** `api_bench_test.go` covers PutCell, GetCell, WalkRing, LoadContext, AscendCellsBySource.
Missing baselines identified by LEAN audit:

| Benchmark                                                  | Location            | Why                                       |
| ---------------------------------------------------------- | ------------------- | ----------------------------------------- |
| `BenchmarkAPI_QueryCells` — varying predicate complexity   | `api_bench_test.go` | baseline before any query engine work     |
| `BenchmarkAPI_LoadContextPack` — varying radii             | `api_bench_test.go` | baseline before budgeting optimisation    |
| `BenchmarkAPI_MVCCVersionResolution` — high version counts | `api_bench_test.go` | baseline before `SelectVisible` O(n) work |

**Design notes:**

- `QueryCells`: sub-benchmarks for tag-only, source-only, spatial, and combined predicates; use `apiBenchPreloadSizes`
- `LoadContextPack`: sub-benchmarks for radii 1/3/5; reuse `benchAPIPreloadCells`
- `MVCCVersionResolution`: open MVCC-enabled DB, put same coord N times (N=10/50/100/500), benchmark `GetCell` which drives `SelectVisible`; sub-benchmarks per version count

**Risk:** Additive only. No production code changes.

**Steps:**

1. Add `BenchmarkAPI_QueryCells` to `api_bench_test.go`
2. Add `BenchmarkAPI_LoadContextPack` to `api_bench_test.go`
3. Add `BenchmarkAPI_MVCCVersionResolution` to `api_bench_test.go`
4. Run `go test -bench=BenchmarkAPI_QueryCells -benchmem -run=^$ .` to verify
5. Run `go test -bench=BenchmarkAPI_LoadContextPack -benchmem -run=^$ .` to verify
6. Run `go test -bench=BenchmarkAPI_MVCCVersionResolution -benchmem -run=^$ .` to verify
7. `make ci` — full green

---

## Completion

- [ ] Item 1: Delete `internal/config`
- [ ] Item 2a: `BenchmarkAPI_QueryCells`
- [ ] Item 2b: `BenchmarkAPI_LoadContextPack`
- [ ] Item 2c: `BenchmarkAPI_MVCCVersionResolution`
- [ ] `make ci` green
- [ ] Update TODOS, CHANGELOG, ROADMAP
- [ ] Commit + merge to main
