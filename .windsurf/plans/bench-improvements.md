---
description: Benchmark-driven performance improvements for HexxlaDB
branch: feat/bench-improvements
created: 2026-04-26
status: complete
---

# Benchmark Improvement Plan

Baseline measurements from `make bench-api` on Intel Core i9-14900HX, Go 1.26, Linux (CachyOS).
All work must be validated by re-running `make bench-api` before and after each change.
No change ships without a regression test or benchmark delta that proves the improvement.

---

## Area 1 — `FindSeams` base cost: 2.3 ms with zero seams

### Measured signal

```
BenchmarkAPI_FindSeams/seams_0   1604   2,275,497 ns/op   4,864,273 B/op   302 allocs/op
BenchmarkAPI_FindSeams/seams_10  775    4,789,759 ns/op   5,311,106 B/op  4280 allocs/op
```

The zero-seam case costs 2.3 ms — more than a full `LoadContext` (793 µs). The 10-seam case
is only 2× more expensive, not proportionally more, so the base cost dominates.

### Root cause (located)

`primitives.go:380` — `findSeams` always calls `lattice.WalkRings(nil, center, radius)` which
materialises the full list of coords before touching the index. For radius=3 that is 37 coords;
for radius=5 it is 91 coords. Two `AscendRange` calls are then issued per coord regardless of
whether any seam index entries exist at that coord.

There is no guard that skips the loop early when the seam index prefix is empty.

### Ideas for improvement

#### Idea A — Pre-flight presence check (low risk)

Add a single `AscendRange` over the entire `seam-by-cells/` prefix to check whether _any_
seams exist in the DB before entering the per-coord loop. Cost is one range scan with a
stop-after-first callback. If the DB has no seams at all, return early in O(1).

```go
// draft sketch only — not final
var hasAny bool
_ = tx.db.btree.AscendRange(index.SeamByCellsPrefix, index.SeamByCellsPrefixEnd, func(_, _ []byte) bool {
    hasAny = true
    return false // stop immediately
})
if !hasAny {
    return nil, nil
}
```

**Cost**: one B+ tree range scan (O(log n) to first leaf). Zero false negatives.
**Risk**: low — purely additive early-exit path.

#### Idea B — Lazy coord iteration (medium risk)

Replace the `WalkRings` materialisation with an inline ring-by-ring iteration that issues
`AscendRange` calls without building the full coord slice upfront. The allocation of 37–91
`lattice.Coord` structs (each 8 bytes) is small, but the per-coord allocation pattern
contributes to the 302 allocs/op baseline. Inline iteration removes the intermediate slice.

```go
// Replace:
coords := lattice.WalkRings(nil, center, radius)
for _, c := range coords { ... }

// With:
for ring := range radius + 1 {
    for _, c := range lattice.Ring(center, ring) { ... }
}
```

`lattice.Ring` already caps its allocation at `6*k` (ring.go:43), so memory is equivalent
but avoids the aggregating `WalkRings` slice.

#### Idea C — Seam presence counter in header (speculative, high risk)

Store a seam count in the DB header (or a `__meta/seam-count` key) and skip all per-coord
scanning if count == 0. Would make the zero-seam case truly O(1).
**Risk**: requires atomic increment/decrement on every `PutSeam`/`DeleteSeam`, adds write
overhead, and counter can diverge on crash without WAL replay. Defer unless A+B are
insufficient.

### Recommended order

1. Implement Idea B (lazy iteration, no extra I/O, removes slice alloc)
2. Benchmark — if zero-seam cost drops below 500 µs, stop
3. If still high, add Idea A pre-flight check on top

---

## Area 2 — `LoadContextPack` allocation pressure at large radii

### Measured signal

```
BenchmarkAPI_LoadContextPack/r5/cells_512   2122   3,342,487 ns/op   1,636,523 B/op    8,599 allocs/op
BenchmarkAPI_LoadContextPack/r5/cells_2000   772   4,622,585 ns/op   2,281,340 B/op   11,756 allocs/op
```

At r=5 / 2000 cells: 2.28 MB and 11,756 allocations per call. This is the LLM hot path —
called on every prompt. GC pauses from this pattern will be visible in production at scale.

### Root cause (located)

