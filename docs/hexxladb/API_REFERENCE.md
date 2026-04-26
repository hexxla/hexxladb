# HexxlaDB API reference (full inventory)

**Audience:** Anyone integrating **`package hexxladb`** (`github.com/hexxla/hexxladb`).
**Normative storage:** **[HEXXLA_DB.md](./HEXXLA_DB.md)**. Transactions and snapshots: **[TX.md](./TX.md)**. Memory model: **[HEXXLA.md](./HEXXLA.md)**.

This document lists **every exported symbol** in the root package as of the current tree, grouped by role. Use it as the single checklist when auditing coverage (tests, demos, product adapters). Generated docs: [pkg.go.dev](https://pkg.go.dev/github.com/hexxla/hexxladb); the **[`doc.go`](../../doc.go)** package comment stays the short overview.

---

## Storage limits (engine B+ tree)

Opaque keys and values passed through **[`(*Tx).Put`](../../tx.go)** are stored in the engine B+ tree. **Maximum key length: 256 bytes; maximum value length: 8192 bytes (8KB)** per page layout in [`internal/engine/btree_page.go`](../../internal/engine/btree_page.go). Rationale and format details: **[`ORDERED_STORE.md`](../../internal/engine/ORDERED_STORE.md)**. **Cell** (and other encoded) records must fit in that value budget; larger logical payloads require application-level chunking or external blob storage.

---

## Database lifecycle

| Symbol                                  | Notes                                                                                       |
| --------------------------------------- | ------------------------------------------------------------------------------------------- |
| **[`Open`](../../db.go)**               | Open or create a database file; applies WAL on startup.                                     |
| **[`(*DB).Close`](../../db.go)**        | Waits for in-flight transactions; idempotent for nil receiver.                              |
| **[`ErrCorruptDatabase`](../../db.go)** | Open-time corruption (header/WAL).                                                          |
| **[`Options`](../../options.go)**       | `EnableMVCC`, `MVCCRetention`, changelog, encryption, `CellValidator`, optional page hooks. |
| **[`MVCCRetention`](../../options.go)** | Retention hint for prune suggestions.                                                       |

---

## Transactions and MVCC snapshots

| Symbol                                 | Notes                                                                  |
| -------------------------------------- | ---------------------------------------------------------------------- |
| **[`(*DB).View`](../../tx.go)**        | Read-only; pins MVCC `read_seq` at start when format v2.               |
| **[`(*DB).Update`](../../tx.go)**      | Exclusive write lock; logical writes via `*Tx`.                        |
| **[`(*DB).Batch`](../../tx.go)**       | Same as **`Update`** (alias for naming parity).                        |
| **[`(*DB).ViewAt`](../../tx.go)**      | Pin **`read_seq`** snapshot (MVCC); **`ErrReadSeqFuture`** if too new. |
| **[`(*DB).ViewAtTime`](../../tx.go)**  | Map wall-clock to latest commit ≤ `as_of`; pins that snapshot.         |
| **[`(*Tx).Writable`](../../tx.go)**    | True inside **`Update`** / **`Batch`**.                                |
| **[`(*Tx).Get`](../../tx.go)**         | Raw btree **Get** by byte key.                                         |
| **[`(*Tx).Put`](../../tx.go)**         | Raw btree **Put** (writes require **`Update`**).                       |
| **[`(*Tx).AscendRange`](../../tx.go)** | Ordered scan **[from, to)** over byte keys.                            |

---

## Cells, seams, rings, context (primitives)

| Symbol                                            | Notes                                                                                                             |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| **[`(*Tx).PutCell`](../../primitives.go)**        | Cell primary + secondaries (`source/`, `time/`, `tag/`).                                                          |
| **[`(*Tx).GetCell`](../../primitives.go)**        | Decode visible cell at packed coord.                                                                              |
| **[`(*Tx).WalkRing`](../../primitives.go)**       | Visit one ring; raw bytes per coord.                                                                              |
| **[`(*Tx).WalkRingAt`](../../primitives.go)**     | Same order; **`record.ValidAt`** filter at **`asOf`**.                                                            |
| **[`(*Tx).LoadContext`](../../primitives.go)**    | Concentric walk; **`maxR`**, **`maxCells`**.                                                                      |
| **[`(*Tx).LoadContextAt`](../../primitives.go)**  | Same as **`LoadContext`** + validity filter.                                                                      |
| **[`(*Tx).WalkRingFacets`](../../primitives.go)** | Facet_mask ring walk; optional validity on cell.                                                                  |
| **[`(*Tx).PutSeam`](../../primitives.go)**        | Seam primary + **`seam-by-cells/`** + seam secondaries.                                                           |
| **[`(*Tx).FindSeams`](../../primitives.go)**      | Query ball using seam index + primaries.                                                                          |
| **[`(*Tx).FindSeamsAt`](../../primitives.go)**    | **`FindSeams`** + seam validity filter.                                                                           |
| **[`(*Tx).MarkConflict`](../../primitives.go)**   | Spec sugar: ULID seam **`mark_conflict`**.                                                                        |
| **[`(*Tx).MarkSupersedes`](../../primitives.go)** | Record directional supersession: superseder replaces superseded; used by **`FilterSuperseded`** context assembly. |
| **[`(*Tx).ResolveSeam`](../../primitives.go)**    | Update resolution fields on **`seam/<ulid>`**.                                                                    |
| **`SeamTypeConflict`**, **`SeamTypeSupersedes`**  | Canonical seam type string constants (**`"mark_conflict"`**, **`"supersedes"`**).                                 |

Validity filtering uses **`record.ValidAt`** ([`internal/record/validity.go`](../../internal/record/validity.go)); external code often reaches it via **`Tx`** helpers above rather than importing **`internal/record`** from outside this module.

---

## Facets and edges

| Symbol                                                   | Notes                                                                  |
| -------------------------------------------------------- | ---------------------------------------------------------------------- |
| **[`(*Tx).PutFacet`](../../facets_edges.go)**            | Upsert facet record (derivation discipline as per spec).               |
| **[`(*Tx).UpdateFacet`](../../facets_edges.go)**         | Requires derivation hash match; else **`ErrFacetDerivationMismatch`**. |
| **[`(*Tx).GetFacet`](../../facets_edges.go)**            | Lookup by packed coord + facet id.                                     |
| **[`(*Tx).AscendFacetsForCell`](../../facets_edges.go)** | All facets at a cell key.                                              |
| **[`(*Tx).PutEdge`](../../facets_edges.go)**             | Directed edge primary.                                                 |
| **[`(*Tx).GetEdge`](../../facets_edges.go)**             | Edge lookup by endpoints + relation type.                              |
| **[`(*Tx).AscendEdgesFrom`](../../facets_edges.go)**     | Out-edges from a packed coord.                                         |
| **[`(*Tx).LinkCells`](../../facets_edges.go)**           | Sugar: pack coords + **`PutEdge`**.                                    |

---

## Secondary indexes

| Symbol                                                         | Notes                                                                 |
| -------------------------------------------------------------- | --------------------------------------------------------------------- |
| **[`(*Tx).AscendCellsBySource`](../../cell_secondary.go)**     | Prefix on **`source/<source_id>/…`**.                                 |
| **[`(*Tx).AscendCellsInTimeBucket`](../../cell_secondary.go)** | One UTC week bucket from **`time/`**.                                 |
| **[`(*Tx).AscendCellsByTag`](../../cell_secondary.go)**        | Prefix on **`tag/<tag>/…`**.                                          |
| **[`(*Tx).AscendDistinctTags`](../../cell_secondary.go)**      | Distinct tag strings visible at this snapshot (streams via callback). |
| **[`(*Tx).ListExistingTopics`](../../cell_secondary.go)**      | Sorted distinct tags (topic names) for tools.                         |
| **[`(*Tx).AscendSeamsBySource`](../../seam_secondary.go)**     | **`seam-source/…`**.                                                  |
| **[`(*Tx).AscendSeamsInTimeBucket`](../../seam_secondary.go)** | **`seam-time/…`**.                                                    |

---

## ASCII hex grid rendering

| Symbol                                                 | Notes                                                       |
| ------------------------------------------------------ | ----------------------------------------------------------- |
| **[`RenderHexGrid`](../../hex_render.go)**             | Pure ASCII hex grid from center to maxR with custom labels. |
| **[`(*Tx).RenderHexGridFromDB`](../../hex_render.go)** | ASCII grid showing occupied (`*`) vs empty (`.`) cells.     |
| **[`HexGridCell`](../../hex_render.go)**               | Coord + label pair for grid rendering.                      |

---

## Batch operations and bulk I/O

| Symbol                                          | Notes                                                         |
| ----------------------------------------------- | ------------------------------------------------------------- |
| **[`(*DB).BatchPutCells`](../../batch_put.go)** | Batched multi-cell write with progress and continue-on-error. |
| **[`BatchPutCellOptions`](../../batch_put.go)** | Batch size, progress callback, continue-on-error flag.        |
| **[`BatchPutCellResult`](../../batch_put.go)**  | Written count + per-cell errors.                              |
| **[`BatchPutCellError`](../../batch_put.go)**   | Index + error for failed cells.                               |
| **[`(*Tx).ExportCellsJSON`](../../bulk_io.go)** | Stream visible cells as JSON array to writer.                 |
| **[`(*DB).ImportCellsJSON`](../../bulk_io.go)** | Read JSON array of cells and batch-write via PutCell.         |

---

## Cell templates

| Symbol                                               | Notes                                                    |
| ---------------------------------------------------- | -------------------------------------------------------- |
| **[`NewUserMessageCell`](../../templates.go)**       | Factory for user-message cells with standard tags.       |
| **[`NewAssistantResponseCell`](../../templates.go)** | Factory for assistant-response cells with standard tags. |
| **[`NewSystemPromptCell`](../../templates.go)**      | Factory for system-prompt cells (confidence 1.0).        |
| **[`NewFactCell`](../../templates.go)**              | Factory for extracted-fact cells with category tag.      |

---

## Tag analytics and ring density

| Symbol                                                 | Notes                                                         |
| ------------------------------------------------------ | ------------------------------------------------------------- |
| **[`(*Tx).TagCounts`](../../tag_analytics.go)**        | Per-tag cell counts, sorted by count descending.              |
| **[`(*Tx).TagCooccurrences`](../../tag_analytics.go)** | Tag pairs appearing together on cells, with min-count filter. |
| **[`(*Tx).UntaggedCells`](../../tag_analytics.go)**    | Coords of visible cells with no tags within a ring radius.    |
| **[`TagCount`](../../tag_analytics.go)**               | Tag string + count pair.                                      |
| **[`TagPair`](../../tag_analytics.go)**                | Co-occurring tag pair + count.                                |
| **[`(*Tx).RingDensityMap`](../../ring_density.go)**    | Per-ring occupied vs total cell counts from center.           |
| **[`TotalDensity`](../../ring_density.go)**            | Aggregate occupied/total across a `[]RingDensity`.            |
| **[`RingDensity`](../../ring_density.go)**             | Ring distance + occupied + total counts.                      |

---

## HEXXLA-shaped views and budgeting

| Symbol                                                                                                                                                                      | Notes                                                                                                                                                                             |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*Tx).AssembleCellView`](../../views.go)**                                                                                                                              | One coord → **`CellView`** with opts.                                                                                                                                             |
| **[`(*Tx).LoadContextWithBudgeting`](../../views.go)**                                                                                                                      | Token-budget **`ContextPack`**.                                                                                                                                                   |
| **[`(*Tx).LoadContextPack`](../../views.go)**                                                                                                                               | Alias of **`LoadContextWithBudgeting`**.                                                                                                                                          |
| **[`(*Tx).LoadContextPackFrom`](../../views.go)**                                                                                                                           | Unified variadic entry point: one coord → `LoadContextPack`; multiple coords → `LoadMultiContextPack` with deduplication. Zero overhead for single-seed callers.                  |
| **[`CellView`](../../views.go)**, **[`ContextPack`](../../views.go)**, **[`FacetView`](../../views.go)**, **[`EdgeView`](../../views.go)**, **[`SeamRef`](../../views.go)** | View types. **`CellView.SupersededFrom`** is set when a cell substituted a superseded cell during assembly.                                                                       |
| **[`LoadContextBudgetConfig`](../../views.go)**, **[`AssembleCellViewOpts`](../../views.go)**, **[`DefaultAssembleCellViewOpts`](../../views.go)**                          | Assembly + seam radius + caps. **`FilterSuperseded`** enables seam-aware context assembly.                                                                                        |
| **[`TokenBudgeter`](../../views.go)**, **[`ByteLenBudgeter`](../../views.go)**                                                                                              | Budget counting.                                                                                                                                                                  |
| **[`CellViewPredicate`](../../views.go)**, **[`FilterCellViews`](../../views.go)**, **[`TruncateCellViewsToTokenBudget`](../../views.go)**                                  | Post-process assembled views.                                                                                                                                                     |
| **[`ContextPackStats`](../../views.go)**                                                                                                                                    | Assembly stats: candidates, evicted, max ring.                                                                                                                                    |
| **[`CellExplanation`](../../views.go)**                                                                                                                                     | Per-cell inclusion/eviction reason (Explain mode). **`Reason`**: `"included"`, `"evicted_low_confidence"`, `"superseded"`. **`SupersededBy`** coord set on superseded exclusions. |

---

## Lattice exports

| Symbol                                                                                                                | Notes                            |
| --------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| **[`Coord`](../../coord_export.go)**, **[`Cube`](../../coord_export.go)**, **[`PackedCoord`](../../coord_export.go)** | Geometry types.                  |
| **[`MaxAxialAbs`](../../coord_export.go)**                                                                            | Packed range bound.              |
| **[`ErrCoordOutOfRange`](../../coord_export.go)**                                                                     | **`Pack`** precondition.         |
| **[`Pack`](../../coord_export.go)**, **[`Unpack`](../../coord_export.go)**                                            | Morton pack/unpack.              |
| **[`Ring`](../../coord_export.go)**, **[`WalkRings`](../../coord_export.go)**                                         | Same order as **`LoadContext`**. |

Methods on **`Coord`** / **`PackedCoord`** (e.g. **`Distance`**, **`Neighbors`**) live on re-exported types — see **`internal/lattice`**.

---

## MVCC retention and pruning

| Symbol                                                                                        | Notes                                                         |
| --------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| **[`(*DB).StatsMVCC`](../../mvcc_lifecycle.go)**                                              | Counters for versioned rows.                                  |
| **[`(*DB).GroupWALStats`](../../db.go)**                                                      | Group-WAL flusher metrics (when group commit is configured).  |
| **[`(*DB).SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go)**                                | Policy from **`MVCCRetention`**.                              |
| **[`(*DB).MVCCPrunePlan`](../../mvcc_lifecycle.go)**                                          | Combine suggestion + batch size profile.                      |
| **[`(*DB).PruneCellVersions`](../../mvcc_lifecycle.go)**                                      | Delete stale cell versions before **`beforeSeq`**.            |
| **[`(*DB).PruneCellVersionsByProfile`](../../mvcc_lifecycle.go)**                             | Same with profile-driven **`maxDelete`**.                     |
| **[`MVCCStats`](../../mvcc_lifecycle.go)**, **[`MVCCPruneProfile`](../../mvcc_lifecycle.go)** | **`MVCCPruneLowLatency`**, **`Balanced`**, **`LongHistory`**. |
| **[`PruneScheduler`](../../mvcc_lifecycle.go)**                                               | **`Tick`**: operator-driven periodic prune helper.            |

