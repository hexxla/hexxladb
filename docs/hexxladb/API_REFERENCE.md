# HexxlaDB public API guide

This guide organizes the stable module-root API by task. It is intentionally not a second copy of every exported declaration. Use [`go doc github.com/hexxla/hexxladb`](https://pkg.go.dev/github.com/hexxla/hexxladb) for the exhaustive, source-derived symbol reference.

All application imports use:

```go
import "github.com/hexxla/hexxladb"
```

Packages below `internal/` are private implementation details.

## Open and close

[`Open`](../../db.go) creates or opens one database handle. HexxlaDB permits one open handle per database path across processes; competing opens return [`ErrDatabaseLocked`](../../errors.go).

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC: true,
})
if err != nil {
    return err
}
defer db.Close()
```

[`Options`](../../options.go) controls page and value limits, MVCC, changelog, embeddings, encryption, hooks, and durability tuning. Defaults and supported combinations are documented in [`CONFIGURATION.md`](./CONFIGURATION.md).

## Transactions

All reads and writes occur through [`Tx`](../../tx.go):

| Method                                                                                               | Use                                              |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| [`DB.View`](../../tx.go)                                                                             | Read the latest committed state.                 |
| [`DB.ViewAt`](../../tx.go), [`DB.ViewAtTime`](../../tx.go), [`DB.ViewAtTag`](../../snapshot_tags.go) | Read an MVCC snapshot.                           |
| [`DB.Update`](../../tx.go)                                                                           | Run one write transaction.                       |
| [`DB.Batch`](../../tx.go)                                                                            | Alias for `Update`; useful for call-site intent. |

Callbacks must not mix or nest read and write transactions on the same `DB`. Commit failures match [`ErrCommitFinalization`](../../errors.go); [`ErrCommitDurable`](../../errors.go) additionally identifies a known-durable primary commit whose changefeed recovery requires close and reopen. See [`TX.md`](./TX.md) for locking, snapshots, validity, and failure semantics.

[`Tx.Get`](../../tx.go), [`Tx.Put`](../../tx.go), and [`Tx.AscendRange`](../../tx.go) expose raw byte keys. Prefer the typed lattice operations below so record encodings and secondary indexes remain consistent.

## Coordinates and records

The root package re-exports the stable geometry and wire types applications need:

| Surface                                                                                                      | Purpose                                                       |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------- |
| [`Coord`, `PackedCoord`, `Cube`, `Pack`, `Unpack`, `Ring`, `WalkRings`](../../export.go)                     | Hex coordinates, packing, and deterministic ring enumeration. |
| [`CellRecord`, `SeamRecord`, `ProvenanceWire`, `ValidityWire`](../../export.go)                              | Persistent cell/seam wire representations and metadata.       |
| [`FacetWalkRecord`, `EdgeWalkRecord`](../../export.go)                                                       | Callback record types for facet and edge walks.               |
| [`NewProvenanceWire`, `NewFacetDerived`](../../templates.go)                                                 | Helpers for constructing common records.                      |
| [`NewUserMessageCell`, `NewAssistantResponseCell`, `NewSystemPromptCell`, `NewFactCell`](../../templates.go) | Optional conversational-memory templates.                     |

Coordinate bounds and packing details are documented in [`internal/lattice/PACKED_COORD.md`](../../internal/lattice/PACKED_COORD.md).

HexxlaDB does not choose coordinates or enforce semantic clustering. For deterministic placement, applications can probe a fixed anchor in `Ring`/`WalkRings` order, use `Tx.GetCell` to select the first free coordinate, and perform the occupancy check plus `PutCell` in one update. Do not silently overwrite a collision when the operation means insert. Preserve existing coordinates during incremental insertion; represent an intentional move by creating a successor and calling `MarkSupersedes` rather than deleting or rewriting history. `ClusterHint` is stored metadata only.

The [`lattice_placement_evidence`](../../examples/lattice_placement_evidence) example implements that workflow entirely through the public API and compares semantic and spatial neighborhood quality.

## Cells, facets, edges, and seams

Use these methods inside `View` or `Update` callbacks:

| Task                                    | API                                                                                                           |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Store and retrieve a cell               | [`Tx.PutCell`, `Tx.GetCell`](../../primitives.go)                                                             |
| Delete a cell                           | [`Tx.DeleteCell`, `Tx.DeleteCellWithOutcome`](../../delete_cell.go)                                           |
| Stream a ring or validity-filtered ring | [`Tx.WalkRing`, `Tx.WalkRingAt`, `Tx.WalkRingFacets`](../../primitives.go)                                    |
| Store and retrieve facets               | [`Tx.PutFacet`, `Tx.UpdateFacet`, `Tx.GetFacet`, `Tx.AscendFacetsForCell`](../../facets_edges.go)             |
| Store and retrieve edges                | [`Tx.PutEdge`, `Tx.LinkCells`, `Tx.GetEdge`, `Tx.AscendEdgesFrom`](../../facets_edges.go)                     |
| Create and query seams                  | [`Tx.PutSeam`, `Tx.MarkConflict`, `Tx.MarkSupersedes`, `Tx.FindSeams`, `Tx.FindSeamsAt`](../../primitives.go) |
| Resolve a seam                          | [`Tx.ResolveSeam`](../../primitives.go)                                                                       |

[`DB.BatchPutCells`](../../batch.go) writes large cell collections in bounded transactions with progress and optional per-cell error collection.

Secondary-index scans avoid full cell or seam scans:

- [`Tx.AscendCellsBySource`, `Tx.AscendCellsInTimeBucket`, `Tx.AscendCellsByTag`](../../cell_secondary.go)
- [`Tx.AscendDistinctTags`, `Tx.ListExistingTopics`](../../cell_secondary.go)
- [`Tx.AscendSeamsBySource`, `Tx.AscendSeamsInTimeBucket`](../../seam_secondary.go)

The canonical record families and key encodings are in [`HEXXLA_DB.md`](./HEXXLA_DB.md).

## Context assembly and spatial retrieval

| API                                                                   | Use                                                                                  |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| [`Tx.AssembleCellView`](../../views.go)                               | Hydrate one coordinate into a `CellView`.                                            |
| [`Tx.LoadContext`](../../context_load.go)                             | Assemble token-budgeted context from one or more seeds.                              |
| [`FilterCellViews`, `TruncateCellViewsToTokenBudget`](../../views.go) | Apply product policy to already assembled views.                                     |
| [`Tx.LoadContextFOV`](../../fov_context.go)                           | Deterministic visibility-filtered loading with an application-supplied opacity rule. |
| [`Tx.LoadContextVoronoi`](../../voronoi_context.go)                   | Partition cells among multiple seeds.                                                |
| [`Tx.FindEdgePath`](../../pathfind_api.go)                            | Dijkstra shortest path over stored weighted edges.                                   |
| [`Tx.WalkEdges`](../../pathfind_api.go)                               | Bounded breadth-first traversal over stored edges.                                   |

[`LoadContextConfig`](../../context_load.go) controls seeds, ring bounds, validity time, edge expansion, token budget, and assembly. `ViewAt` snapshot time and record validity time are independent; see [`TX.md`](./TX.md).

## Query and content search

[`Tx.QueryCells`](../../query_exec.go) executes structured [`CellQuery`](../../query.go) filters and uses an applicable secondary index when possible. [`Tx.SearchCells`](../../search.go) provides ranked lexical, substring, tag, source, temporal, and optional embedding-assisted retrieval.

Typical retrieval flow:

```text
SearchCells or QueryCells
  -> choose one or more Coord seeds
  -> LoadContext, LoadContextFOV, or LoadContextVoronoi
  -> ContextPack
