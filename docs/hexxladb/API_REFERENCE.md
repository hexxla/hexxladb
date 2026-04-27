# HexxlaDB API reference (full inventory)

**Audience:** Anyone integrating **`package hexxladb`** (`github.com/hexxla/hexxladb`).
**Normative storage:** **[HEXXLA_DB.md](./HEXXLA_DB.md)**. Transactions and snapshots: **[TX.md](./TX.md)**. Memory model: **[HEXXLA.md](./HEXXLA.md)**.

This document lists **every exported symbol** in the root package as of the current tree, grouped by role. Use it as the single checklist when auditing coverage (tests, demos, product adapters). Generated docs: [pkg.go.dev](https://pkg.go.dev/github.com/hexxla/hexxladb); the **[`doc.go`](../../doc.go)** package comment stays the short overview.

---

## Storage limits (engine B+ tree)

Opaque keys and values passed through **[`(*Tx).Put`](../../tx.go)** are stored in the engine B+ tree. **Maximum key length: 256 bytes.** **Maximum value length: configurable per-database** via [`Options.MaxValueBytes`](../../options.go) — accepted values: **512, 1024, 2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576** bytes; **default 8192 (8 KB)**. Values exceeding the page's inline threshold (~3.7 KiB at 4 KiB page size) are stored in **overflow pages** automatically. The limit is persisted in the file header and enforced on every write. Read it at runtime with **[`(*DB).MaxValueBytes`](../../db.go)**. **Page size: configurable per-database** via [`Options.PageSize`](../../options.go) — accepted values: **4096, 8192, 16384, 65536** bytes; **default 4096 (4 KiB)**. Existing databases read the page size from the file header on open. Read it at runtime with **[`(*DB).PageSize`](../../db.go)**. B+ tree leaf capacity is dynamic (fill-based splitting); smaller pages reduce wasted space for small databases. Rationale and format details: **[`ORDERED_STORE.md`](../../internal/engine/ORDERED_STORE.md)**. **Cell** (and other encoded) records must fit in the value budget; larger logical payloads require application-level chunking or external blob storage. **Compression: configurable per-database** via [`Options.Compression`](../../options.go) — **`CompressionNone`** (default) or **`CompressionDeflate`** (DEFLATE via `compress/flate`, Go standard library). Persisted in the file header. Compressed and uncompressed values coexist transparently. Read at runtime with **[`(*DB).Compression`](../../db.go)**.

---

## Database lifecycle

| Symbol                                   | Notes                                                                                                                                                    |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`Open`](../../db.go)**                | Open or create a database file; applies WAL on startup.                                                                                                  |
| **[`(*DB).Close`](../../db.go)**         | Waits for in-flight transactions; idempotent for nil receiver.                                                                                           |
| **[`(*DB).Compact`](../../compact.go)**  | Copy-compact open DB to destPath (minimal-size file, preserves all data including MVCC history).                                                         |
| **[`CompactTo`](../../compact.go)**      | Standalone copy-compaction from srcPath to destPath; propagates format, encryption, MaxValueBytes.                                                       |
| **[`ErrCorruptDatabase`](../../db.go)**  | Open-time corruption (header/WAL).                                                                                                                       |
| **[`Options`](../../options.go)**        | `EnableMVCC`, `MVCCRetention`, changelog, encryption, `CellValidator`, `AfterPutCell`, `AfterPutSeam`, `PageSize`, `MaxValueBytes`, optional page hooks. |
| **[`(*DB).PageSize`](../../db.go)**      | Returns the active page size (bytes); 4096 default for new databases.                                                                                    |
| **[`(*DB).MaxValueBytes`](../../db.go)** | Returns the effective per-database max value size (bytes) from the file header.                                                                          |
| **[`MVCCRetention`](../../options.go)**  | Retention hint for prune suggestions.                                                                                                                    |

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
| **[`(*Tx).DeleteCell`](../../delete_cell.go)**    | Remove cell + secondaries + facets + outbound edges. Idempotent. MVCC: tombstone; seams NOT removed.              |
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

## Snapshot Tags

Human-friendly names for MVCC commit sequences. Tags are stored in the B+ tree under `__meta/snap-tag/<label>` and survive database close/reopen.