`internal/views/budget.go:109` — `collectCandidates`:

```go
var items []scoredCandidate   // zero capacity — grows via append
```

For r=5 the ring area is `3*5²+3*5+1 = 91` cells. Starting from zero means ~7 doublings
before capacity stabilises (1→2→4→8→16→32→64→128). Each doubling copies the previous slice.

Additionally, `seen map[lattice.Coord]struct{}` starts empty and must resize.

Secondary: `internal/views/budget.go:238`:

```go
items = append(items[:drop], items[drop+1:]...)  // shifts entire slice on each eviction
```

And the total recalc loop (lines 241–243) re-sums all items after each drop — O(n) per drop
when it could be O(1) by subtracting the dropped item's cost.

### Ideas for improvement

#### Idea A — Pre-size `items` and `seen` with ring area (low risk)

```go
// Ring area formula: 3r²+3r+1 (exact cell count for radius r)
ringArea := 3*maxR*maxR + 3*maxR + 1
if capCells < ringArea {
    ringArea = capCells
}
items := make([]scoredCandidate, 0, ringArea)
seen  := make(map[lattice.Coord]struct{}, ringArea)
```

**Cost**: two extra multiplies per call. Eliminates all intermediate slice doublings.
**Risk**: very low — pure capacity hint, no behavioural change.

#### Idea B — O(1) eviction total tracking (low risk)

Replace the O(n) `total` recalculation after each drop with a running subtract:

```go
// Before eviction loop, compute total once (already done at line 203-205).
// Inside the loop, after drop:
total -= CellViewTokens(budgeter, items[drop].view, cfg.IncludeFacetText)
items = append(items[:drop], items[drop+1:]...)
evicted++
// Remove the inner recalc loop entirely.
```

The slice shift (`append(items[:drop], items[drop+1:]...)`) is still O(n) per eviction but
only triggers when over budget. If eviction is rare this is acceptable. If eviction is
frequent (tight budgets), consider a heap keyed on (ring, confidence) — defer until
benchmarks show it's needed.

#### Idea C — `sync.Pool` for decode buffers (medium risk)

`AssembleCellView` → `tx.GetCell` → `record.DecodeCell` allocates a new buffer per cell.
A `sync.Pool[[]byte]` in `internal/record` for the decode scratch buffer would amortise
these allocations across calls. Requires careful pool size management to avoid retaining
large buffers. **Benchmark first to confirm decode is the dominant alloc source** (use
`go test -bench=BenchmarkAPI_LoadContextPack -memprofile=mem.out` + `go tool pprof`).

### Recommended order

1. Idea A (pre-size) — 5 lines, zero risk, immediate win
2. Idea B (O(1) total) — 3 lines, remove inner loop
3. Profile before considering Idea C

---

## Area 3 — `QueryCells` source/combined scan: O(n) unbounded

### Measured signal

```
BenchmarkAPI_QueryCells/source_only/cells_512    324   10,331,594 ns/op   3,912,000 B/op   50,851 allocs/op
BenchmarkAPI_QueryCells/source_only/cells_2000   100   53,730,561 ns/op  16,285,738 B/op  210,350 allocs/op
BenchmarkAPI_QueryCells/combined/cells_512       242   14,244,274 ns/op   3,650,919 B/op   50,845 allocs/op
BenchmarkAPI_QueryCells/combined/cells_2000       84   52,625,577 ns/op  14,460,109 B/op  210,339 allocs/op
```

5.2× latency increase for 4× more cells — strictly linear in matching rows. The 50k+
allocs/op confirms a full index walk with no cursor limit.

### Root cause (located)

`QueryCells` source-only path walks the entire `source/<id>/` B+ tree prefix via
`AscendCellsBySource`, decoding and allocating a `CellRecord` per entry with no row
budget separate from `CellQuery.MaxResults`. When `MaxResults` is unset (0 = unlimited),
the scan never stops early.

### Ideas for improvement

#### Idea A — `MaxScanRows` field on `CellQuery` (low risk, additive API)

Add an optional field to `CellQuery`:

```go
// MaxScanRows caps the number of index rows examined before returning.
// 0 means unlimited (current behaviour). Protects against unbounded
// full-index walks when callers do not set MaxResults.
MaxScanRows int
```