---

## Logical changefeed

| Symbol                                                                                                                                                         | Notes                                                    |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| **[`(*DB).ReadChangelogSince`](../../db_changelog.go)**                                                                                                        | Requires **`Options.ChangelogEnabled`**.                 |
| **[`(*DB).ReadChangelogFiltered`](../../db_changelog.go)**                                                                                                     | Filtered read by op codes and/or key prefix.             |
| **[`ChangelogFilter`](../../db_changelog.go)**                                                                                                                 | Filter config: **`Ops []byte`**, **`KeyPrefix []byte`**. |
| **[`ChangelogRecord`](../../db_changelog.go)**                                                                                                                 | Typed alias of internal record.                          |
| **`ChangelogOpPutCell`**, **`ChangelogOpPutSeam`**, **`ChangelogOpResolveSeam`** (only **`ResolveSeam`**), **`ChangelogOpPutFacet`**, **`ChangelogOpPutEdge`** | Stable op codes.                                         |

See **[CHANGEFEED.md](./CHANGEFEED.md)**.

---

## Database health check

| Symbol                                            | Notes                                                                                                                                                       |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*DB).HealthCheck`](../../health.go)**        | Integrity scan: cell count, seam resolution summary, orphaned seam detection, index consistency, MVCC stats.                                                |
| **[`HealthReport`](../../health.go)**             | Result type: `CellCount`, `SeamCount`, `SeamsResolved`, `SeamsUnresolved`, `OrphanedSeams`, `TagIndexErrors`, `SourceIndexErrors`, `MVCCStats`, `Warnings`. |
| **[`HealthCheckConfig`](../../health.go)**        | `CheckOrphans`, `CheckTagIndex`, `CheckSourceIndex`, `MaxErrors`, `ScanRadius`.                                                                             |
| **[`DefaultHealthCheckConfig`](../../health.go)** | Returns config with all checks enabled and `ScanRadius=64`.                                                                                                 |

---

## Composable Query Engine

`Tx.QueryCells` is the unified query entry point. All predicate fields are AND-combined; zero/empty values are ignored.

| Symbol                                        | Notes                                                                                                                                                                                                                    |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **[`(*Tx).QueryCells`](../../query_exec.go)** | Execute a `CellQuery` against the snapshot. Planner picks cheapest index; remaining predicates applied in-memory.                                                                                                        |
| **[`CellQuery`](../../query.go)**             | Predicate: `Query`, `RequireTags` (AND), `AnyTags` (OR), `ExcludeTags` (NOT), `SourceID`, `MinConfidence`, `MaxConfidence`, `After`/`Before` (temporal), `Center`+`Radius` (spatial), `MaxResults`, `SortBy`, `Explain`. |
| **[`CellQueryResult`](../../query.go)**       | `Cell CellView`, `Score float64`, `Explanation string` (when `Explain=true`).                                                                                                                                            |
| **[`SortOrder`](../../query.go)**             | `SortByScore` (default), `SortByConfidence`, `SortByRecency`, `SortByCoord`.                                                                                                                                             |

### Query planner index selection

| Condition               | Primary index                                                  |
| ----------------------- | -------------------------------------------------------------- |
| `RequireTags` non-empty | `tag/` secondary index                                         |
| `SourceID` set          | `source/` secondary index                                      |
| `After` or `Before` set | `time/` week-bucket index (single `AscendRange`, no full scan) |
| `Center`+`Radius` set   | Ring walk around `Center`                                      |
| Fallback                | Full scan (ring walk from origin, radius 32)                   |

### Temporal queries

`After`/`Before` filter on cell `ValidFrom`. Cells with no `ValidFrom` are excluded from any temporal query. Uses the existing `time/` weekly-bucket index.

---

## Content Search

`SearchCells` is a convenience wrapper over `QueryCells` kept for backward compatibility. For `ExcludeTags`, `SortBy`, `Explain`, or temporal filters, use `QueryCells` directly.

| Symbol                                     | Notes                                                                                                                                                                                                  |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **[`(*Tx).SearchCells`](../../search.go)** | Wrapper over `QueryCells`. Returns `[]CellSearchResult` sorted by score; each result includes a `Coord` for use as a context-pack seed.                                                                |
| **[`CellSearchConfig`](../../search.go)**  | `Query`, `RequireTags` (AND), `AnyTags` (OR), `MinConfidence`, `MaxConfidence`, `SourceID`, `Center`+`Radius`, `MaxResults`, `MaxScanRadius`. Forward-compatible: `Embedding []float32` addable later. |
| **[`CellSearchResult`](../../search.go)**  | `Cell CellView` + `Score float64`.                                                                                                                                                                     |

### Content Search scoring

| Condition                                      | Score contribution |
| ---------------------------------------------- | ------------------ |
| Query matches a tag exactly (case-insensitive) | +1.0               |
| Query is a prefix of a tag                     | +0.8               |
| Query found verbatim in `RawContent`           | +0.6               |
| Query found case-insensitively in `RawContent` | +0.5               |
| Query matches `SourceID` exactly               | +0.3               |
| Confidence bonus                               | +0.1 × Confidence  |

---

## Multi-seed context assembly

A **seed** is a `Coord` — the centre point of a ring-walk expansion. `SearchCells` returns `CellSearchResult` values each carrying a `Coord`; those coords are the seeds passed to the assembly APIs, which expand each matched location's neighbourhood into context. One natural-language or keyword query → N matched coords → N seeds → one merged `ContextPack`.

| Symbol                                                     | Notes                                                                                                                                                                                                          |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*Tx).LoadContextPackFrom`](../../views.go)**          | **Recommended unified entry point.** Variadic: one coord → zero-overhead `LoadContextPack`; multiple coords → `LoadMultiContextPack` with `DeduplicateCoords`. Callers never switch API based on result count. |
| **[`(*Tx).LoadMultiContextPack`](../../multi_context.go)** | Expand multiple seed coords (e.g. top-N from `SearchCells`), merge under a shared token budget, cross-seed confidence re-ranking, optional deduplication of shared neighbourhood cells.                        |
| **[`MultiContextConfig`](../../multi_context.go)**         | `Centers []Coord`, `MaxR`, `MaxTokens`, `Budgeter`, `AssemblyConfig LoadContextBudgetConfig`, `DeduplicateCoords`.                                                                                             |

