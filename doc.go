// Package hexxladb is the stable public API for the embedded HexxlaDB engine.
//
// Documentation map:
//   - docs/hexxladb/HEXXLA_DB.md — storage layout and key spec (how it works).
//   - docs/hexxladb/API_REFERENCE.md — task-oriented public API guide.
//   - docs/hexxladb/CONFIGURATION.md — options, defaults, and supported combinations.
//   - docs/hexxladb/TX.md — transactions, primitives, MVCC temporal semantics.
//   - docs/hexxladb/OPERATIONS.md — embedding, backups, MVCC retention, incident response.
//   - docs/hexxladb/PERFORMANCE_EVIDENCE.md — repeatable correctness and performance evidence.
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
//     MVCC on new plaintext databases via [Options.EnableMVCC] / [Options.MVCCRetention];
//     new encrypted databases always use authenticated MVCC format v3;
//     lifecycle: [DB.StatsMVCC], [DB.PruneCellVersions], [DB.SuggestedPruneBeforeSeq], [PruneScheduler]
//     (see docs/hexxladb/OPERATIONS.md). Encryption operations include [RotateEncryption].
//     [PreflightMigrateV1ToV2] and [MigrateV1ToV2] provide an offline,
//     capacity-checked, resumable, verified upgrade from a format-v1 source.
//     [PreflightMigrateToAuthenticated] and [MigrateToAuthenticated] create a
//     verified authenticated encrypted v3 candidate from a closed v1 or v2 source.
//   - [DB.SnapshotDiff] — retained MVCC cell/seam history between two commit sequences;
//     use the logical changelog for complete CDC or audit processing.
//   - [DB.View], [DB.ViewAt], [DB.ViewAtTime], [DB.Update], [DB.Batch], [Tx] —
//     Bolt-style transactions; [DB.Batch] is an alias for [DB.Update]; see docs/hexxladb/TX.md.
//     [DB.WriteStats] and [DB.GroupWALStats] expose cumulative write-phase timing and batching counters.
//   - [DB.StorageStats] — physical, live, reusable, and reclaimable storage accounting;
//     [DB.ReclaimTail] safely truncates a contiguous authenticated-freelist suffix.
//   - [DB.BackupTo] — consistent online physical backup of the primary, WAL, and optional changelog.
//   - [DB.BatchPutCells], [DB.ImportCellsJSON], [Tx.ExportCellsJSON] — bounded-transaction
//     cell ingestion and application-level JSON transfer; JSON transfer is not a physical backup.
//   - [DB.Compact], [DB.CompactWithOptions], [CompactTo], [CompactToWithOptions] — bounded copy-compaction to reclaim dead pages
//     with optional durable progress and destination verification; [PreflightCompactTo]
//     checks paths, source storage, and capacity (see docs/hexxladb/OPERATIONS.md).
//   - [Tx.Get], [Tx.Put], [Tx.AscendRange] — byte-key ordered store.
//     Lattice primitives: [Tx.FindFreeCellPlacement], [Tx.PutCell], [Tx.GetCell], [Tx.DeleteCell], [Tx.DeleteCellWithOutcome], [Tx.WalkRing], [Tx.PutSeam],
//     [Tx.FindSeams], [Tx.FindSeamsAt], [Tx.LoadContext], [Tx.ResolveSeam] (see [primitives.go]);
//     public context, raw-spatial, seam, and candidate constants bound enumeration
//     work and return [ErrSpatialScanLimit] when a seam scan exhausts its budget;
//     facets/edges: [Tx.PutFacet], [Tx.GetFacet], [Tx.AscendFacetsForCell],
//     [Tx.PutEdge], [Tx.GetEdge], [Tx.AscendEdgesFrom] ([facets_edges.go]);
//     stable record aliases: [CellRecord], [SeamRecord], [FacetWalkRecord], [EdgeWalkRecord] ([export.go]);
//     [NewFacetDerived], [NewProvenanceWire] ([templates.go]) for callers outside this module;
//     spec-named sugar: [Tx.MarkConflict], [Tx.UpdateFacet], [Tx.LinkCells];
//     validity read filters: [Tx.WalkRingAt], [Tx.WalkRingFacets], and [LoadContextConfig.AsOf];
//     secondary index walks: [Tx.AscendCellsBySource], [Tx.AscendCellsInTimeBucket],
//     [Tx.AscendCellsByTag], [Tx.AscendDistinctTags], [Tx.ListExistingTopics],
//     [Tx.AscendSeamsBySource], [Tx.AscendSeamsInTimeBucket];
//     HEXXLA-shaped views ([views.go]): [CellView], [ContextPack], [Tx.AssembleCellView],
//     [CellViewPredicate], [FilterCellViews];
//     spatial context: [Tx.LoadContextFOV] (shadowcasting FOV), [Tx.LoadContextVoronoi] (Voronoi regions),
//     [Tx.FindEdgePath] (Dijkstra over weighted edges), [Tx.WalkEdges] (BFS over edges);
//     raw compatibility scans: [Tx.ScanContextRaw], [Tx.ScanContextAtRaw];
//     diagnostics: [Tx.RingDensityMap], [TotalDensity], [Tx.TagCounts],
//     [Tx.TagCooccurrences], [Tx.UntaggedCells], [RenderHexGrid], [Tx.RenderHexGridFromDB];
//     derived hierarchy: [NewSuperHexSummaryIndex], [SuperHexSummaryIndex], and
//     [SuperHexSummary] (rebuildable aperture-7 occupancy summaries).
//   - Embedding / vector search: dimension auto-detected from first [Tx.PutEmbedding]
//     (or pre-set via [Options.EmbeddingDimension]); [Options.DistanceMetric] (default cosine);
//     [Tx.PutEmbedding], [Tx.PutEmbeddingWithOptions], [Tx.GetEmbedding], [Tx.DeleteEmbedding];
//     [Tx.SearchByEmbedding] (HNSW-accelerated ANN, flat-scan fallback),
//     [Tx.SearchByEmbeddingWithStats] (execution path and effective breadth), [EmbeddingSearchConfig];
//     runtime configuration introspection through [DB.EmbeddingDimension] and [DB.EmbeddingMetric];
//     bounded derived-index publication through [DB.RebuildEmbeddingIndex];
//     [Tx.ReindexEmbeddings]; [CellQuery.Embedding] / [CellSearchConfig.Embedding] integrate
//     vector search into [Tx.QueryCells] / [Tx.SearchCells] (see docs/hexxladb/API_REFERENCE.md).
//   - Sentinel errors are defined in errors.go and support [errors.Is]. Recovery-sensitive
//     categories include lifecycle ([ErrDatabaseClosed], [ErrDatabaseLocked]), persisted integrity
//     ([ErrCorruptDatabase]), bounded scans ([ErrQueryScanLimit], [ErrSpatialScanLimit]), format and migration
//     refusal ([ErrUnsupportedFormatVersion], [ErrMigrationIncomplete], [ErrMigrationChangelogState], [ErrCompactionIncomplete]),
//     commit finalization ([ErrCommitFinalization], [ErrCommitDurable]), encryption and changelog
//     integrity ([ErrEncryptionKeyMismatch], [ErrChangelogCorrupt], [ErrChangelogConsumerInvalidated]),
//     placement ([ErrNoFreeCellPlacement]), embedding rebuild
//     ([ErrEmbeddingIndexChanged], [ErrEmbeddingIndexTooLarge]), maintenance capacity ([ErrInsufficientSpace]),
//     and MVCC snapshot errors ([ErrReadSeqFuture], [ErrMVCCRequired]).
//   - Named snapshots: [DB.TagSnapshot], [DB.ListSnapshotTags], [DB.ViewAtTag],
//     [DB.DeleteSnapshotTag]; retained-history comparison through [DB.SnapshotDiff].
//   - Interrupted offline encryption swaps fail closed through [ErrRotationIncomplete]
//     and are rolled back explicitly with [RecoverInterruptedRotation].
//
// Lattice types ([Coord], [PackedCoord], [Pack], [Unpack], [Ring], [WalkRings]) are
// re-exported from internal/lattice; see docs/hexxladb/HEXXLA.md and
// internal/lattice/PACKED_COORD.md for geometry and key layout.
//
// Private spatial algorithms power the context-loading methods above: symmetric
// shadowcasting for [Tx.LoadContextFOV], multi-source Dijkstra with an optional
// [VoronoiWeightFunc] for [Tx.LoadContextVoronoi], and pathfinding heuristics for
// regular-grid callers. [Tx.FindEdgePath] uses
// Dijkstra because stored edges may be long-range and have subunit weights.
//
// # Embedding in your program
//
// The examples/remote_access command demonstrates configuration, structured
// logging, [Open] with [DB.Close], and dependency injection at an application
// boundary. Outbound adapters should call only types and functions exported from
// package hexxladb, not internal/engine (see docs/architecture/HEXAGONAL_ARCHITECTURE.md).
package hexxladb
