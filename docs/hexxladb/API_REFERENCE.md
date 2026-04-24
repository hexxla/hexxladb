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

| Symbol                                            | Notes                                                    |
| ------------------------------------------------- | -------------------------------------------------------- |
| **[`(*Tx).PutCell`](../../primitives.go)**        | Cell primary + secondaries (`source/`, `time/`, `tag/`). |
| **[`(*Tx).GetCell`](../../primitives.go)**        | Decode visible cell at packed coord.                     |
| **[`(*Tx).WalkRing`](../../primitives.go)**       | Visit one ring; raw bytes per coord.                     |
| **[`(*Tx).WalkRingAt`](../../primitives.go)**     | Same order; **`record.ValidAt`** filter at **`asOf`**.   |
| **[`(*Tx).LoadContext`](../../primitives.go)**    | Concentric walk; **`maxR`**, **`maxCells`**.             |
| **[`(*Tx).LoadContextAt`](../../primitives.go)**  | Same as **`LoadContext`** + validity filter.             |
| **[`(*Tx).WalkRingFacets`](../../primitives.go)** | Facet_mask ring walk; optional validity on cell.         |
| **[`(*Tx).PutSeam`](../../primitives.go)**        | Seam primary + **`seam-by-cells/`** + seam secondaries.  |
| **[`(*Tx).FindSeams`](../../primitives.go)**      | Query ball using seam index + primaries.                 |
| **[`(*Tx).FindSeamsAt`](../../primitives.go)**    | **`FindSeams`** + seam validity filter.                  |
| **[`(*Tx).MarkConflict`](../../primitives.go)**   | Spec sugar: ULID seam **`mark_conflict`**.               |
| **[`(*Tx).ResolveSeam`](../../primitives.go)**    | Update resolution fields on **`seam/<ulid>`**.           |

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

| Symbol                                                                                                                                                                      | Notes                                              |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| **[`(*Tx).AssembleCellView`](../../views.go)**                                                                                                                              | One coord → **`CellView`** with opts.              |
| **[`(*Tx).LoadContextWithBudgeting`](../../views.go)**                                                                                                                      | Token-budget **`ContextPack`**.                    |
| **[`(*Tx).LoadContextPack`](../../views.go)**                                                                                                                               | Alias of **`LoadContextWithBudgeting`**.           |
| **[`CellView`](../../views.go)**, **[`ContextPack`](../../views.go)**, **[`FacetView`](../../views.go)**, **[`EdgeView`](../../views.go)**, **[`SeamRef`](../../views.go)** | View types.                                        |
| **[`LoadContextBudgetConfig`](../../views.go)**, **[`AssembleCellViewOpts`](../../views.go)**, **[`DefaultAssembleCellViewOpts`](../../views.go)**                          | Assembly + seam radius + caps.                     |
| **[`TokenBudgeter`](../../views.go)**, **[`ByteLenBudgeter`](../../views.go)**                                                                                              | Budget counting.                                   |
| **[`CellViewPredicate`](../../views.go)**, **[`FilterCellViews`](../../views.go)**, **[`TruncateCellViewsToTokenBudget`](../../views.go)**                                  | Post-process assembled views.                      |
| **[`ContextPackStats`](../../views.go)**                                                                                                                                    | Assembly stats: candidates, evicted, max ring.     |
| **[`CellExplanation`](../../views.go)**                                                                                                                                     | Per-cell inclusion/eviction reason (Explain mode). |

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

- **Exhaustive public-API walk (ELI5 + real files):** [`examples/full_api_demo`](../../examples/full_api_demo/) — **`go run ./examples/full_api_demo`** seeds **`./.tmp/full_api_demo/`** (MVCC + changelog main file; optional encrypted file) and prints one section per major **`package hexxladb`** capability.
- **Session-shaped teaching demo:** [`examples/live_session_demo`](../../examples/live_session_demo/) — scripted LLM-session cells; smaller output.

## What `examples/live_session_demo` does _not_ call (and why)

That demo is a **single-session smoke test** for Hexxla-shaped writes and reads; it stays readable. Omitted APIs fall into a few buckets: **low-level escape hatches**, **validity-specialized variants**, **seam lifecycle / conflict sugar**, **post-assembly helpers**, **operator features**, **encryption ops**. Use **`full_api_demo`** for breadth; keep **`live_session_demo`** for narrative density.

### Raw btree: `Tx.Get`, `Tx.Put`, `Tx.AscendRange`

- **Why omitted:** HEXXLA and **`live_session_demo`** target **logical** cells/seams/facets/edges. Raw keys bypass **`cell/`** layout and indexes unless you duplicate encodings by hand.
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

- **Why omitted:** One seam is inserted with **`PutSeam`**; **`FindSeams`** is enough for geometry. **`MarkConflict`** duplicates policy **`PutSeam`** could express. **`ResolveSeam`** is a **follow-up workflow** step. **`FindSeamsAt`** adds **validity** filtering on seams — redundant when seam validity is open/default.
- **Use when:** **HEXXLA** contradiction UX — quick conflict stub (**`MarkConflict`**), operator/LM resolution (**`ResolveSeam`**), replay “what contradictions existed **as of** time T?” (**`FindSeamsAt`**).

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

### Changelog: `ReadChangelogSince`, op constants

- **Why omitted:** Requires **`Options.ChangelogEnabled`** and sidecar file; orthogonal to lattice semantics in a single-binary demo.
- **Use when:** Downstream replication, audit, incremental indexers — **service** deployment concern.

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
