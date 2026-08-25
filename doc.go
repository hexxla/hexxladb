// Package hexxladb is the stable public API for the embedded HexxlaDB engine.
//
// Documentation map:
//   - docs/hexxladb/HEXXLA_DB.md — storage layout and key spec (how it works).
//   - docs/hexxladb/API_REFERENCE.md — task-oriented public API guide.
//   - docs/hexxladb/TX.md — transactions, primitives, MVCC temporal semantics.
//   - docs/hexxladb/OPERATIONS.md — embedding, backups, MVCC retention, incident response.
//   - docs/hexxladb/DURABILITY.md, ENCRYPTION.md, CHANGEFEED.md — focused feature refs.
//   - docs/hexxladb/HEXXLA.md — memory model reference (product context).
//   - docs/ROADMAP.md — pending and evidence-gated work.
//
// Implementation packages under internal/ are private to this module.
//
// Exported entrypoints:
//   - [Open], [Options], [DB], [DB.Close] — exclusively locked engine shell with durable WAL (see docs/hexxladb/DURABILITY.md).
//     Optional logical changefeed ([Options.ChangelogEnabled], [DB.ReadChangelogSince]) with durable named cursors
//     ([DB.AdvanceChangelogConsumer], [DB.GetChangelogConsumerCursor]; see docs/hexxladb/CHANGEFEED.md).
//     Configurable page size via [Options.PageSize] (4096/8192/16384/65536; default 4096); readable at runtime via [DB.PageSize].
//     Per-database value size limit via [Options.MaxValueBytes] (512–1048576 bytes; default 8192); readable at runtime via [DB.MaxValueBytes].
//     Transparent per-value DEFLATE compression is always-on (compress/flate, Go stdlib; values ≥ 64 bytes).
//     MVCC on new databases via [Options.EnableMVCC] / [Options.MVCCRetention];
//     lifecycle: [DB.StatsMVCC], [DB.PruneCellVersions], [DB.SuggestedPruneBeforeSeq], [PruneScheduler]
//     (see docs/hexxladb/OPERATIONS.md). Encryption operations include [RotateEncryption].
//     [MigrateV1ToV2] performs the offline, resumable, verified upgrade from a format-v1 source.
//   - [DB.SnapshotDiff] — MVCC change diff between two commit sequences; yields [SnapshotDiff] with [CellDiff]/[SeamDiff] slices.
//   - [DB.View], [DB.ViewAt], [DB.ViewAtTime], [DB.Update], [DB.Batch], [Tx] —
//     Bolt-style transactions; [DB.Batch] is an alias for [DB.Update]; see docs/hexxladb/TX.md.
//     [DB.WriteStats] and [DB.GroupWALStats] expose cumulative write-phase timing and batching counters.
//   - [DB.StorageStats] — physical, live, and reclaimable storage accounting.
//   - [DB.BackupTo] — consistent online physical backup of the primary, WAL, and optional changelog.
//   - [DB.Compact], [DB.CompactWithOptions], [CompactTo], [CompactToWithOptions] — bounded copy-compaction to reclaim dead pages
//     with optional durable progress reporting (see docs/hexxladb/OPERATIONS.md).
//   - [Tx.Get], [Tx.Put], [Tx.AscendRange] — byte-key ordered store.
//     Lattice primitives: [Tx.PutCell], [Tx.GetCell], [Tx.DeleteCell], [Tx.DeleteCellWithOutcome], [Tx.WalkRing], [Tx.PutSeam],
//     [Tx.FindSeams], [Tx.FindSeamsAt], [Tx.LoadContext], [Tx.ResolveSeam] (see [primitives.go]);
//     facets/edges: [Tx.PutFacet], [Tx.GetFacet], [Tx.AscendFacetsForCell],
//     [Tx.PutEdge], [Tx.GetEdge], [Tx.AscendEdgesFrom] ([facets_edges.go]);
//     stable record aliases: [CellRecord], [SeamRecord], [FacetWalkRecord], [EdgeWalkRecord] ([export.go]);
//     [NewFacetDerived], [NewProvenanceWire] ([templates.go]) for callers outside this module;
//     spec-named sugar: [Tx.MarkConflict], [Tx.UpdateFacet], [Tx.LinkCells];
//     validity read filters: [record.ValidAt], [Tx.WalkRingAt], [Tx.WalkRingFacets];
//     secondary index walks: [Tx.AscendCellsBySource], [Tx.AscendCellsInTimeBucket],
//     [Tx.AscendCellsByTag], [Tx.AscendDistinctTags], [Tx.ListExistingTopics],
//     [Tx.AscendSeamsBySource], [Tx.AscendSeamsInTimeBucket];
//     HEXXLA-shaped views ([views.go]): [CellView], [ContextPack], [Tx.AssembleCellView],
//     [CellViewPredicate], [FilterCellViews];
//     spatial context: [Tx.LoadContextFOV] (shadowcasting FOV), [Tx.LoadContextVoronoi] (Voronoi regions),
//     [Tx.FindEdgePath] (Dijkstra over weighted edges), [Tx.WalkEdges] (BFS over edges);
//     derived hierarchy: [NewSuperHexSummaryIndex], [SuperHexSummaryIndex], and
//     [SuperHexSummary] (rebuildable aperture-7 occupancy summaries).
//   - Embedding / vector search: dimension auto-detected from first [Tx.PutEmbedding]
//     (or pre-set via [Options.EmbeddingDimension]); [Options.DistanceMetric] (default cosine);
//     [Tx.PutEmbedding], [Tx.GetEmbedding], [Tx.DeleteEmbedding];
//     [Tx.SearchByEmbedding] (HNSW-accelerated ANN, flat-scan fallback),
//     [Tx.SearchByEmbeddingWithStats] (execution path and effective breadth), [EmbeddingSearchConfig];
//     [Tx.ReindexEmbeddings]; [CellQuery.Embedding] / [CellSearchConfig.Embedding] integrate
//     vector search into [Tx.QueryCells] / [Tx.SearchCells] (see docs/hexxladb/API_REFERENCE.md).
//   - Sentinel errors: [ErrCorruptDatabase], [ErrDatabaseClosed], [ErrDatabaseLocked], [ErrTxReadOnly],
//     [ErrNilCallback], [ErrNotImplemented], [ErrClosed], [ErrSeamNotFound],
//     [ErrSeamEndpointMismatch], [ErrInvalidArgument], [ErrEncryptionKeyRequired],
//     [ErrDatabaseNotEncrypted], [ErrEncryptionOptions], [ErrEncryptionKeyMismatch],
//     [ErrCellNotFound], [ErrFacetDerivationMismatch], [ErrChangelogDisabled],
//     [ErrChangelogCorrupt], [ErrChangelogConsumerNotFound], [ErrChangelogCursorConflict],
//     [ErrChangelogCursorRegression], [ErrChangelogCursorBeyondHead], [ErrChangelogConsumerInvalidated],
//     [ErrReadSeqFuture], [ErrMVCCRequired],
//     [ErrSnapshotTagNotFound], [ErrSnapshotTagLabelTooLong].
//
// Lattice types ([Coord], [PackedCoord], [Pack], [Unpack], [Ring], [WalkRings]) are
// re-exported from internal/lattice; see docs/hexxladb/HEXXLA.md and
// internal/lattice/PACKED_COORD.md for geometry and key layout.
//
// Internal spatial algorithms (not exported at root level) power the context-loading
// methods above: [FieldOfView] (symmetric shadowcasting, Albert Ford 2021 adaptation),
// [Voronoi] (multi-source Dijkstra with optional [WeightFunc] cost function),
// and pathfinding heuristics for regular-grid callers. [Tx.FindEdgePath] uses
// Dijkstra because stored edges may be long-range and have subunit weights.
//
// # Embedding in your program
//
// Follow the wiring pattern in cmd/hexxladb/main.go: configuration, structured
// logging, [Open] with [DB.Close], inject dependencies into your application
// layer. Outbound adapters should call only types and functions exported from
// package hexxladb, not internal/engine (see docs/architecture/HEXAGONAL_ARCHITECTURE.md).
package hexxladb
