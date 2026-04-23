# HEXXLA memory model vs `hexxladb` library

**Audience:** Integrators aligning [HEXXLA.md](./HEXXLA.md) with this module. **Normative storage:** [HEXXLA_DB.md](./HEXXLA_DB.md).

## Spec layers (library vs assembly vs product)

[HEXXLA.md](./HEXXLA.md) combines three layers. Use this split to avoid treating product-only items as missing engine features.

| Layer | Scope in this repo | Primary docs |
|-------|-------------------|--------------|
| **A — HexxlaDB library** | `package hexxladb`, `internal/engine`, MVCC, changefeed | [HEXXLA_DB.md](./HEXXLA_DB.md), [ADOPTION.md](./ADOPTION.md) |
| **B — Assembly / mapping** | [views.go](../../views.go), wire types, token-budget helpers | This document, [MVCC_TEMPORAL.md](./MVCC_TEMPORAL.md) |
| **C — Hexxla product** | Seed selection, filters/ranking beyond primitives, auto-seams, HTTP/JSON, dashboard | [HEXXLA.md](./HEXXLA.md) Implementation Notes; implement in **your service** (not shipped in **hexxladb**—this repo keeps **`internal/adapters/in`** as a stub; use **`internal/app`** patterns when embedding) |

**Modern Go:** follow `go.mod` / CI; treat [MODERN_GO.md](../context/MODERN_GO.md) as a release inventory for toolchain upgrades, not a mandate for repo-wide rewrites.

**Hexagonal:** persistence crosses **[`domain.Storage`](../../internal/domain/storage.go)** implemented by **[`internal/adapters/out/hexxladb`](../../internal/adapters/out/hexxladb/storage.go)**; new transports belong in **`internal/adapters/in`**—see **[HEXXLA_PRODUCT_WIRING.md](./HEXXLA_PRODUCT_WIRING.md)**.

## Core types

| HEXXLA.md (concept) | Shipped in library | Notes |
|---------------------|-------------------|--------|
| `Coord`, geometry | [`Coord`](../../coord_export.go), [`Pack`](../../coord_export.go), [`Ring`](../../coord_export.go), [`WalkRings`](../../coord_export.go) | Axial + Morton [`PackedCoord`](../../coord_export.go). |
| `Cell` / wire storage | [`record.CellRecord`](../../internal/record/types.go) | No `ActiveFacet` on disk—product state above the store. |
| `CellView` | [`CellView`](../../views.go), [`Tx.AssembleCellView`](../../views.go) | Aggregates facets/edges/seams per opts. |
| `FacetView` | [`FacetView`](../../views.go) | From [`record.FacetRecord`](../../internal/record/types.go). |
| `ContextPack` | [`ContextPack`](../../views.go), [`Tx.LoadContextWithBudgeting`](../../views.go), [`Tx.LoadContextPack`](../../views.go) | Token eviction: outer ring, lowest [`Provenance.Confidence`](../../internal/record/types.go) first. |
| `Seam` / `SeamRef` | [`record.SeamRecord`](../../internal/record/types.go), [`SeamRef`](../../views.go) | ULID keys; full seam in `ContextPack.Seams`. |
| `load_context` (tokens) | [`Tx.LoadContext`](../../primitives.go) (`maxCells`), [`LoadContextWithBudgeting`](../../views.go) | Wire-first vs token-budget helper. |

### Filter recipe and `ContextPack` naming

[HEXXLA.md](./HEXXLA.md) names primitives `walk_ring(..., filters Filter)` and `load_context(..., filters Filter)`. There is no single opaque **`Filter`** type; combine primitives instead:

- **Facet mask:** [`Tx.WalkRingFacets`](../../primitives.go) (`facetMask uint8`, optional `asOf`).
- **Tag / confidence / validity:** btree tag discovery via **[`Tx.AscendCellsByTag`](../../cell_secondary.go)** (prefix scan on **`tag/`** maintained by **[`PutCell`](../../primitives.go)**); otherwise filter after [`WalkRing`](../../primitives.go), [`LoadContext`](../../primitives.go), or post-process **[`CellView`](../../views.go)** slices with **[`CellViewPredicate`](../../views.go)** / **[`FilterCellViews`](../../views.go)**; alternatively wrap **[`TokenBudgeter`](../../views.go)** to skip rows during **[`LoadContextWithBudgeting`](../../views.go)** (or truncate an assembled pack with **[`TruncateCellViewsToTokenBudget`](../../views.go)**).

**Naming:** the spec’s `load_context` → **`ContextPack`** shape matches [`ContextPack`](../../views.go); **`Tx.LoadContextWithBudgeting`** and the alias **`Tx.LoadContextPack`** both return `ContextPack`. [`Tx.LoadContext`](../../primitives.go) remains the **`maxCells`** wire-first primitive.

## Orchestration vs engine (Track C boundaries)

| Behavior | Where it lives |
|----------|----------------|
| Seed selection (embeddings, lexical, tags-only) | **Outside** core DB—application or search layer. |
| Automatic seams on `put_cell` / thresholds | **Not** in [`PutCell`](../../primitives.go); use [`PutSeam`](../../primitives.go) / [`MarkConflict`](../../primitives.go) from orchestration. |
| Facet 2 “conflict notes” auto-fill | **Not** automatic on seam write; update facets in same [`Update`](../../tx.go) if required. |
| `ActiveFacet` rotation | **Client/app** state unless you add a dedicated field or side table in a higher layer. |
| Tag secondary index (`tag/`) | **Shipped:** [`Tx.AscendCellsByTag`](../../cell_secondary.go); multi-tag ranking / lexical search **outside** core DB remains product-owned. |
| HTTP/JSON tool API | **Not** this module; separate service using [`package hexxladb`](../../doc.go). |

## Ports

[`domain.Storage`](../../internal/domain/storage.go) mirrors wire primitives for hexagonal wiring. Optional **view** APIs are consumed directly from **`hexxladb`** unless you extend the port deliberately—avoid forcing every adapter to implement assembly helpers.

## Related

- [ADOPTION.md](./ADOPTION.md) — Operational rollout and post–v1 backlog pointer.
- [SERVICE_INTEGRATION.md](./SERVICE_INTEGRATION.md) — Best practices for services (tags, seeds, edges, writes).
- [HEXXLA_PRODUCT_WIRING.md](./HEXXLA_PRODUCT_WIRING.md) — Where HTTP/orchestration work belongs (hexagonal).
- [HEXXLA.md](./HEXXLA.md) — Product memory model.