| Symbol                                                  | Notes                                                                                                                                        |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*DB).TagSnapshot`](../../snapshot_tags.go)**       | Pin the current head `CommitSeq` under `label`. Overwrites an existing tag with the same name. Label: non-empty, ≤ 200 bytes.                |
| **[`(*DB).ViewAtTag`](../../snapshot_tags.go)**         | Open a read-only snapshot pinned to the `CommitSeq` recorded by `TagSnapshot`. Returns **`ErrSnapshotTagNotFound`** if label does not exist. |
| **[`(*DB).ListSnapshotTags`](../../snapshot_tags.go)**  | Return all tags as `[]SnapshotTag` sorted by label.                                                                                          |
| **[`(*DB).DeleteSnapshotTag`](../../snapshot_tags.go)** | Remove a tag entry. Returns **`ErrSnapshotTagNotFound`** if absent. Does not affect the underlying data.                                     |
| **[`SnapshotTag`](../../snapshot_tags.go)**             | `Label string`, `CommitSeq uint64`.                                                                                                          |
| **[`ErrSnapshotTagNotFound`](../../errors.go)**         | Returned by `ViewAtTag` / `DeleteSnapshotTag` when the label has no entry.                                                                   |
| **[`ErrSnapshotTagLabelTooLong`](../../errors.go)**     | Returned by `TagSnapshot` when `len(label) > 200`.                                                                                           |

---

## Logical changefeed

| Symbol                                                                                                                                                             | Notes                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------- |
| **[`(*DB).ReadChangelogSince`](../../db_changelog.go)**                                                                                                            | Requires **`Options.ChangelogEnabled`**.                 |
| **[`(*DB).ReadChangelogFiltered`](../../db_changelog.go)**                                                                                                         | Filtered read by op codes and/or key prefix.             |
| **[`ChangelogFilter`](../../db_changelog.go)**                                                                                                                     | Filter config: **`Ops []byte`**, **`KeyPrefix []byte`**. |
| **[`ChangelogRecord`](../../db_changelog.go)**                                                                                                                     | Typed alias of internal record.                          |
| **`ChangelogOpPutCell`**, **`ChangelogOpPutSeam`**, **`ChangelogOpResolveSeam`**, **`ChangelogOpPutFacet`**, **`ChangelogOpPutEdge`**, **`ChangelogOpDeleteCell`** | Stable op codes.                                         |

See **[CHANGEFEED.md](./CHANGEFEED.md)**.

---

## Database health check

| Symbol                                            | Notes                                                                                                                                                                       |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*DB).HealthCheck`](../../health.go)**        | Integrity scan: cell count, seam resolution summary, orphaned seam detection, index consistency, MVCC stats.                                                                |
| **[`HealthReport`](../../health.go)**             | Result type: `CellCount`, `SeamCount`, `SeamsResolved`, `SeamsUnresolved`, `OrphanedSeams`, `TagIndexErrors`, `SourceIndexErrors`, `MVCCStats`, `Warnings`.                 |
| **[`HealthCheckConfig`](../../health.go)**        | `CheckOrphans`, `CheckTagIndex`, `CheckSourceIndex`, `MaxErrors`. `ScanRadius` — deprecated, retained for backward compat, has no effect (cell scan now covers all coords). |
| **[`DefaultHealthCheckConfig`](../../health.go)** | Returns config with all checks enabled.                                                                                                                                     |

---

## Composable Query Engine

`Tx.QueryCells` is the unified query entry point. All predicate fields are AND-combined; zero/empty values are ignored.

| Symbol                                        | Notes                                                                                                                                                                                                                                   |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **[`(*Tx).QueryCells`](../../query_exec.go)** | Execute a `CellQuery` against the snapshot. Planner picks cheapest index; remaining predicates applied in-memory.                                                                                                                       |
| **[`CellQuery`](../../query.go)**             | Predicate: `Query`, `RequireTags` (AND), `AnyTags` (OR), `ExcludeTags` (NOT), `SourceID`, `MinConfidence`, `MaxConfidence`, `After`/`Before` (temporal), `Center`+`Radius` (spatial), `MaxResults`, `MaxScanRows`, `SortBy`, `Explain`. |
| **[`CellQueryResult`](../../query.go)**       | `Cell CellView`, `Score float64`, `Explanation string` (when `Explain=true`).                                                                                                                                                           |
| **[`SortOrder`](../../query.go)**             | `SortByScore` (default), `SortByConfidence`, `SortByRecency`, `SortByCoord`.                                                                                                                                                            |

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