### Typical pipeline

```text
SearchCells(query) → []CellSearchResult → extract .Cell.Coord → LoadContextPackFrom(coords...)
```

Token budget across seeds: each seed expands independently (ring walk, `FilterSuperseded`), cells merge into one pool, pool re-ranked by `Confidence` descending, greedy fill to `MaxTokens`.

---

## Encryption

| Symbol                                                 | Notes                                          |
| ------------------------------------------------------ | ---------------------------------------------- |
| **[`DeriveKeyFromPassphrase`](../../encryption.go)**   | KDF helper for **`Options.Passphrase`**.       |
| **[`RotateEncryption`](../../rotation.go)**            | Offline re-key / migration entry.              |
| **[`RotateEncryptionWithOptions`](../../rotation.go)** | Extended rotation + progress.                  |
| **[`RotateOptions`](../../rotation.go)**               | Batch size / **`OnProgress`** for long copies. |

See **[ENCRYPTION.md](./ENCRYPTION.md)**.

---

## Cell validation

| Symbol                                          | Notes                                                           |
| ----------------------------------------------- | --------------------------------------------------------------- |
| **[`CellValidator`](../../validation.go)**      | Interface: **`ValidateCell(CellRecord) error`** pre-write hook. |
| **[`CellValidatorFunc`](../../validation.go)**  | Adapter: plain function → **`CellValidator`**.                  |
| **[`Options.CellValidator`](../../options.go)** | Set on **`Open`** to enable pre-write validation in `PutCell`.  |

