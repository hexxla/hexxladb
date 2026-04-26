# Tier 1 Feature Plan

**Status:** Planning
**Target:** v0.2.0
**Items:** Health Check API · Content Search · Temporal Range Queries

---

## Embedding / Content Search Decision

### Current state of embeddings in HexxlaDB

`CellRecord` has **no embedding field**. There is **no vector storage** in v1 by design (HEXXLA_DB.md § v1 Scope).

The spec reserves an `embed/<partition>/<vector_ref>` keyspace for a **future optional** ANN index, but this is explicitly labelled "not required in v1". Seed selection (including via embeddings) is treated as an **orchestration concern outside the DB** — the DB only needs a starting `Coord`.

### Content Search approach (without embeddings)

For now: **brute-force substring/prefix scan** over `RawContent`, `Tags`, and `Provenance.SourceID` for visible cells. This is honest about what the data model supports today. The API should be designed so it can later be accelerated by:

1. A `content/` btree secondary index (trigrams or prefix tokens) — Near-term
2. The `embed/<partition>/<vector_ref>` keyspace + ANN — Future milestone

### When embeddings arrive

- `CellRecord` will need an `Embedding []float32` field (wire format v3 bump or sidecar key)
- A new `embed/<partition>/<vector_ref>/<packed_coord>` secondary key family is already reserved
- **Content Search API should be forward-compatible: `SearchCellsConfig` with a `Query string` today, extensible with `Embedding []float32` later without breaking callers**

### Multi-coord results and multi-context-pack loading

`SearchCells` returns **`[]CellSearchResult`** — each result carries the matching `CellView` **and** its `Coord`. Callers can:

1. Use the top-N `Coord` values directly as seeds for `LoadContextPack` calls
2. Use the new `LoadMultiContextPack` helper (see Feature 2b below) to assemble a **merged** `ContextPack` across multiple seed coords in one call, respecting a shared token budget

This makes content search a first-class **seed selection** mechanism for the hybrid retrieval path described in HEXXLA_DB.md.

---

## Feature 1: Database Health Check API

**File:** `health.go`
**Public types:** `HealthReport`, `HealthCheckConfig`
**Public func:** `(db *DB) HealthCheck(ctx, cfg) (HealthReport, error)`

### What it checks

| Check                    | How                                                       |
| ------------------------ | --------------------------------------------------------- |
| Cell count (visible)     | Ring walk from origin or AscendRange on `cell/` prefix    |
| Orphaned seam detection  | For each seam, verify CellA and CellB exist as live cells |
| Tag index consistency    | For each `tag/<t>/<coord>` entry, verify cell exists      |
| Source index consistency | For each `source/<id>/<coord>` entry, verify cell exists  |
| MVCC stats snapshot      | Call `StatsMVCC()`                                        |
| Seam resolution summary  | Resolved vs unresolved counts                             |

### Report shape (sketch)

```go
type HealthReport struct {
    CellCount         int
    SeamCount         int
    OrphanedSeams     []string // ULID IDs of seams with missing endpoints
    TagIndexErrors    int
    SourceIndexErrors int
    MVCCStats         MVCCStats
    SeamsResolved     int
    SeamsUnresolved   int
    Warnings          []string
}
```

### Config shape

```go
type HealthCheckConfig struct {
    CheckTagIndex    bool // default true
    CheckSourceIndex bool // default true
    CheckOrphans     bool // default true
    MaxErrors        int  // stop after N errors (0 = unlimited)
}
```

### Checklist

- [ ] Write `health.go` with `HealthReport`, `HealthCheckConfig`, `DB.HealthCheck`
- [ ] Unit test: empty DB has zero errors
- [ ] Unit test: seam with deleted cell → orphan detected
- [ ] Unit test: tag index entry with no cell → error counted
- [ ] Update `API_REFERENCE.md`
- [ ] Update `CHANGELOG.md` + `TODOS.md`

---

## Feature 2: Content Search + Multi-Context Loading

**File:** `search.go`
**Public types:** `CellSearchConfig`, `CellSearchResult`
**Public func:** `(tx *Tx) SearchCells(ctx, cfg) ([]CellSearchResult, error)`

### 2a: SearchCells

#### Design rationale

- Brute-force for v1: scan all visible cells via `WalkRings` from origin outward, apply filters
- Searches: `RawContent` (substring, case-insensitive), `Tags` (exact + prefix), `Provenance.SourceID` (exact), `Provenance.Confidence` (range filter)
- **Forward-compatible:** `CellSearchConfig` has `Query string` today; `Embedding []float32` can be added later as an optional field without breaking callers
- Runs inside a `db.View` transaction for consistent snapshot
- Returns `[]CellSearchResult` — each entry has the full `CellView` **and** `Coord` for direct use as a seed
- Sorted by score descending; ties broken by `Confidence` descending

#### API sketch

```go
type CellSearchConfig struct {
    // Primary query — matched against RawContent (substring), Tags (exact+prefix), SourceID (exact).
    // Future: Embedding []float32 will be added here for ANN seed selection without breaking callers.
    Query string

    // Tag filters: RequireTags = AND (cell must have ALL); AnyTags = OR (cell must have at least one).
    RequireTags []string
    AnyTags     []string

    // Confidence range filter.
    MinConfidence float64
    MaxConfidence float64 // 0 = no upper bound

    // Source filter: only cells from this SourceID.
    SourceID string

    // Spatial restriction: only cells within Radius rings of Center.
    // Zero Center + zero Radius = no spatial restriction (scan all).
    Center Coord
    Radius int

    // MaxResults caps output (default 20, 0 = use default).
    MaxResults int

    // MaxScanRadius controls how far from origin to walk (default 32).
    // Increase for sparse or geographically wide databases.
    MaxScanRadius int
}

type CellSearchResult struct {
    Coord CellView  // full assembled view
    Score float64   // composite relevance score (see scoring below)
}
```

