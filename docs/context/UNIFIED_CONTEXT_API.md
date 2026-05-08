# Unified Context API — Design Plan

**Branch:** `feat/unified-context-api`
**Status:** Design / Pre-implementation
**Target version bump:** v0.5.0 (minor — breaking API surface reduction)

---

## Problem Statement

The current public API exposes too many overlapping context loading methods. Callers are
expected to pick the right algorithm (ring-walk vs LOD vs graph vs Voronoi vs FOV) based on
data characteristics they may not know. This produces:

- A confusing surface with 10 context-loading methods doing partially overlapping things
- An inconsistent return type: newer spatial tools return `[]CellRecord`; established tools return `ContextPack` (`CellView`-based)
- Redundant aliases: `LoadContextPack` = `LoadContextWithBudgeting`; `LoadContextPackFrom` dispatches to `LoadMultiContextPack`
- Algorithm selection burden on every caller/adapter (e.g. Mosaic MCP must choose LOD vs ring-walk itself)

**Goal:** The DB picks the best algorithm. Callers express intent (seeds, radius, budget, optional
graph traversal). One entry point. Consistent return type.

---

## Current API Inventory

### Context loading — return `ContextPack` (CellView-based)

| Method | Notes |
|---|---|
| `Tx.LoadContextWithBudgeting` | Core implementation |
| `Tx.LoadContextPack` | Exact alias of above |
| `Tx.LoadContextPackFrom` | Unified single/multi — dispatches internally |
| `Tx.LoadMultiContextPack` | Explicit multi-seed with dedup |

### Context loading — return `[]CellRecord` (raw wire type)

| Method | Notes |
|---|---|
| `Tx.LoadContext` | Original primitive, no budget |
| `Tx.LoadContextAt` | MVCC temporal variant |
| `Tx.LoadContextByEdges` | Graph BFS traversal |
| `Tx.LoadContextLOD` | Level-of-detail coarsening for large radii |
| `Tx.LoadContextFOV` | Field-of-view occlusion |
| `Tx.LoadContextVoronoi` | Multi-region Voronoi partition → `map[int][]CellRecord` |

### Type inconsistency

`LoadContextByEdges`, `LoadContextLOD`, `LoadContextFOV` all return `[]CellRecord` — the
raw wire format — while `LoadContextPackFrom` returns `ContextPack` with assembled `CellView`
values (facets, edges, seam refs). This means callers get different richness depending on
which method they choose.

---

## Proposed Design

### Single entry point

```go
// LoadContextConfig is the unified configuration for all context loading strategies.
// The DB selects the best internal algorithm based on the provided fields.
type LoadContextConfig struct {
    // Seeds is one or more seed coordinates (required).
    // Multiple seeds → deduped multi-seed ring walk.
    Seeds []Coord

    // MaxRing is the maximum ring radius to expand (default 5).
    // The DB automatically switches to LOD coarsening when MaxRing >= LODThreshold (default 10).
    MaxRing int

    // MaxTokens is the byte/token budget (default 4096).
    MaxTokens int

    // Budgeter counts tokens. Nil defaults to ByteLenBudgeter.
    Budgeter TokenBudgeter

    // EdgeFilter, when non-empty, switches to graph BFS traversal instead of ring walk.
    // Value is a comma-separated list of relation types to follow (e.g. "requires,causes").
    // Empty string = all relation types when combined with MaxHops > 0.
    EdgeFilter string

    // MaxHops limits BFS depth when EdgeFilter is set (default 5).
    MaxHops int

    // AsOf pins the read to a specific time (MVCC only). Nil = current snapshot.
    AsOf *time.Time

    // Assembly controls CellView enrichment (facets, edges, seams, supersession).
    Assembly LoadContextBudgetConfig
}

// LoadContext is the unified context loading entry point.
// The DB selects the optimal algorithm:
//   - EdgeFilter non-empty  → graph BFS traversal (LoadContextByEdges internally)
//   - MaxRing >= LODThreshold AND single seed → LOD coarsened ring walk
//   - Multiple seeds → deduped multi-seed ring walk
//   - Otherwise → standard ring walk with budgeting
//
// Always returns ContextPack regardless of internal algorithm.
func (tx *Tx) LoadContext(ctx context.Context, cfg LoadContextConfig) (ContextPack, error)
```

### Internal dispatch logic (pseudocode)

```
LoadContext(cfg):
    normalize defaults (MaxRing, MaxTokens, Budgeter, MaxHops)

    if cfg.EdgeFilter != "" || cfg.MaxHops > 0:
        → internal graph BFS (assemble CellViews from []CellRecord result)
        → wrap into ContextPack

    if len(cfg.Seeds) == 1 && cfg.MaxRing >= lodThreshold:
        → internal LOD ring walk (assemble CellViews from []CellRecord result)
        → wrap into ContextPack

    if len(cfg.Seeds) > 1:
        → LoadMultiContextPack (already returns ContextPack)

    → LoadContextPack single-seed (already returns ContextPack)
```

### LOD threshold

```go
// lodAutoThreshold is the MaxRing value above which LoadContext automatically
// uses Level-of-Detail coarsening for single-seed loads.
// At ring 10+, LOD reduces outer-ring lookups from O(6k) to O(6k/CoarseFactor²).
const lodAutoThreshold = 10
```

This constant is internal. Callers never see it.

### FOV and Voronoi — keep as specialist overrides

`LoadContextFOV` requires a caller-supplied `opaque func(Coord) bool` predicate — the DB
cannot supply this automatically. It stays public but clearly documented as a specialist API.