---

## Sentinel errors (complete)

| Variable                                                                                                                  | When                                 |
| ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| **`ErrNotImplemented`**                                                                                                   | Stub API.                            |
| **`ErrSeamNotFound`**                                                                                                     | Missing **`seam/<ulid>`**.           |
| **`ErrSeamEndpointMismatch`**                                                                                             | Immutable endpoints for ULID.        |
| **`ErrInvalidArgument`**                                                                                                  | Bad parameter.                       |
| **`ErrClosed`**                                                                                                           | Closed handle.                       |
| **`ErrDatabaseClosed`**                                                                                                   | **`DB`** closed.                     |
| **`ErrTxReadOnly`**                                                                                                       | Write in **`View`**.                 |
| **`ErrNilCallback`**                                                                                                      | Nil **`View`/`Update`** fn.          |
| **`ErrEncryptionKeyRequired`**, **`ErrDatabaseNotEncrypted`**, **`ErrEncryptionOptions`**, **`ErrEncryptionKeyMismatch`** | Encryption options vs file.          |
| **`ErrCellNotFound`**                                                                                                     | e.g. **`UpdateFacet`** without cell. |
| **`ErrFacetDerivationMismatch`**                                                                                          | Facet hash vs raw.                   |
| **`ErrChangelogDisabled`**, **`ErrChangelogCorrupt`**                                                                     | Changefeed.                          |
| **`ErrReadSeqFuture`**                                                                                                    | **`ViewAt`** too new.                |
| **`ErrCommitFinalization`**                                                                                               | Post-callback failure.               |
| **`ErrCorruptDatabase`**                                                                                                  | Open failure.                        |