```

Use [`RenderHexGrid`](../../hex_render.go) for bounded diagnostic rendering. It is a presentation helper, not a query primitive.

## Embeddings

Embeddings are optional. Configure a dimension and distance metric with [`Options`](../../options.go), or allow the first write to establish the dimension.

| API                                                                                 | Use                                                                                       |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [`Tx.PutEmbedding`, `Tx.GetEmbedding`, `Tx.DeleteEmbedding`](../../tx_embedding.go) | Manage one vector per cell.                                                               |
| [`Tx.SearchByEmbedding`](../../embedding_search.go)                                 | HNSW-assisted nearest-neighbor search with flat-scan fallback.                            |
| [`Tx.SearchByEmbeddingWithStats`](../../embedding_search.go)                        | The same search plus the selected HNSW/flat path and effective HNSW query breadth.        |
| [`Tx.ReindexEmbeddings`](../../embedding_reindex.go)                                | Recompute stored vectors in a transaction.                                                |

`EmbeddingSearchConfig.EfSearch` trades HNSW latency and allocation for recall. Zero uses a bounded dimension-aware default; explicit values from 1 through 10,000 are accepted and are raised to at least `MaxResults`. `EmbeddingSearchStats` exposes the effective value. `CellQuery.Embedding` and `CellSearchConfig.Embedding` integrate vector similarity into the query and search paths. HexxlaDB remains usable without embeddings through explicit coordinates and indexed metadata.

## Derived super-hex occupancy

[`NewSuperHexSummaryIndex`](../../superhex_summary.go) creates a rebuildable, process-local aperture-7 occupancy index. Call `Rebuild` from a consistent snapshot, then `Sync` from the logical changelog. It is not persisted and contains occupancy statistics rather than cell content.

Operational setup and evidence gates are documented in [`OPERATIONS.md`](./OPERATIONS.md) and [`PERFORMANCE_EVIDENCE.md`](./PERFORMANCE_EVIDENCE.md).

## MVCC lifecycle and snapshots

MVCC is enabled only when creating a new database with `Options.EnableMVCC`.

| API                                                                                | Use                                                   |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------- |
| [`DB.StatsMVCC`, `DB.SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go)            | Inspect history and derive a retention watermark.     |
| [`DB.PruneCellVersions`, `DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go) | Remove eligible old versions in bounded passes.       |
| [`PruneScheduler`](../../mvcc_lifecycle.go)                                        | Drive pruning from an application-owned timer.        |
| [`DB.TagSnapshot`, `DB.ViewAtTag`, `DB.DeleteSnapshotTag`](../../snapshot_tags.go) | Name and revisit commit snapshots.                    |
| [`DB.SnapshotDiff`](../../snapshot_diff.go)                                        | Compare cell and seam writes across commit sequences. |

Pruning does not shrink the primary file. Use compaction after pruning when disk reclamation is required.

## Changefeed, health, and maintenance

| API                                                                          | Use                                                    |
| ---------------------------------------------------------------------------- | ------------------------------------------------------ |
| [`DB.ReadChangelogSince`, `DB.ReadChangelogFiltered`](../../db_changelog.go) | Read the optional at-least-once logical changefeed.             |
| [Durable consumer cursor methods](../../changelog_consumers.go)              | Register, compare-and-advance, inspect, and delete named cursors. |
| [`DB.HealthCheck`](../../health.go)                                          | Validate database structure and report counts.                 |
| [`DB.GroupWALStats`](../../db.go)                                            | Observe group-WAL batching.                                    |
| [`DB.WriteStats`](../../write_stats.go)                                      | Observe cumulative write contention and phase timing.         |
| [`DB.BackupTo`](../../backup.go)                                             | Capture a consistent primary/WAL/changelog recovery set.       |
| [`DB.StorageStats`](../../storage_stats.go)                                  | Measure physical, reachable, and reclaimable storage.         |
| [`DB.Compact`, `CompactTo`](../../compact.go)                                | Rewrite live keys into a new compact file.                     |
| [`DB.CompactWithOptions`, `CompactToWithOptions`](../../compact.go)          | Bound copy batches and receive durable progress checkpoints.  |
| [`DeriveKeyFromPassphrase`](../../encryption.go)                             | Derive an encryption key using the database KDF.               |
| [`RotateEncryption`, `RotateEncryptionWithOptions`](../../rotation.go)       | Perform offline key rotation or encryption migration.          |

See [`CHANGEFEED.md`](./CHANGEFEED.md), [`OPERATIONS.md`](./OPERATIONS.md), [`DURABILITY.md`](./DURABILITY.md), and [`ENCRYPTION.md`](./ENCRYPTION.md) before enabling these deployment features.

## Validation and hooks

[`CellValidator`](../../hooks.go) validates cells before persistence. [`AfterPutCellHook`](../../hooks.go) and [`AfterPutSeamHook`](../../hooks.go) run synchronously after their corresponding logical writes. Function adapters are provided for all three interfaces, and hooks are configured through [`Options`](../../options.go).

Hook errors are returned to the transaction callback. They do not provide an independent transactional boundary; callers must follow the commit-finalization guidance in [`TX.md`](./TX.md).

## Errors

Public sentinel errors are defined in [`errors.go`](../../errors.go). Handle stable error semantics with `errors.Is` and `errors.As`, including:

- lifecycle and locking errors such as `ErrDatabaseClosed` and `ErrDatabaseLocked`;
- argument and transaction errors such as `ErrInvalidArgument` and `ErrTxReadOnly`;
- MVCC errors such as `ErrReadSeqFuture` and `ErrMVCCRequired`;
- encryption and changelog errors documented in their focused references;
- `ErrCommitFinalization`, which requires recovery before another write, and `ErrCommitDurable`, which specifically means the authoritative commit is known durable and must not be retried.

Do not compare error strings.

## Examples

- [`examples/conversational_memory`](../../examples/conversational_memory) — cell lifecycle, MVCC, context assembly, hooks, health, diff, compaction, FOV, and graph traversal.
- [`examples/spatial_algorithms`](../../examples/spatial_algorithms) — FOV, LOD, Voronoi, Dijkstra, BFS, and graph-aware context.
- [`examples/llm_context_engine`](../../examples/llm_context_engine) — embedding-backed retrieval and prompt assembly; requires Ollama with `all-minilm`.
- [`examples/performance_evidence`](../../examples/performance_evidence) — controlled evidence collection for spatial algorithms and super-hex occupancy.
- [`examples/vector_scale_evidence`](../../examples/vector_scale_evidence) — bounded HNSW build, recall, reopen, churn, memory, and disk evidence.
- [`examples/lattice_placement_evidence`](../../examples/lattice_placement_evidence) — deterministic placement, collision, incremental-stability, supersession, and semantic/lattice divergence evidence.

Examples demonstrate workflows; they are not expected to call every exported symbol.

## Related documents

| Document                                 | Owns                                          |
| ---------------------------------------- | --------------------------------------------- |
| [`CONFIGURATION.md`](./CONFIGURATION.md) | Options and common configurations             |
| [`TX.md`](./TX.md)                       | Transaction, snapshot, and validity semantics |
| [`HEXXLA_DB.md`](./HEXXLA_DB.md)         | Storage layout and key families               |
| [`HEXXLA.md`](./HEXXLA.md)               | Product memory concepts                       |
| [`OPERATIONS.md`](./OPERATIONS.md)       | Backup, retention, compaction, and recovery   |
