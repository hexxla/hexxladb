# HexxlaDB Streaming Architecture

## Background

HexxlaDB stores conversation turns as cells on a hex lattice backed by a B+ tree
(the engine package). The B+ tree already supports cursor-based streaming via
`AscendRange(from, to, fn)` — it never loads the whole tree. The problem is the
_layers above_ it have historically materialised results into slices before
returning them to callers.

For long-running agents, the cell count grows linearly with turns. A DB with
100k turns has `MaxRing ≈ 183`. Materialising 31k packed coords before a single
B+ tree lookup is O(r²) unnecessary allocation on every context load.

Reference: Badger DB (`stream.go`) uses the same discipline — `Iterator.Seek`/
`Next` cursor reads one key at a time, pushes into a bounded channel, and the
consumer breaks as soon as it has what it needs. We apply the same pattern.

---

## Streaming Status by Layer

### Layer 1 — B+ tree cursor (engine)

**Status: ✅ Always streamed.**

`AscendRange(from, to, fn func(k,v []byte) bool)` is a pure cursor. `fn`
returning `false` stops iteration immediately. No slice is ever allocated.

### Layer 2 — Secondary index scans (primitives.go, cell_secondary.go)

**Status: ✅ Already streamed.**

`AscendCellsByTag`, `AscendCellsBySource`, `AscendCellsInTimeBucket`,
`AscendEdgesFrom` — all use `AscendRange` with a callback. They are streaming
by construction.

### Layer 3 — Ring coordinate generation (internal/lattice)

**Status: ✅ Streamed as of this work.**

| Function                                    | Before                   | After                                         |
| ------------------------------------------- | ------------------------ | --------------------------------------------- |
| `WalkRingsPacked(center, r)`                | Allocates `O(3r²)` slice | Kept for compatibility                        |
| `WalkRingsPackedSeq(center, r)`             | —                        | NEW: lazy `iter.Seq[CoordPacked]`, zero alloc |
| `SpiralRangePackedSeq(center, minR, maxR)`  | —                        | NEW: lazy `iter.Seq[CoordPacked]`, zero alloc |
| `RingSeq`, `WalkRingsSeq`, `SpiralRangeSeq` | —                        | NEW: lazy `iter.Seq[Coord]`, zero alloc       |

For `r=100` (31,401 cells) with a 16-cell budget, zero coords beyond the 16th
are ever computed.

### Layer 4 — Context loading (internal/views)

**Status: ✅ Streamed as of this work.**

| Path                                   | Before                                                | After                                                                         |
| -------------------------------------- | ----------------------------------------------------- | ----------------------------------------------------------------------------- |
| Standard ring walk (`MaxRing < 10`)    | Already streamed via `RingInto` + per-cell callback   | Unchanged                                                                     |
| LOD coord collection (`MaxRing >= 10`) | `O(3r²)` packed slice, then iterate                   | Lazy `iter.Seq`, breaks on `maxCoords`                                        |
| LOD + edge-BFS cell assembly           | Fan-out N goroutines, full-sort, trim                 | Sequential loop, stops at token budget                                        |
| Multi-seed                             | Concurrent per-seed (already capped), cross-seed sort | Per-seed still concurrent; merge sort kept (required for cross-seed fairness) |

### Layer 5 — Query engine (query_exec.go)

**Status: ✅ Streamed as of this work.**

| Sub-path                    | Before                                           | After                                                            |
| --------------------------- | ------------------------------------------------ | ---------------------------------------------------------------- |
| `scanByRadius`              | `WalkRingsPacked` → `O(3r²)` slice               | `WalkRingsPackedSeq` → lazy, breaks on `maxScanRows`             |
| `scanByTag`, `scanBySource` | Collect all rows into `[]CellRecord` slice       | **Fused**: filter+score inlined into `AscendCellsByTag` callback |
| `scanByTimeRange`           | Collect all rows into `[]CellRecord` slice       | **Fused**: filter+score inlined into `AscendRange` callback      |
| `scanByEmbedding`           | ANN returns bounded `k` results — already capped | Unchanged (inherently bounded by `MaxResults×2`)                 |
| Filter + score pass         | Separate loop over `candidates` slice            | Eliminated — fused into scan callback                            |
| Sort                        | Required (score/confidence/recency/coord order)  | Kept — no streaming equivalent for a sorted result               |

**The sort cannot be streamed away.** A globally sorted result requires seeing all
candidates. This is the same constraint Badger acknowledges: `Stream` does not
guarantee sorted output; `Iterator` is used when order is required. The
improvement here is that the candidates slice feeding the sort is no longer a
separate materialisation — filter and score are fused into the scan, so only
_passing_ records enter the sort input.

### Layer 6 — Edge BFS (pathfind_api.go, internal/views)

**Status: ✅ Streamed as of this work.**

`loadContextByEdges` previously collected all BFS coords into a slice, then
passed them to assembly. Now the BFS result feeds directly into the streaming
assembly loop without an intermediate collection step.

---

## What Cannot Be Fully Streamed

### Sorted query results

`QueryCells` and `SearchCells` must sort results by score/confidence/recency
before returning. This requires all candidates to be scored before any can be
returned in order. The `[]CellQueryResult` slice is irreducible.

**Mitigation:** `MaxResults` and `MaxScanRows` bound the sort input. The scan
itself is now fused (no extra allocation), so the sort input is as small as
possible before sorting occurs.

### Multi-seed merge sort

`loadContextMultiSeedConcurrent` merges per-seed packs and re-ranks by
confidence for fair cross-seed budget enforcement. The merge sort is O(n log n)
on the combined candidate set. A min-heap could reduce this but the total
candidate count is already bounded by `n_seeds × MaxCandidateCells`.

---

## Data Flow Diagram (after streaming work)

```text
B+ tree cursor (AscendRange)
    │  yields (k,v) one at a time
    ▼
Secondary index scan callback
    │  decodes record, checks ctx.Err()
    │  FUSED: apply predicate, score, append to results if passes
    ▼
results []CellQueryResult   ← only passing, scored records
    │
    ▼
sort (required)
    │
    ▼
results[:maxResults]        ← cap
    │
    ▼
caller
```

For context loading (LoadContext paths):

```text
WalkRingsSeq / WalkRingsPackedSeq   ← lazy iter.Seq, no slice alloc
    │  yields one Coord at a time
    ▼
B+ tree GetCell lookup (per coord)
    │  found? → AssembleCellView
    ▼
token budget check
    │  budget full? → break (no further coords computed)
    ▼
ContextPack returned to caller
```

---

## References

- `internal/lattice/ring.go` — `RingSeq`, `WalkRingsSeq`, `SpiralRangeSeq`
- `internal/lattice/walk.go` — `WalkRingsPackedSeq`, `SpiralRangePackedSeq`
- `internal/views/load_context.go` — `collectLODCoords`, `assembleCoordsIntoContextPack`
- `query_exec.go` — fused scan pipeline
- Badger `stream.go` / `iterator.go` — reference implementation