Use **`errors.Is` / `errors.As`** for stable handling.

---

## Live demos and coverage

- **Conversational memory service:** [`examples/conversational_memory`](../../examples/conversational_memory/) — **`go run ./examples/conversational_memory`** seeds **`./.tmp/conversational_memory/`** (MVCC + changelog) and walks through cell storage (templates + batch), context assembly (stats + explain), tag analytics, query patterns, MVCC time-travel, ASCII grid rendering, ring density, and filtered changelog.

## What `examples/conversational_memory` does _not_ call (and why)

The demo is a **session-shaped production walkthrough**; it stays readable. Omitted APIs fall into a few buckets: **low-level escape hatches**, **validity-specialized variants**, **seam lifecycle / conflict sugar**, **post-assembly helpers**, **operator features**, **encryption ops**.

### Raw btree: `Tx.Get`, `Tx.Put`, `Tx.AscendRange`

- **Why omitted:** The demo targets **logical** cells/seams/facets/edges. Raw keys bypass **`cell/`** layout and indexes unless you duplicate encodings by hand.
- **Use when:** Custom migrations, debugging the engine, experimental index families, or tooling that walks **`__meta/`** keys — **not** typical product paths.

### Ring primitives: `WalkRing`, `WalkRingAt`, `WalkRingFacets`