#### Match scoring

| Condition                                      | Score contribution   |
| ---------------------------------------------- | -------------------- |
| Query matches a tag exactly (case-insensitive) | +1.0                 |
| Query is a prefix of a tag                     | +0.8                 |
| Query found verbatim in `RawContent`           | +0.6                 |
| Query found case-insensitively in `RawContent` | +0.5                 |
| Each additional tag match (multi-word query)   | +0.3 per extra match |
| Query matches `SourceID` exactly               | +0.3                 |
| `Confidence` bonus                             | `+0.1 × Confidence`  |

Final score = sum of contributions. Sorted descending; ties broken by `Confidence`.

### 2b: LoadMultiContextPack — multi-seed context assembly

Allows callers to pass **multiple seed coords** (e.g. from top-N search results) and get a **single merged `ContextPack`** within a shared token budget. Each seed expands its own ring neighbourhood; results are merged and re-ranked by confidence before budget eviction.

```go
type MultiContextConfig struct {
    Centers         []Coord                 // seed coordinates (e.g. from SearchCells)
    MaxR            int                     // ring radius per seed
    MaxTokens       int                     // shared token budget across all seeds
    Budgeter         TokenBudgeter
    AssemblyConfig  LoadContextBudgetConfig // FilterSuperseded, Explain, etc.
    DeduplicateCoords bool                  // skip coords already included by an earlier seed
}

// LoadMultiContextPack assembles a merged ContextPack from multiple seed coordinates.
// Seeds are expanded in order; DeduplicateCoords prevents double-counting shared neighbours.
func (tx *Tx) LoadMultiContextPack(ctx context.Context, cfg MultiContextConfig) (ContextPack, error)
```

#### Checklist

- [ ] Write `search.go` with `CellSearchConfig`, `CellSearchResult`, `Tx.SearchCells`
- [ ] Write `LoadMultiContextPack` in `views.go` (or `multi_context.go`)
- [ ] Unit test: empty DB → empty search results
- [ ] Unit test: tag exact match scores 1.0, content substring 0.5
- [ ] Unit test: `RequireTags` AND filter, `AnyTags` OR filter
- [ ] Unit test: `MinConfidence` / `MaxConfidence` range filter
- [ ] Unit test: `Radius` spatial restriction excludes far cells
- [ ] Unit test: `MaxResults` caps output
- [ ] Unit test: `LoadMultiContextPack` with 2 seeds, shared budget respected
- [ ] Unit test: `DeduplicateCoords` prevents double-counted neighbours
- [ ] Benchmark: 1000 cells, `SearchCells` query "foo" — establish baseline
- [ ] Update `API_REFERENCE.md`
- [ ] Update `CHANGELOG.md` + `TODOS.md`

---

## Feature 3: Temporal Range Queries

**File:** `temporal_range.go`
**Public types:** `TemporalRangeConfig`, `TemporalRangeResult`
**Public func:** `(db *DB) CellsInTimeRange(ctx, from, to time.Time, cfg) ([]TemporalRangeResult, error)`

### Existing infrastructure

- `time/<valid_bucket>/<packed_coord>` secondary index already exists
- `ReadChangelogFiltered` with `ChangelogFilter` exists for changefeed queries
- `ViewAtTime` provides MVCC snapshot at a wall-clock time

### What this adds

A **"what changed between time A and time B"** query that:

1. Uses the `time/` secondary index to find cells with validity overlapping the range
2. Cross-references the changelog (if enabled) for `PutCell`/`ResolveSeam` ops in the window
3. Returns a diff-like summary: cells added, cells that became invalid, seams created/resolved

### API sketch

```go
type TemporalRangeConfig struct {
    IncludeCells bool // default true
    IncludeSeams bool // default true
    MaxResults   int  // default 100
    Center       *Coord // optional: restrict to ring radius
    Radius       int
}

type TemporalRangeResult struct {
    AddedCells    []CellView
    ExpiredCells  []CellView
    NewSeams      []SeamRef
    ResolvedSeams []SeamRef
    From          time.Time
    To            time.Time
}
```

### Checklist

- [ ] Write `temporal_range.go` with `TemporalRangeConfig`, `TemporalRangeResult`, `DB.CellsInTimeRange`
- [ ] Unit test: cells with validity in range are returned
- [ ] Unit test: cells with validity outside range excluded
- [ ] Unit test: `Center` + `Radius` spatial restriction works
- [ ] Unit test: seams detected in window appear in `NewSeams`
- [ ] Update `API_REFERENCE.md`
- [ ] Update `CHANGELOG.md` + `TODOS.md`

---

## Implementation Order

```
1. health.go        — no dependencies on 2 or 3; standalone
2. search.go        — no dependencies; can start immediately after 1
3. temporal_range.go — leans on existing time/ index and changelog; start after 1+2
```

## File Placement (hexagonal arch compliance)

All three are **pure query operations over `*Tx` or `*DB`** — they belong at the **root package** alongside `views.go`, `primitives.go`, `tag_analytics.go`. No adapter or internal changes required.

## Version target

These are additive, non-breaking API additions → **v0.2.0** (minor bump per `VERSIONING.md`).