## MVCC Snapshot Diff

Compare database state between two commit sequences. Requires MVCC (format v2). Returns all cell and seam writes in the half-open range `(fromSeq, toSeq]`. Useful for incremental replication, CDC pipelines, and audit trails.

| Symbol                                             | Notes                                                                                                                     |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| **[`(*DB).SnapshotDiff`](../../snapshot_diff.go)** | Returns a [SnapshotDiff] with all cell/seam writes in `(fromSeq, toSeq]`. Requires MVCC; **`ErrMVCCRequired`** otherwise. |
| **[`SnapshotDiff`](../../snapshot_diff.go)**       | `FromSeq`, `ToSeq uint64`; `Cells []CellDiff`; `Seams []SeamDiff`.                                                        |
| **[`CellDiff`](../../snapshot_diff.go)**           | `Coord`, `CommitSeq`, `Op DiffOp`, `Record record.CellRecord`.                                                            |
| **[`SeamDiff`](../../snapshot_diff.go)**           | `ID`, `CommitSeq`, `Op DiffOp`, `Record record.SeamRecord`.                                                               |
| **[`DiffOp`](../../snapshot_diff.go)**             | String kind constant; currently only **`DiffOpPut`**.                                                                     |
| **[`SnapshotDiffConfig`](../../snapshot_diff.go)** | `IncludeCells *bool`, `IncludeSeams *bool` — omit nil to include both.                                                    |
| **[`ErrMVCCRequired`](../../errors.go)**           | Returned when `SnapshotDiff` is called on a format-v1 (non-MVCC) database.                                                |

---

## Event Hooks

Post-write callbacks invoked synchronously inside the `Update` callback, after the write succeeds. A non-nil error is returned from the triggering write method. Set on `Open` via `Options`.

| Symbol                                         | Notes                                                                                                            |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| **[`AfterPutCellHook`](../../hooks.go)**       | Interface: **`AfterPutCell(ctx, CellRecord) error`** — called after each successful `PutCell`.                   |
| **[`AfterPutCellHookFunc`](../../hooks.go)**   | Adapter: plain function → **`AfterPutCellHook`**.                                                                |
| **[`AfterPutSeamHook`](../../hooks.go)**       | Interface: **`AfterPutSeam(ctx, SeamRecord) error`** — called after `PutSeam`, `MarkConflict`, `MarkSupersedes`. |
| **[`AfterPutSeamHookFunc`](../../hooks.go)**   | Adapter: plain function → **`AfterPutSeamHook`**.                                                                |
| **[`Options.AfterPutCell`](../../options.go)** | Wire a cell hook at `Open`.                                                                                      |
| **[`Options.AfterPutSeam`](../../options.go)** | Wire a seam hook at `Open`.                                                                                      |

---

## Sentinel errors (complete)

| Variable                                                                                                                  | When                                                    |
| ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| **`ErrNotImplemented`**                                                                                                   | Stub API.                                               |
| **`ErrSeamNotFound`**                                                                                                     | Missing **`seam/<ulid>`**.                              |
| **`ErrSeamEndpointMismatch`**                                                                                             | Immutable endpoints for ULID.                           |
| **`ErrInvalidArgument`**                                                                                                  | Bad parameter.                                          |
| **`ErrClosed`**                                                                                                           | Closed handle.                                          |
| **`ErrDatabaseClosed`**                                                                                                   | **`DB`** closed.                                        |
| **`ErrTxReadOnly`**                                                                                                       | Write in **`View`**.                                    |
| **`ErrNilCallback`**                                                                                                      | Nil **`View`/`Update`** fn.                             |
| **`ErrEncryptionKeyRequired`**, **`ErrDatabaseNotEncrypted`**, **`ErrEncryptionOptions`**, **`ErrEncryptionKeyMismatch`** | Encryption options vs file.                             |
| **`ErrCellNotFound`**                                                                                                     | e.g. **`UpdateFacet`** without cell.                    |
| **`ErrFacetDerivationMismatch`**                                                                                          | Facet hash vs raw.                                      |
| **`ErrChangelogDisabled`**, **`ErrChangelogCorrupt`**                                                                     | Changefeed.                                             |
| **`ErrReadSeqFuture`**                                                                                                    | **`ViewAt`** / **`SnapshotDiff`** seq too new.          |
| **`ErrCommitFinalization`**                                                                                               | Post-callback failure.                                  |
| **`ErrMVCCRequired`**                                                                                                     | MVCC-only op on v1 database.                            |
| **`ErrSnapshotTagNotFound`**                                                                                              | **`ViewAtTag`** / **`DeleteSnapshotTag`** label absent. |
| **`ErrSnapshotTagLabelTooLong`**                                                                                          | Label > 200 bytes in **`TagSnapshot`**.                 |
| **`ErrCorruptDatabase`**                                                                                                  | Open failure.                                           |

