# Unified Context API

**Branch:** `feat/unified-context-api`
**Status:** Complete — implemented and deprecated methods removed
**Version:** v0.5.0 (minor — breaking API surface reduction)

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

## Pre-unification API (historical)

Before unification, the API had two incompatible return types and 10+ overlapping methods:

| Category              | Methods                                                                                                              | Return type                              |
| --------------------- | -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| ContextPack-returning | `LoadContextWithBudgeting`, `LoadContextPack`, `LoadContextPackFrom`, `LoadMultiContextPack`                         | `ContextPack`                            |
| Raw-returning         | `LoadContext` (old), `LoadContextAt`, `LoadContextByEdges`, `LoadContextLOD`, `LoadContextFOV`, `LoadContextVoronoi` | `[]CellRecord` or `map[int][]CellRecord` |

All ContextPack-returning methods and the raw spatial methods (`LoadContextByEdges`, `LoadContextLOD`) have been **removed**. Low-level raw scan is still available as `ScanContextRaw` / `ScanContextAtRaw`.

---

## Design

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
//   - EdgeFilter non-empty  → graph BFS traversal
//   - MaxRing >= 10 AND single seed → LOD coarsened ring walk
//   - Multiple seeds → concurrent multi-seed ring walk, merged under shared budget
//   - Otherwise → standard ring walk with budgeting
//
// Always returns ContextPack regardless of internal algorithm.
func (tx *Tx) LoadContext(ctx context.Context, cfg LoadContextConfig) (ContextPack, error)
```

### Internal dispatch logic

```
LoadContext(cfg):
    normalize defaults (MaxRing=5, MaxTokens=4096, Budgeter=ByteLenBudgeter{}, MaxHops=5)

    if cfg.EdgeFilter != "" || cfg.MaxHops > 0:
        → internal graph BFS via WalkEdges
        → assemble CellViews from reached coords
        → wrap into ContextPack

    if len(cfg.Seeds) == 1 && cfg.MaxRing >= lodAutoThreshold (10):
        → internal LOD ring walk (coarsen outer rings)
        → assemble CellViews
        → wrap into ContextPack

    if len(cfg.Seeds) > 1:
        → concurrent per-seed ring walks (goroutine per seed)
        → merge + dedup under shared token budget
        → wrap into ContextPack

    → standard single-seed ring walk with budgeting
        → wrap into ContextPack
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

## Completed migration

### What was removed

- `Tx.LoadContextWithBudgeting` — core implementation, folded into `LoadContext` dispatch
- `Tx.LoadContextPack` — alias of above
- `Tx.LoadContextPackFrom` — variadic multi-seed shim
- `Tx.LoadMultiContextPack` — explicit multi-seed
- `Tx.LoadContextByEdges` — graph BFS; replaced by `LoadContext` with `EdgeFilter`
- `Tx.LoadContextLOD` + `LODContextConfig` — LOD coarsening; auto-dispatched by `LoadContext` when `MaxRing >= 10`
- `MultiContextConfig` struct

### What was renamed

- Old `Tx.LoadContext` (returned `[]CellRecord`) → `Tx.ScanContextRaw`
- Old `Tx.LoadContextAt` (returned `[]CellRecord`) → `Tx.ScanContextAtRaw`

### What was kept

- `Tx.LoadContextFOV` — specialist: requires caller-supplied opaque predicate
- `Tx.LoadContextVoronoi` — specialist: non-overlapping per-region output shape
- `Tx.ScanContextRaw`, `Tx.ScanContextAtRaw` — low-level raw scan primitives
- `CellRecord` re-export — still needed by `SnapshotDiff`, `PutCell`, write paths

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

| Symbol                                               | Purpose                                        |
| ---------------------------------------------------- | ---------------------------------------------- |
| `Tx.LoadContext(ctx, LoadContextConfig) ContextPack` | **Unified entry point**                        |
| `LoadContextConfig`                                  | Configuration struct                           |
| `ContextPack`, `CellView`, `ContextPackStats`        | Result types                                   |
| `TokenBudgeter`, `ByteLenBudgeter`                   | Budget interface + default                     |
| `LoadContextBudgetConfig`                            | Assembly options (seams, supersession, facets) |

### Specialist (stable, documented as advanced)

| Symbol                  | Purpose                                   |
| ----------------------- | ----------------------------------------- |
| `Tx.LoadContextFOV`     | FOV with caller-supplied opaque predicate |
| `Tx.LoadContextVoronoi` | Multi-region Voronoi partition            |
| `Tx.FindEdgePath`       | A\* shortest path via edges               |
| `Tx.WalkEdges`          | BFS edge walk returning coordinates       |
| `Tx.RingDensityMap`     | Per-ring occupancy counts                 |

### Removed (v0.5.0)

- `Tx.LoadContextWithBudgeting`
- `Tx.LoadContextPack`
- `Tx.LoadContextPackFrom`
- `Tx.LoadMultiContextPack`
- `Tx.LoadContextByEdges` (folded into `LoadContext` via `EdgeFilter`)
- `Tx.LoadContextLOD` (folded into `LoadContext` auto-dispatch)
- `MultiContextConfig`, `LODContextConfig`
- Old raw `Tx.LoadContext` → renamed `Tx.ScanContextRaw`
- Old raw `Tx.LoadContextAt` → renamed `Tx.ScanContextAtRaw`

### Kept but under review

| Symbol                                 | Concern                                                                        |
| -------------------------------------- | ------------------------------------------------------------------------------ |
| `Tx.Get`, `Tx.Put`, `Tx.AscendRange`   | Raw KV — risk of internal key namespace corruption; consider deprecating `Put` |
| `RenderHexGrid`, `RenderHexGridFromDB` | Debug/TUI utility; consider moving to `cmd/tui`                                |

---

## Versioning

- **v0.5.0** — added `LoadContext`, renamed raw primitives, removed all deprecated methods

---

## Open Questions

1. **`Tx.Put` exposure** — should raw `Put` be deprecated entirely or just documented with
   strong warnings? Rotation and changelog use `putDirect` internally; the public `Put` is
   only needed if an external caller wants raw KV access alongside HexxlaDB primitives.

2. **`LoadContextVoronoi` return type** — upgrade to `map[int]ContextPack` in a future version,
   or keep `map[int][]CellRecord` as it is a specialist use case and the assembly cost may
   not be wanted by all callers?
