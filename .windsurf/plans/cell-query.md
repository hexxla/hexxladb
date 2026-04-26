# CellQuery — Composable Query Engine Plan

**Status:** In progress
**Target:** v0.2.0
**Branch:** feat/cell-query

---

## Goal

Replace the ad-hoc `CellSearchConfig` with a fully composable `CellQuery` predicate model.
All existing filter APIs (`SearchCells`, `AscendCellsByTag`, temporal) become implementations
of this single model. A future HQL string parser compiles to `CellQuery` without touching
the engine.

---

## Architecture

```
CellQuery (pure value type, no I/O)
    │
    ▼
query/planner.go  — pick cheapest index scan strategy
    │
    ▼
query/executor.go — run scan, apply predicate pipeline, score, sort, limit
    │
    ▼
Tx.QueryCells(ctx, CellQuery) ([]CellQueryResult, error)   ← root package, pure query on *Tx
```

All query logic lives in root package (`package hexxladb`) as pure `*Tx` operations —
consistent with `search.go`, `health.go`, `ring_density.go`. No new adapter packages needed.

---

## CellQuery struct

```go
type CellQuery struct {
    // --- Lexical ---
    Query       string   // substring match across content/tags/source
    RequireTags []string // AND — all must be present
    AnyTags     []string // OR — at least one
    ExcludeTags []string // NOT — none may be present

    // --- Provenance ---
    SourceID      string
    MinConfidence float64
    MaxConfidence float64 // 0 = no upper limit

    // --- Temporal (ValidFrom on cell) ---
    After  time.Time // cell ValidFrom > After
    Before time.Time // cell ValidFrom < Before

    // --- Spatial ---
    Center Coord
    Radius int // 0 = no spatial filter

    // --- Output ---
    MaxResults int         // 0 = unlimited
    SortBy     SortOrder   // ScoreDesc (default), ConfidenceDesc, RecencyDesc, CoordAsc
    Explain    bool        // populate CellQueryResult.Explanation

    // --- Future (zero = ignored, no breaking change) ---
    Embedding []float32
}

type SortOrder int
const (
    SortByScore      SortOrder = iota // composite score desc (default)
    SortByConfidence                  // Provenance.Confidence desc
    SortByRecency                     // ValidFrom desc
    SortByCoord                       // Coord lexicographic asc
)

type CellQueryResult struct {
    Cell        CellView
    Score       float64
    Explanation string // only when CellQuery.Explain = true
}
```

---

## Execution plan (planner)

Planner inspects `CellQuery` fields and picks a strategy:

| Condition | Primary index |
|---|---|
| `RequireTags` non-empty | `tag/` secondary index (AscendCellsByTag for rarest tag) |
| `SourceID` set | `source/` secondary index |
| `After`/`Before` set | `time/` week-bucket index (iterate buckets in range) |
| `Center`+`Radius` set | Ring walk (`WalkRings`) |
| Fallback | Full cell scan (`WalkRings` from origin, all cells) |

After primary index narrows the candidate set, remaining predicates applied as in-memory filters.
Scoring and sort unchanged from `SearchCells`.

---

## Temporal implementation

The `time/` index keys are `time/<week_bucket_int64be>/<packed_coord>`.
`WeekBucketFromValidity` converts `ValidFrom` nanos → bucket.

For `After`/`Before`:
1. Compute bucket range: `bucketFrom = After.UnixNano() / WeekNanos`, `bucketTo = Before.UnixNano() / WeekNanos`
2. `AscendRange` over `[TimeRangePrefix(bucketFrom), TimeRangePrefix(bucketTo+1))`
3. For each coord hit, `GetCell` to load and apply remaining predicates
4. Filter `ValidFrom` precisely (bucket is coarse, exact time check is fine)

---

## Files

| File | Purpose |
|---|---|
| `query.go` | `CellQuery`, `SortOrder`, `CellQueryResult` types |
| `query_exec.go` | `Tx.QueryCells` — planner + executor |
| `query_test.go` | Tests: empty, tag AND/OR/NOT, temporal, spatial, sort, explain |
| `search.go` | `SearchCells` becomes 1-line wrapper: `tx.QueryCells(ctx, CellQuery{Query: cfg.Query, ...})` |

---

## Backward compatibility

- `SearchCells` / `CellSearchConfig` / `CellSearchResult` kept as thin wrappers — no callers broken
- `LoadContextPackFrom` / `LoadMultiContextPack` unchanged
- `CellQueryResult` is a strict superset of `CellSearchResult`

---

## HQL string syntax (future, not this branch)

```
SELECT CELLS
WHERE tag IN ('preference', 'fact')
  AND confidence >= 0.8
  AND written_after '2026-04-01'
  AND content LIKE 'database'
  AND radius(0, 0, 3)
ORDER BY score DESC
LIMIT 10
```

Parses to `CellQuery`. Lives in a separate `hql` package or sub-package. No grammar work here.

---

## Checklist

- [ ] Write `query.go` — types
- [ ] Write `query_exec.go` — planner + executor
- [ ] Write `query_test.go` — all predicate combinations
- [ ] Refactor `search.go` → thin wrapper over `QueryCells`
- [ ] Update `API_REFERENCE.md`, `CHANGELOG.md`, `TODOS.md`, `ROADMAP.md`
- [ ] `make ci` clean