Use **`errors.Is` / `errors.As`** for stable handling.

---

## Live demos and coverage

- **Conversational memory service:** [`examples/conversational_memory`](../../examples/conversational_memory/) — **`go run ./examples/conversational_memory`** seeds **`./.tmp/conversational_memory/`** (MVCC + changelog + `AfterPutCell` hook) and walks through: cell storage (templates + batch), supersession, tag analytics + co-occurrences, query patterns (`QueryCells` + `SearchCells`), MVCC time-travel, ASCII grid + ring density, filtered changelog, multi-seed context assembly, **database health check** (`HealthCheck`), **event hook telemetry** (`AfterPutCell`), **MaxValueBytes** (per-database limit printed at startup), **MVCC Snapshot Diff** (`SnapshotDiff` full range + narrow diff), **`Tx.DeleteCell`** (MVCC tombstone + snapshot isolation + idempotent re-delete), and **`DB.Compact`** (bulk write→delete→prune→compact with file size reduction). Phases 1–12.

## What `examples/conversational_memory` does _not_ call (and why)

The demo is a **session-shaped production walkthrough**; it stays readable. Omitted APIs fall into a few buckets: **low-level escape hatches**, **validity-specialized variants**, **seam lifecycle / conflict sugar**, **post-assembly helpers**, **operator features**, **encryption ops**.

> **Demonstrated in Phase 11 (added):** `DB.HealthCheck` + `HealthReport`, `DB.SnapshotDiff` + `SnapshotDiff`/`CellDiff`/`SeamDiff`/`SnapshotDiffConfig`, `AfterPutCellHook`/`AfterPutCellHookFunc` (wired in `Options.AfterPutCell`), `DB.MaxValueBytes()` (printed at startup). These were previously in this omissions list.
>
> **Demonstrated in Phase 12 (added):** `Tx.DeleteCell` (MVCC tombstone, `ViewAt` snapshot isolation, idempotent re-delete, `HealthCheck` cell count drop), `DB.Compact` (bulk write→delete→prune→compact with file size reduction, compacted DB health check).

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

### `MarkConflict`, `ResolveSeam`, `FindSeamsAt`

- **Why omitted:** `MarkSupersedes` and `MarkConflict` are both demonstrated (Phase 3+4). **`ResolveSeam`** is a **follow-up workflow** step. **`FindSeamsAt`** adds **validity** filtering on seams — redundant when seam validity is open/default.
- **Use when:** Operator/LM resolution (**`ResolveSeam`**); replay "what contradictions existed **as of** time T?" (**`FindSeamsAt`**).

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

### `DB.HealthCheck` / `AfterPutCellHook` / `AfterPutSeamHook` / `DB.MaxValueBytes` / `DB.SnapshotDiff`

Now demonstrated in **Phase 11**. See above.

### `Tx.DeleteCell` / `DB.Compact` / `CompactTo`

Now demonstrated in **Phase 12**. See above.

### MVCC: `StatsMVCC`, `GroupWALStats`, `SuggestedPruneBeforeSeq`, `MVCCPrunePlan`, `PruneCellVersions*`, `PruneScheduler`

- **Why omitted:** Long-running disk retention; demo uses default file without filling history so far that pruning matters.
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