`LoadContextVoronoi` returns `map[int][]CellRecord` by design (per-region result) — a
fundamentally different shape. It stays public, but return type should be upgraded to
`map[int]ContextPack` for consistency.

---

## Migration Plan

### Phase 1 — Add unified entry point (non-breaking)

- Add `LoadContextConfig` struct
- Add `Tx.LoadContext(ctx, LoadContextConfig) (ContextPack, error)`
- Internal dispatch to existing implementations
- All existing methods remain exported and functional

### Phase 2 — Fix return type inconsistency

- `LoadContextByEdges` → return `ContextPack` (assemble CellViews internally)
- `LoadContextLOD` → return `ContextPack`
- `LoadContextFOV` → return `ContextPack` (or `[]CellView`)
- `LoadContextVoronoi` → return `map[int]ContextPack`

### Phase 3 — Deprecate redundant methods

Add `// Deprecated: use Tx.LoadContext instead.` godoc to:

- `Tx.LoadContextWithBudgeting`
- `Tx.LoadContextPack`
- `Tx.LoadContextPackFrom`
- `Tx.LoadMultiContextPack`
- `Tx.LoadContext` (the old primitive returning `[]CellRecord` — rename conflict to resolve)
- `Tx.LoadContextAt`

The old primitive `Tx.LoadContext` (returns `[]CellRecord`) conflicts with the new name.
Resolution: rename old primitive to `Tx.loadContextRaw` (unexport it) since it has no
direct callers outside the package — it is only called by `LoadContextWithBudgeting` internally.

### Phase 4 — Cleanup (v0.6.0 or later)

- Remove deprecated methods
- Remove `CellRecord` from public API (only needed because old spatial tools return it)
- Keep `ProvenanceWire`, `ValidityWire` as they are needed for `CellRecord`-based CDC via `SnapshotDiff`

---

## Raw vs Assembled Return Type

### The CellRecord vs CellView distinction

`CellRecord` is the raw wire format (B+ tree value, decoded). `CellView` is the assembled
read model: it adds facets, edges, seam refs, and is the type `ContextPack.Cells` holds.

All context loading should return `ContextPack` with `CellView` cells. Internal methods that
currently return `[]CellRecord` need an assembly step added before returning to the caller.

The assembly cost is negligible for typical use (it is already done by `LoadContextPackFrom`).

### What CellRecord is legitimately needed for

- `SnapshotDiff` — returns `CellDiff.Record record.CellRecord` for CDC use cases
- `PutCell` — write side takes `CellRecord`
- `AscendEdgesFrom` / `AscendFacetsForCell` — walk callbacks use wire records

These are **write/CDC paths** — correct to keep. The read/context-assembly path should
exclusively use `CellView` / `ContextPack`.

---

## API Surface After Simplification

### Primary (stable)

| Symbol | Purpose |
|---|---|
| `Tx.LoadContext(ctx, LoadContextConfig) ContextPack` | **Unified entry point** |
| `LoadContextConfig` | Configuration struct |
| `ContextPack`, `CellView`, `ContextPackStats` | Result types |
| `TokenBudgeter`, `ByteLenBudgeter` | Budget interface + default |
| `LoadContextBudgetConfig` | Assembly options (seams, supersession, facets) |

### Specialist (stable, documented as advanced)

| Symbol | Purpose |
|---|---|
| `Tx.LoadContextFOV` | FOV with caller-supplied opaque predicate |
| `Tx.LoadContextVoronoi` | Multi-region Voronoi partition |
| `Tx.FindEdgePath` | A* shortest path via edges |
| `Tx.WalkEdges` | BFS edge walk returning coordinates |
| `Tx.RingDensityMap` | Per-ring occupancy counts |

### Deprecated (removed in v0.6.0)

- `Tx.LoadContextWithBudgeting`
- `Tx.LoadContextPack`
- `Tx.LoadContextPackFrom`
- `Tx.LoadMultiContextPack`
- `Tx.LoadContextByEdges` (folded into `LoadContext` via `EdgeFilter`)
- `Tx.LoadContextLOD` (folded into `LoadContext` auto-dispatch)
- `Tx.LoadContextAt` (folded into `LoadContext` via `AsOf *time.Time`)
- The old raw `Tx.LoadContext` → unexported as `loadContextRaw`

### Kept but under review

| Symbol | Concern |
|---|---|
| `Tx.Get`, `Tx.Put`, `Tx.AscendRange` | Raw KV — risk of internal key namespace corruption; consider deprecating `Put` |
| `RenderHexGrid`, `RenderHexGridFromDB` | Debug/TUI utility; consider moving to `cmd/tui` |

---

## Versioning

- **Phase 1+2** (add `LoadContext`, fix return types): `v0.5.0` — new features, no removals
- **Phase 3** (add deprecation notices): `v0.5.x` — documentation only
- **Phase 4** (remove deprecated): `v0.6.0` — breaking removals, documented in CHANGELOG

---

## Open Questions

1. **`Tx.Put` exposure** — should raw `Put` be deprecated entirely or just documented with
   strong warnings? Rotation and changelog use `putDirect` internally; the public `Put` is
   only needed if an external caller wants raw KV access alongside HexxlaDB primitives.

2. **`LoadContextVoronoi` return type** — upgrade to `map[int]ContextPack` in Phase 2, or
   keep `map[int][]CellRecord` as it is a specialist use case and the assembly cost may
   not be wanted by all callers?

3. **`FOVContextConfig` and `LODContextConfig`** — these config structs remain public for
   the specialist APIs. After `LoadContextLOD` is folded into `LoadContext`, `LODContextConfig`
   can be removed (the LOD threshold and coarse factor become internal constants or `Options` fields).