- **Why omitted:** **`LoadContext`** / **`LoadContextPack`** already implement the normative “expand from seed” story the demo showcases; per-ring walks add verbosity without changing the narrative.
- **Use when:** Streaming one ring at a time, UI highlight of a single horizon, targeted facet loads (**`WalkRingFacets`** with **`facetMask`**) without assembling a full **`ContextPack`**.

### `AssembleCellView` alone

- **Why omitted:** **`LoadContextPack`** calls assembly internally; the demo exercises the **budgeted pack** path products use for prompts.
- **Use when:** Hydrating **one** coordinate for a detail pane, preview tooltip, or custom ranking after you already chose coords elsewhere.

### `UpdateFacet` vs `PutFacet`

- **Why omitted:** Seeding uses **`PutFacet`** once (derivation hash matches center raw). **`UpdateFacet`** enforces **derivation discipline** on update — extra setup for a teaching script.
- **Use when:** Production **rotate summary** when raw is unchanged but derived text changes under the same hash rule — **HEXXLA** facet lifecycle (“update only if hash matches”).

### Explicit `PutEdge` / `GetEdge` / `AscendEdgesFrom` (besides `LinkCells`)

- **Why omitted:** **`LinkCells`** is the spec-named sugar for **conversation_turn**-style edges; the demo does not need multiple relation types or edge reads.
- **Use when:** Non-adjacent graphs, multiple **relation types**, answering “what points **from** this cell?” (**`AscendEdgesFrom`**), idempotent edge upserts (**`PutEdge`**).

