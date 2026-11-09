# HexxlaDB public API guide

This guide organizes the module-root API by task. During pre-v1 development,
the API stability inventory in [`VERSIONING.md`](../../VERSIONING.md#root-api-stability-inventory)
is authoritative. Run `go doc -all github.com/hexxla/hexxladb` from the checkout for the full exported reference.

All application imports use:

```go
import "github.com/hexxla/hexxladb"
```

Packages below `internal/` are private implementation details.

## Open and close

[`Open`](../../db.go) creates or opens one database handle. HexxlaDB permits one open handle per database path across processes; competing opens return [`ErrDatabaseLocked`](../../errors.go).
Open validates pending WAL records and recovers the commit before handing
control to callers. An invalid WAL, including older pending v2 redo without a
commit header, returns `ErrCorruptDatabase`. Use [`OpenExisting`](../../db.go)
when a missing or empty primary must be rejected. It still takes the exclusive
lock and can replay the WAL and create sidecars. See [recovery compatibility](./DURABILITY.md#recovery-and-version-compatibility).

```go
db, err := hexxladb.Open("memory.db", nil)
if err != nil {
    return err
}
defer db.Close()
```

[`Options`](../../options.go) controls page and value limits, prune suggestions, changelog, embeddings, encryption, hooks, and durability tuning. Both storage profiles use MVCC. Defaults and supported combinations are documented in [`CONFIGURATION.md`](./CONFIGURATION.md).

`DB.Close` attempts every cleanup step. A single failure is returned directly.
Simultaneous failures are joined and remain inspectable with `errors.Is` and
`errors.As`.

## Transactions

All reads and writes occur through [`Tx`](../../tx.go):

| Method                                                                                               | Use                                              |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| [`DB.View`](../../tx.go)                                                                             | Read the latest committed state.                 |
| [`DB.ViewAt`](../../tx.go), [`DB.ViewAtTime`](../../tx.go), [`DB.ViewAtTag`](../../snapshot_tags.go) | Read an MVCC snapshot.                           |
| [`DB.Update`](../../tx.go)                                                                           | Run one write transaction.                       |

Use the supplied `Tx` for work inside a callback. Put related writes in the
same `DB.Update` to share a synchronous durability barrier. A transaction that
exceeds the WAL size limit returns `ErrInvalidArgument` before writing the WAL.
For an uncertain commit outcome, close and reopen before retrying.
`ErrCommitDurable` identifies a known-durable primary commit that requires
recovery. See [transaction failures](./TX.md#commit-finalization-failures).

For historical graph or vector requests, use a separate `DB.View` if current data is acceptable.
See [snapshot semantics](./TX.md#mvcc-temporal-semantics).

## Coordinates and records

The root package re-exports the stable geometry and wire types applications need:

| Surface                                                                                                      | Purpose                                                       |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------- |
| [`Coord`, `PackedCoord`, `Cube`, `Pack`, `Unpack`, `Ring`, `WalkRings`](../../export.go)                     | Hex coordinates, packing, and deterministic ring enumeration. |
| [`CellRecord`, `SeamRecord`, `ProvenanceWire`, `ValidityWire`](../../export.go)                              | Persistent cell/seam wire representations and metadata.       |
| [`CellPlacement`, `MaxCellPlacementRadius`](../../cells.go)                                             | Bounded deterministic free-coordinate selection result/limit. |
| [`FacetWalkRecord`, `EdgeWalkRecord`](../../export.go)                                                       | Callback record types for facet and edge walks.               |
| [`NewProvenanceWire`, `NewFacetDerived`](../../templates.go)                                                 | Helpers for constructing common records.                      |
| [`NewUserMessageCell`, `NewAssistantResponseCell`, `NewSystemPromptCell`, `NewFactCell`](../../templates.go) | Optional conversational-memory templates.                     |

Coordinate bounds and packing details are documented in [`internal/lattice/PACKED_COORD.md`](../../internal/lattice/PACKED_COORD.md).

Typed writes require every `PackedCoord` key, endpoint, and cluster hint to
round-trip through `Unpack`; construct them with `Pack`. Provenance confidence,
edge weight, and seam confidence delta must be finite. Confidence remains an
application-defined signal and is not restricted to a database-defined range.
Violations return `ErrInvalidArgument` before serialization or index updates.

HexxlaDB does not infer semantic anchors or enforce clustering. [`Tx.FindFreeCellPlacement`](../../cells.go) searches from a caller-supplied anchor in deterministic ring order and returns the first coordinate without a visible cell, its packed key, and the number of occupied positions probed. Call it inside the same `DB.Update` as `PutCell`; the writable transaction prevents another writer from claiming the result, while `PutCell` remains the explicit canonical write. The helper does not reserve a result from repeated calls in the same transaction, so write the returned key before searching again. It accepts radii from zero through `MaxCellPlacementRadius`, requires the complete search disk to fit within the packable coordinate range, and returns `ErrNoFreeCellPlacement` on bounded exhaustion. A current MVCC tombstone is free for placement; writing there creates a new version while retained snapshots continue to expose the earlier cell.

Use [`Tx.FindUnusedCellPlacement`](../../cells.go) for a new logical identity
when inbound edges or seams may still reference deleted cells. It skips recorded
tombstones, including deletions staged in the current update. `FindFreeCellPlacement`
can reuse those addresses. For histories
predating MVCC migration, keep an application-owned allocation map if addresses
must never be reused. The allocator checks cell history. The application owns
references to coordinates that never contained a cell.

Preserve existing coordinates during incremental insertion; represent an intentional move by creating a successor and calling `MarkSupersedes` rather than deleting or rewriting history. `ClusterHint` is stored metadata only. Applications continue to own anchor selection, semantic grouping, relocation policy, and placement-quality thresholds.

The [`lattice_placement_evidence`](../../tools/evidence/lattice_placement_evidence) runner implements that workflow entirely through the public API and compares semantic and spatial neighborhood quality.

## Cells, facets, edges, and seams

Use these methods inside `View` or `Update` callbacks:

| Task                                    | API                                                                                                           |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| Select a bounded cell placement         | [`Tx.FindUnusedCellPlacement`, `Tx.FindFreeCellPlacement`](../../cells.go)                                                            |
| Store and retrieve a cell               | [`Tx.PutCell`, `Tx.GetCell`](../../cells.go)                                                             |
| Delete a cell                           | [`Tx.DeleteCell`, `Tx.DeleteCellWithOutcome`](../../delete_cell.go)                                           |
| Stream a ring or validity-filtered ring | [`Tx.WalkRing`, `Tx.WalkRingAt`, `Tx.WalkRingFacets`](../../spatial_reads.go)                                    |
| Store and retrieve facets               | [`Tx.PutFacet`, `Tx.UpdateFacet`, `Tx.GetFacet`, `Tx.AscendFacetsForCell`](../../facets_edges.go)             |
| Store and retrieve edges                | [`Tx.PutEdge`, `Tx.LinkCells`, `Tx.GetEdge`, `Tx.AscendEdgesFrom`](../../facets_edges.go)                     |
| Create and query seams                  | [`Tx.PutSeam`, `Tx.MarkConflict`, `Tx.MarkSupersedes`, `Tx.FindSeams`, `Tx.FindSeamsAt`](../../seams.go) |
| Stream snapshot-visible seams           | [`Tx.AscendSeams`](../../seam_scan.go)                                                               |
| Resolve a seam                          | [`Tx.ResolveSeam`](../../seams.go)                                                                       |

`UpdateFacet` requires a visible cell and the SHA-256 hash of its current
raw content. It returns `ErrCellNotFound` for a missing cell and
`ErrFacetDerivationMismatch` for a mismatched hash. Use `PutFacet` for
a write that does not require the hash check. `PutSeam` returns
`ErrSeamEndpointMismatch` if an update changes an existing seam's endpoints.

[`DB.BatchPutCells`](../../batch.go) writes large cell collections in bounded transactions. `ContinueOnError`
collects pre-write rejections. Storage and post-write hook failures abort the
current chunk. Progress and the written count describe durable rows.

[`Tx.ExportCellsJSON`](../../batch.go) streams visible cells in a bounded spatial
disk as a JSON array. [`DB.ImportCellsJSON`](../../batch.go) consumes that shape
and commits cells in bounded batches. JSON transfer is an application-data
interchange mechanism, not a physical backup: it excludes non-cell key families,
WAL state, changelog history, and retained cell versions. Import is not atomic
across the complete stream; an error or cancellation can leave earlier batches
committed, and the returned count reports that progress. Use [`DB.BackupTo`](../../backup.go)
for a recoverable database copy.

Secondary-index scans avoid full cell or seam scans:

- [`Tx.AscendCellsBySource`, `Tx.AscendCellsInTimeBucket`, `Tx.AscendCellsByTag`](../../cell_secondary.go)
- [`Tx.AscendDistinctTags`, `Tx.ListExistingTopics`](../../cell_secondary.go)
- [`Tx.AscendSeamsBySource`, `Tx.AscendSeamsInTimeBucket`](../../seam_secondary.go)

The canonical record families and key encodings are in [`HEXXLA_DB.md`](./HEXXLA_DB.md).

## Context assembly and spatial retrieval

| API                                                 | Use                                                                                  |
| --------------------------------------------------- | ------------------------------------------------------------------------------------ |
| [`Tx.AssembleCellView`](../../views.go)             | Hydrate one coordinate into a `CellView`.                                            |
| [`Tx.LoadContext`](../../context_load.go)           | Assemble deterministically bounded context candidates from one or more seeds.        |
| [`FilterCellViews`](../../views.go)                 | Apply caller-owned policy to already assembled views.                                |
| [`Tx.LoadContextFOV`](../../fov_context.go)         | Deterministic visibility-filtered loading with an application-supplied opacity rule. |
| [`Tx.LoadContextVoronoi`](../../voronoi_context.go) | Partition cells among multiple seeds.                                                |
| [`Tx.FindEdgePath`](../../pathfind_api.go)          | Dijkstra shortest path over stored weighted edges.                                   |
| [`Tx.WalkEdges`](../../pathfind_api.go)             | Bounded breadth-first traversal over stored edges.                                   |
| [`Tx.ScanContextRaw`, `Tx.ScanContextAtRaw`](../../spatial_reads.go) | Compatibility primitives for bounded nearest-first raw cell scans. Prefer `LoadContext` for new retrieval code. |

[`LoadContextConfig`](../../context_load.go) controls seeds, ring bounds, validity time, edge expansion, the `MaxCells` result limit, and assembly; validity, supersession, explanations, and requested seams apply across dispatch strategies. A single-seed ring load is nearest-first; multi-seed loads merge candidates round-robin in caller-supplied seed order and deduplicate coordinates and seams. Graph traversal counts only assembled live results against `MaxCells` and stops
once that limit is reached. Separate limits apply to coordinate probes,
outgoing edge rows, and seam probes and index rows. Those work limits are shared
across seeds and enrichment. Exceeding a runtime work limit returns
`ErrSpatialScanLimit`. `AsOf` filters cells; it does not filter enrichment by
its validity window. HexxlaDB does not rank by confidence or count LLM tokens during context assembly. Applications own product ranking, complete-request rendering, provider/model token accounting, and output-token reservation. `ViewAt` snapshot time and record validity time are independent; see [`TX.md`](./TX.md).

## Query and content search

[`Tx.QueryCells`](../../query_exec.go) is the main entry point for finding
seeds. It returns ranked hits containing the primary cell fields. Use
`AssembleCellView` for one enriched cell or `LoadContext` for expansion from
one or more seeds. `CellQuery.Radius` explicitly sets the spatial scope;
zero leaves it unrestricted. Set a positive `MaxResults` for a bounded result
set; non-positive values allow unlimited results. To retain the former bounded
search defaults, use `CellQuery{Radius: 32, MaxResults: 20, SortBy: SortByScore}`.

Lexical scores sum each whitespace-separated token's matches: exact tag +1.0,
tag prefix +0.8, lowercased token found verbatim in content +0.6 (otherwise a
case-insensitive match +0.5), and exact case-insensitive source +0.3. Each
case-folded distinct tag contributes once per token. Confidence adds
`0.1 * Confidence` once per cell. Embedding similarity contributes when supplied;
a blank query disables the lexical filter.

The query planner uses the exact coordinate-probe count to prefer a small
spatial disk over an unknown-cardinality tag or source range; otherwise it uses
the first required tag or source, then a radius scan if present, and finally
a primary scan. Time predicates are applied to visible records within the
selected scope and handle dates on either side of the Unix epoch. The planner
uses no persistent cardinality statistics.

`CellQuery.MaxScanRows` bounds physical cell/tag/source rows or spatial
coordinate probes. A positive `MaxResults` limits the retained results while
preserving the selected ordering over scanned candidates. Ties preserve
candidate traversal order. The query returns sorted partial results with
`ErrQueryScanLimit` if it has work left at that budget. When combined with an
embedding and a required tag, source, or radius, `MaxScanRows` selects exact
scoring over the bounded filter scan. Leave `EmbeddingCandidateLimit` unset
in this mode.

With a positive result limit or a candidate budget, the vector route filters
candidates and can widen the search window to seek more matches. HNSW is
approximate. The flat path takes over when the graph is absent, dirty, or
returns fewer vectors than requested and available. It scores vectors exactly,
but a candidate window can still limit which records reach the filters.
[`QueryCellsWithStats`](../../query_exec.go) reports the path, distinct
candidates examined, and whether the candidate ceiling stopped widening
before the result quota was filled. The ceiling is at most
`MaxEmbeddingFilterCandidates`. With an unlimited result count and both
budgets unset, the query scores stored embeddings exactly.
With text and an embedding supplied, a lexical token must match and embedding
similarity contributes to ranking.

Typical retrieval flow:

```text
DB.View
  -> QueryCells
  -> choose one or more Cell.Coord seeds
  -> LoadContext
  -> ContextPack
```

### Specialized retrieval

Use `WalkEdges` for reachability and `FindEdgePath` for weighted paths.
`WalkEdgeCoords` remains an alias of `WalkEdges`. Field of view
(`LoadContextFOV`) returns raw cells, and Voronoi (`LoadContextVoronoi`) returns
raw cells grouped by seed index.

Weighted Voronoi assigns each coordinate in the footprint to the seed with the
lowest path cost within the hop radius. The binary values of finite penalties
and the unit hop costs are summed exactly, and seed order breaks exact ties.
Invalid penalties or sums above the largest finite `float64` return
`ErrInvalidArgument`. FOV uses
symmetric center-to-center visibility, with an opaque hex boundary blocking
sight through it. Both stop computing geometry when the context is cancelled.
Graph context accepts a comma-separated set of relation names in a single
breadth-first traversal.

`FindEdgePath` returns a nil path with no error when the goal is unreachable.
Set `MaxExpand` to cap expanded nodes and `MaxEdgeRows` to cap adjacency rows
examined across the entire search, including rows excluded by `Filter`. Zero
means unlimited. Budget exhaustion returns `ErrSpatialScanLimit`. Coordinates
outside the packable lattice, negative budgets, and reachable goals whose path
costs all overflow `float64` return `ErrInvalidArgument`.

For a current view with `requires` edges, this pack follows up to two hops
from `start`, even when a target lies outside its geometric neighborhood.
The path call uses the stored edge costs to reach `goal`.

```go
pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
    Seeds: []hexxladb.Coord{start}, EdgeFilter: "requires",
    MaxHops: 2, MaxCells: 12,
})
if err != nil {
    return err
}
path, err := tx.FindEdgePath(ctx, start, goal, hexxladb.FindEdgePathConfig{
    Filter: "requires", MaxExpand: 100, MaxEdgeRows: 2_000,
})
if err != nil {
    return err
}
fmt.Println(len(pack.Cells), path)
```

With an empty `EdgeFilter`, `LoadContext` expands in rings. Historical
views can use radial context with historical cell data. Requests for graph
expansion or edge enrichment on those views return
`ErrHistoricalDataUnavailable`.

To treat known obstacle coordinates as opaque, pass a stable lookup to FOV.
For two independent seeds, Voronoi gives each coordinate to one region and
returns cells keyed by the original seed index.

```go
visible, err := tx.LoadContextFOV(ctx, start, 3, func(c hexxladb.Coord) bool {
    return obstacles[c]
}, hexxladb.FOVContextConfig{MaxCells: 12})
if err != nil {
    return err
}
regions, err := tx.LoadContextVoronoi(ctx, []hexxladb.Coord{start, goal},
    hexxladb.VoronoiContextConfig{MaxRadius: 4, MaxCellsPerSeed: 12})
if err != nil {
    return err
}
fmt.Println(len(visible), len(regions[0]), len(regions[1]))
```

Spatial work is bounded before lattice expansion: raw/ring scans accept radii
through `MaxSpatialScanRadius`; seam lookup additionally bounds radius, index
rows, and returned seams; context loading bounds radius, seeds, results, hops,
and combined coordinate probes; FOV accepts radii through 256; Voronoi accepts
radii through 64 and at most 32 seeds. Result limits remain separate from work
limits.

Use [`RenderHexGrid`](../../hex_render.go) for bounded diagnostic rendering. It is a presentation helper, not a query primitive.

## Inspection and analytics

These APIs inspect visible state without introducing a metrics subsystem:

| API                                                             | Use                                                            |
| --------------------------------------------------------------- | -------------------------------------------------------------- |
| [`Tx.RingDensityMap`, `TotalDensity`](../../ring_density.go)     | Measure per-ring and aggregate cell occupancy.                  |
| [`RenderHexGrid`, `Tx.RenderHexGridFromDB`](../../hex_render.go) | Render a caller-labelled or database-occupancy diagnostic grid. |
| [`Tx.TagCounts`](../../tag_analytics.go)                         | Count visible cells for every distinct tag.                     |
| [`Tx.TagCooccurrences`](../../tag_analytics.go)                  | Count visible tag pairs above a caller-supplied minimum.        |
| [`Tx.UntaggedCells`](../../tag_analytics.go)                     | Find visible untagged cells in a bounded spatial disk.          |

Grid rendering is capped at `MaxRenderRadius`. Density and untagged-cell scans
use the public spatial radius bound. Tag counts and co-occurrence analysis can
visit the complete visible tag index and are cancellable but do not expose a
row budget; treat them as operator or offline analytics on larger databases,
not as unbounded request-path operations.

## Embeddings

Embeddings are optional. Configure a dimension and distance metric with [`Options`](../../options.go), or allow the first write to establish the dimension.

| API                                                                                 | Use                                                                                       |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [`Tx.PutEmbedding`, `Tx.GetEmbedding`, `Tx.DeleteEmbedding`](../../tx_embedding.go) | Manage one vector per cell.                                                               |
| [`Tx.PutEmbeddingWithOptions`](../../tx_embedding.go)                              | Store an authoritative vector while optionally deferring HNSW maintenance.                |
| [`Tx.SearchByEmbedding`](../../embedding_search.go)                                 | HNSW-assisted nearest-neighbor search with flat-scan fallback.                            |
| [`Tx.SearchByEmbeddingWithStats`](../../embedding_search.go)                        | The same search plus the selected HNSW/flat path and effective HNSW query breadth.        |
| [`DB.RebuildEmbeddingIndex`](../../embedding_index_rebuild.go)                      | Build a bounded replacement HNSW graph and publish it atomically if embeddings are unchanged. |
| [`Tx.ReindexEmbeddings`](../../embedding_reindex.go)                                | Recompute vectors transactionally; the first vector can establish an unset dimension.    |
| [`DB.EmbeddingDimension`, `DB.EmbeddingMetric`](../../embedding.go)                  | Inspect persisted vector configuration; dimension is zero before the first auto-detected write. |

`EmbeddingSearchConfig.EfSearch` trades HNSW latency and allocation for recall. Zero uses a bounded dimension-aware default; explicit values from 1 through 10,000 are accepted and are raised to at least `MaxResults`. `EmbeddingSearchStats` exposes the effective value. `CellQuery.Embedding` integrates vector similarity into cell queries. Both `SearchByEmbedding(ctx, vec, cfg)` and `SearchByEmbeddingWithStats(ctx, vec, cfg)` honor cancellation during graph traversal and flat scans. HexxlaDB remains usable without embeddings through explicit coordinates and indexed metadata.

For bulk ingestion, pass `EmbeddingWriteOptions{DeferIndexMaintenance: true}`
to each write, then call `RebuildEmbeddingIndex`. The persisted index revision
makes every query use exact flat search while the graph is stale. Rebuild first
checks the vector count, conservative memory budget, and available filesystem
space. It uses the published HNSW diversity heuristic, builds outside the write
transaction, and checks that layer zero can reach every vector from the entry
point. It publishes all graph records in one commit only if no embedding changed
after the snapshot. An unreachable vector prevents publication and returns
`ErrEmbeddingIndexDisconnected`. Failed or cancelled rebuilds leave flat search
active and preserve the previously published graph.
Online insertions that may remove existing layer-zero links or move the entry
point recheck reachability before the graph can serve searches. Older graphs
use exact search until rebuilt.
The default rebuild limit is 10,000 vectors and the validated hard limit is
20,000; raise neither service expectations nor resource budgets without
representative evidence.

Embedding reads validate the persisted vector length, configured dimension, and
component finiteness. Malformed persisted embedding keys or values return
`ErrCorruptDatabase`; they are never silently skipped or decoded as partial vectors.

## Derived super-hex occupancy

[`NewSuperHexSummaryIndex`](../../superhex_summary.go) creates a rebuildable, process-local aperture-7 occupancy index. Call `Rebuild` from a consistent snapshot, then `Sync` from the logical changelog. It is not persisted and contains occupancy statistics rather than cell content.

Operational setup and evidence gates are documented in [`OPERATIONS.md`](./OPERATIONS.md) and [`PERFORMANCE_EVIDENCE.md`](./PERFORMANCE_EVIDENCE.md).

## MVCC lifecycle and snapshots

Plaintext databases use engine v2 and encrypted databases use authenticated
v3. Both support MVCC snapshots. Ordinary `Open` retains the persisted format.

| API                                                                                | Use                                                   |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------- |
| [`DB.StatsMVCC`, `DB.SuggestedPruneBeforeSeq`, `DB.MVCCPrunePlan`](../../mvcc_lifecycle.go) | Inspect history and derive a retention watermark and bounded pass size. |
| [`DB.PruneCellVersions`, `DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go) | Remove eligible old versions in bounded passes.       |
| [`DB.OldestSnapshotSeq`](../../mvcc_snapshot_floor.go) | Inspect the earliest guaranteed point-in-time snapshot. |
| [`PruneScheduler`](../../mvcc_lifecycle.go)                                        | Drive pruning from an application-owned timer.        |
| [`DB.TagSnapshot`, `DB.ListSnapshotTags`, `DB.ViewAtTag`, `DB.DeleteSnapshotTag`](../../snapshot_tags.go) | Name, enumerate, revisit, and delete commit snapshots. |
| [`DB.SnapshotDiff`](../../snapshot_diff.go)                                        | Inspect retained cell and seam versions across commit sequences. |

Pruning preserves the cell state visible at the cutoff and can advance the
earliest guaranteed snapshot. Historical views below it return
`ErrSnapshotExpired`. Tags name sequences and expire with the history they
reference. See [transaction semantics](./TX.md#retained-snapshots) for the
upgrade rule. Use tail reclaim or compaction when disk reclamation is needed.

`SnapshotDiff` is a deterministic retained-history diagnostic. Pruned versions
and mutations outside the cell/seam families are absent; use the optional
logical changelog with durable consumer cursors for CDC and audit processing.

The exported root API is classified as a v1 candidate but the module remains on
the `v0.y.z` line until the external graduation gates in
[`VERSIONING.md`](../../VERSIONING.md) pass. CI runs `task api-check` against the
[current API baseline](../../api/public.txt) so additions, signature changes,
and public documentation changes receive deliberate review.

## Advanced byte-key access

[`Tx.Put`](../../tx.go) writes application-owned values. Use a prefix such as
`app/`. Writes under database-owned families return `ErrInvalidArgument`.
`Tx.Get` and `Tx.AscendRange` expose physical rows. Use typed reads for MVCC
visibility and typed writes for validation, indexes, and changefeed.

New writes escape binary values matching internal storage markers. Older builds
stored raw 14-byte values beginning `0xFF 0x4F` in the same representation as
overflow stubs. Verify affected historical application values against trusted
original bytes before rewriting them.

## Changefeed, health, and maintenance

| API                                                                          | Use                                                    |
| ---------------------------------------------------------------------------- | ------------------------------------------------------ |
| [`DB.ReadChangelogSince`, `DB.ReadChangelogFiltered`](../../db_changelog.go) | Read the optional at-least-once logical changefeed.             |
| [`DB.AdvanceChangelogConsumer`, `DB.GetChangelogConsumerCursor`](../../changelog_consumers.go) | Register and compare-and-advance a named durable cursor. |
| [`DB.ListChangelogConsumers`, `DB.DeleteChangelogConsumer`, `DB.ChangelogRetentionFloor`](../../changelog_consumers.go) | Inspect/delete consumers and compute the advisory minimum acknowledged sequence. |
| `DB.ResetChangelogHistory` | Explicitly clear the retained-history checkpoint on a disabled handle with zero consumers before re-bootstrap. |
| [`DB.HealthCheck`](../../health.go)                                          | Fail closed on malformed visible records and optionally validate orphans and secondary indexes. |
| [`DB.WALStats`](../../db.go)                                            | Observe WAL commit and redo-sync counters.                                    |
| [`DB.WriteStats`](../../write_stats.go)                                      | Observe cumulative write contention and phase timing.         |
| [`DB.BackupTo`](../../backup.go)                                             | Capture a consistent primary/WAL/changelog recovery set.       |
| [`DB.StorageStats`](../../storage_stats.go)                                  | Measure physical, reachable, and reclaimable storage.         |
| [`DB.ReclaimTail`](../../reclaim.go)                                         | Truncate a contiguous authenticated-freelist suffix safely.   |
| [`PreflightCompactTo`](../../maintenance_preflight.go)                       | Check source storage, path collisions, and copy capacity without creating a candidate. |
| [`DB.Compact`, `CompactTo`](../../compact.go)                                | Copy physical keys, including retained versions and tombstones, into a compact file.                     |
| [`DB.CompactWithOptions`, `CompactToWithOptions`](../../compact.go)          | Bound copy batches, receive durable progress, and optionally verify the candidate. |
| [`RepairMVCCIndexesTo`](../../compact.go)                                    | Rebuild MVCC secondary indexes in a verified copy from retained primary versions. |
| [`DeriveKeyFromPassphrase`](../../encryption.go)                             | Derive an encryption key using the database KDF.               |
| [`RotateEncryption`, `RotateEncryptionWithOptions`](../../rotation.go)       | Encrypt plaintext v2 or rotate authenticated v3 credentials offline, preserving retained snapshots.          |
| [`RecoverInterruptedRotation`](../../rotation.go)                            | Roll back an interrupted encryption-file swap using matching changelog configuration. |

Retired formats require [offline conversion](./OPERATIONS.md#offline-conversion-of-retired-files).

## Validation and hooks

[`CellValidator`](../../hooks.go) validates cells before persistence. [`AfterPutCellHook`](../../hooks.go) and [`AfterPutSeamHook`](../../hooks.go) run synchronously after their corresponding logical writes. Function adapters are provided for all three interfaces, and hooks are configured through [`Options`](../../options.go).

A cell post-write hook error aborts the update. For seam hooks, return the
hook error from the update callback to abort. Dispatch irreversible external
effects after commit confirmation. See [commit finalization](./TX.md#commit-finalization-failures).

## Errors

Public sentinel errors are defined in [`errors.go`](../../errors.go). Handle stable error semantics with `errors.Is` and `errors.As`, including:

- lifecycle and locking errors such as `ErrDatabaseClosed` and `ErrDatabaseLocked`;
- argument and transaction errors such as `ErrInvalidArgument` and `ErrTxReadOnly`. An encoded cell larger than the configured value limit returns `ErrInvalidArgument`;
- snapshot errors such as `ErrReadSeqFuture`, `ErrSnapshotExpired`, and `ErrHistoricalDataUnavailable`;
- format and migration errors such as `ErrUnsupportedFormatVersion`, `ErrMigrationIncomplete`, and `ErrMigrationChangelogState`;
- interrupted candidate refusal through `ErrCompactionIncomplete`;
- maintenance-capacity refusal through `ErrInsufficientSpace`;
- encryption and changelog errors documented in their focused references;
- `ErrCommitFinalization`, which requires recovery before another write, and `ErrCommitDurable`, which specifically means the authoritative commit is known durable and must not be retried.

Do not compare error strings.

## Examples

- [Quickstart](../../examples/quickstart) writes cells, searches them, loads graph context, and fits the rendered prompt.
- [Ports and adapters](../../examples/ports_adapters) writes an atomic contradiction through an application-owned port.
- [Remote access](../../examples/remote_access) demonstrates a loopback HTTP service with a single database owner.

See the [performance evidence guide](./PERFORMANCE_EVIDENCE.md) for the opt-in workload tools.