Planner passes `MaxScanRows` into `AscendCellsBySource` as a counter. If the counter
hits the cap, return results collected so far with a `truncated=true` flag in
`CellQueryResult` or a new `QueryStats` field.
**Risk**: additive — zero behavioural change for existing callers (MaxScanRows=0).

#### Idea B — Default scan cap in the planner (medium risk)

When `MaxResults > 0` and `MaxScanRows == 0`, auto-set `MaxScanRows = MaxResults * 10`
as a safety multiplier. Prevents runaway scans without requiring callers to opt in.
**Risk**: could silently truncate results for callers expecting all matches. Must document
clearly and surface via stats. Consider opt-out flag instead.

#### Idea C — Source index with row count metadata (high risk, speculative)

Store a per-source cell count under `__meta/source-count/<sourceID>` incremented on
`PutCell`. Allows the planner to skip the index entirely if the source has 0 cells, or
estimate scan cost before committing. Adds write overhead on every `PutCell`.
**Defer**: only worth it if Idea A proves insufficient and source queries are a primary
hot path in production.

### Recommended order

1. Idea A (MaxScanRows field) — additive, safe, immediate protection
2. Idea B (auto-cap) — only after confirming no caller relies on unlimited scan
3. Idea C — defer to post-profiling evidence

---

## Area 4 — `BenchmarkAPI_BatchPutCells` missing

### Measured signal

```
BenchmarkAPI_PutCell-32   442   8,344,367 ns/op   1,420,546 B/op   352 allocs/op
```

Single-cell `PutCell` = 8.3 ms (one fsync per transaction). `BatchPutCells` exists and
amortises fsync across a configurable batch size, but has no benchmark — so any regression
in batch throughput is invisible.

### What to build

`BenchmarkAPI_BatchPutCells` in `api_bench_test.go` — sub-benchmarks for batch sizes
10, 100, 500 cells. Should report a custom `cells/op` metric alongside ns/op so throughput
is readable directly:

```go
func BenchmarkAPI_BatchPutCells(b *testing.B) {
    for _, batchSize := range []int{10, 100, 500} {
        b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
            // build batchSize CellRecords, call BatchPutCells, reset timer
            b.ReportMetric(float64(batchSize), "cells/op")
        })
    }
}
```

Expected result: batch/500 should be roughly `8.3ms * 500 / (500/batchAmortisation)` —
probably 20–50× faster per cell than single `PutCell`.

---

## Indirect bottlenecks with knock-on effects

These are not directly in the four concern areas but are shared primitives called by all hot paths.
Fixing them amplifies the gains from Areas 1–4.

### B1 — `mortonPack63` / `mortonUnpack63`: 21-iteration bit loop (53 ns/op)

**Measured**: `BenchmarkPack = 53 ns/op`, `BenchmarkUnpack = 53 ns/op` (zero allocs — good).

**Where it's called across all hot paths:**

- `findSeams`: `Pack(c)` per coord — 37× at r=3, 91× at r=5 = 37×53 ns = ~2 µs just in Pack
- `collectSeamFind`: `Unpack(CellA)` + `Unpack(CellB)` per seam found — 100 seams = 200×53 ns = ~10.6 µs
- `collectCandidates` → `AssembleCellView` → `Pack(coord)` per candidate cell
- `QueryCells` spatial path: `Pack` per result coord

**Root cause**: `mortonPack63` runs a 21-iteration scalar bit loop. Standard portable
implementation but well-known to be replaceable with lookup tables.

**Proposed fix — lookup table interleaving:**
Replace the 21-iteration loop with two 256-entry tables (one for odd bits, one for even),
processing 8 bits at a time in 3 passes. Estimated result: **5–10 ns/op** (5–10× speedup).
Wire format is unchanged — `PackedCoord` bytes are identical.

```go
// sketch — expand each byte to interleaved bits via table lookup
var mortonTable256 [256]uint64 // precomputed: bit i of input → bit 3i of output
func mortonExpand8(b uint8) uint64 { return mortonTable256[b] }
func mortonPack63(qp, rp, sp uint64) uint64 {
    var m uint64
    for shift := range 3 { // 3 passes of 21 bits = 63 bits total, 8+8+5 per axis
        m |= mortonExpand8(uint8(qp>>uint(shift*8))) << uint(shift*24)
        m |= mortonExpand8(uint8(rp>>uint(shift*8))) << uint(shift*24+1)
        m |= mortonExpand8(uint8(sp>>uint(shift*8))) << uint(shift*24+2)
    }
    return m
}
```