### `MarkConflict`, `MarkSupersedes`, `ResolveSeam`, `FindSeamsAt`

- **Why omitted:** The demo adds a dedicated supersession phase (Phase 4) that calls **`MarkSupersedes`** and shows **`FilterSuperseded`** in action. **`MarkConflict`** duplicates policy **`PutSeam`** could express. **`ResolveSeam`** is a **follow-up workflow** step. **`FindSeamsAt`** adds **validity** filtering on seams — redundant when seam validity is open/default.
- **Use when:** **HEXXLA** contradiction UX — quick conflict stub (**`MarkConflict`**), supersede a stale cell (**`MarkSupersedes`**), operator/LM resolution (**`ResolveSeam`**), replay "what contradictions existed **as of** time T?" (**`FindSeamsAt`**).

### `AscendSeamsBySource`, `AscendSeamsInTimeBucket`

- **Why omitted:** Demo proves **cell** tag/source indexes; seam secondaries mirror the same pattern for **dashboards on seam provenance/time**.
- **Use when:** “All seams attributed to **session/assistant**” or “seams in this **week bucket**” without scanning cells.

### `FilterCellViews`, `TruncateCellViewsToTokenBudget`

- **Why omitted:** **`LoadContextPack`** already applies token eviction; extra filters are **product policy** (redact, drop role X).
- **Use when:** Post-processing **`CellView`** slices from **custom** assembly (e.g. after **`AssembleCellView`** in a loop) or trimming after manual merges.

### `DB.ViewAt(read_seq)`

- **Why omitted:** **`ViewAtTime`** matches “as of wall clock” product stories; pinning **exact commit sequence** is for **reproducibility** and **tests**.
- **Use when:** Bit-for-bit replay of a known snapshot, diff tooling, correlating with **`CommitSeq`** from **`StatsMVCC`** / WAL forensics.

### `Batch`

- **Why omitted:** Identical to **`Update`**; no extra behavior to demonstrate.
- **Use when:** Call-site clarity only.

### Changelog: `ReadChangelogSince`

- **Why omitted:** **`ReadChangelogFiltered`** is shown in the demo; the unfiltered **`ReadChangelogSince`** variant is a simpler subset.
- **Use when:** Bulk sequential replay without op-type filtering.

### MVCC: `StatsMVCC`, `GroupWALStats`, `SuggestedPruneBeforeSeq`, `MVCCPrunePlan`, `PruneCellVersions*`, `PruneScheduler`

- **Why omitted:** Long-running disk retention; demo uses default file or optional **`-mvcc`** without filling history so far that pruning matters.
- **Use when:** Production **disk** and **latency** governance — **not** prompt assembly.

### Encryption: `DeriveKeyFromPassphrase`, `RotateEncryption*`

- **Why omitted:** Demo defaults to plaintext under **`.tmp/`**; encryption is **[ENCRYPTION.md](./ENCRYPTION.md)** deployment mode.
- **Use when:** **At-rest** keys, **rotation** without exposing plaintext on disk.

---

## Related documents

| Doc                                  | Purpose                                                        |
| ------------------------------------ | -------------------------------------------------------------- |
| **[HEXXLA_DB.md](./HEXXLA_DB.md)**   | Keyspace, record families, indexes.                            |
| **[TX.md](./TX.md)**                 | Locking, MVCC snapshot + temporal semantics, validity filters. |
| **[HEXXLA.md](./HEXXLA.md)**         | Hexxla memory model + library mapping.                         |
| **[OPERATIONS.md](./OPERATIONS.md)** | Embedding, backups, MVCC retention/prune, incident response.   |
| **[DURABILITY.md](./DURABILITY.md)** | WAL, group commit, durability barriers.                        |
| **[ENCRYPTION.md](./ENCRYPTION.md)** | Threat model and key options.                                  |
| **[CHANGEFEED.md](./CHANGEFEED.md)** | Logical changelog semantics.                                   |
| **[../ROADMAP.md](../ROADMAP.md)**   | Non-goals, spec-vs-code backlog.                               |

---

## Maintenance

When adding exports to **`package hexxladb`**, update this file and the **[README API table](../../README.md)** so **two** navigable inventories stay aligned (`README` remains the contributor landing page; this file is the exhaustive checklist).