**Risk**: low — internal to `internal/lattice`, no API change, existing round-trip tests
validate correctness. Add a fuzz test comparing old vs new output before removing old impl.

---

### B2 — `Ring()` allocates a new slice per call

**Where it's called**: every `collectCandidates` ring iteration (r=5 → 5 allocations),
every `findSeams` lazy iteration after Area 1 Idea B is applied.

**Root cause**: `make([]Coord, 0, 6*k)` on every `Ring(center, k)` call. When the caller
loops over rings 0..maxR, this produces `maxR` separate heap allocations.

**Proposed fix — `RingInto(dst []Coord, center Coord, k int) []Coord`:**
Add a variant that appends into a caller-supplied slice. Callers pre-allocate once:

```go
buf := make([]Coord, 0, 3*maxR*maxR+3*maxR+1) // exact ring area
for k := range maxR+1 {
    buf = RingInto(buf[:0], center, k) // reuse buffer, reset length
    for _, c := range buf { ... }
}
```

The existing `Ring` function is kept for backward compatibility; `RingInto` is additive.
**Risk**: very low — purely additive, existing callers unchanged.

---

### B3 — `AssembleCellView` double-copies `Tags` slice

**Location**: `internal/views/views.go:202`:

```go
Tags: append([]string(nil), rec.Tags...),
```

This copies the tags slice returned by `DecodeCell`. Since `DecodeCell` already allocates
`r.Tags = make([]string, 0, nt)` (cell.go:108), `AssembleCellView` creates a second copy.
For a 91-candidate `LoadContextPack/r5` call, this doubles the tags allocation cost.

**Proposed fix**: If `CellView.Tags` is treated as read-only after assembly (no callers
mutate it), remove the copy and share the underlying array from `DecodeCell`.
**Risk**: low if `CellView` is documented as immutable. Must audit all callers of
`CellView.Tags` to confirm none append/assign to the slice.

---

### B4 — `BTreeAscendRange` called 74× in `FindSeams` at r=3 (even for empty ranges)

**Measured**: `BenchmarkBTreeAscendRange = 44,693 ns/op`.

`findSeams` at r=3 issues 2 `AscendRange` calls per coord × 37 coords = **74 calls**.
For empty ranges (no seams at that coord), the call still traverses the B+ tree to the
leaf level. Even if each empty-range scan costs 10 µs (faster than the 44 µs full scan),
74 × 10 µs = 740 µs — a large fraction of the 2.3 ms base cost.

**This directly validates Area 1, Idea A** (pre-flight check): a single AscendRange
confirming the seam index is empty saves all 74 subsequent calls. This is the highest
ROI single-line change in the entire plan.

---

## Work order

| #   | Area                                                     | Effort        | Risk     | Expected gain              |
| --- | -------------------------------------------------------- | ------------- | -------- | -------------------------- |
| 1   | `collectCandidates` pre-size (Area 2, Idea A)            | 5 lines       | very low | eliminates slice doublings |
| 2   | O(1) eviction total (Area 2, Idea B)                     | 3 lines       | very low | removes O(n) inner loop    |
| 3   | `FindSeams` lazy iteration (Area 1, Idea B)              | 10 lines      | low      | removes WalkRings alloc    |
| 4   | Add `BenchmarkAPI_BatchPutCells` (Area 4)                | ~40 lines     | none     | coverage only              |
| 5   | `QueryCells` MaxScanRows (Area 3, Idea A)                | ~30 lines     | low      | bounds worst-case latency  |
| 6   | `FindSeams` pre-flight check (Area 1, Idea A)            | ~10 lines     | low      | O(1) for seam-empty DBs    |
| 7   | Profile `LoadContextPack` decode allocs (Area 2, Idea C) | investigative | —        | determine if pool helps    |

Run `make bench-api` baseline → implement #1 → run again → commit delta → repeat.
