# Changelog

## [Unreleased]

### Added

- **`(*Tx).DeleteCellWithOutcome`** — Like **`DeleteCell`** but returns **`removed bool`**: **`true`** when a visible cell was tombstoned (MVCC) or hard-deleted (v1); **`false`** on idempotent no-op (`delete_cell.go`). Callers that need deletion observability should use this; **`DeleteCell`** remains a thin wrapper.

### Fixed

- **HealthCheck double-counting MVCC seams** — The seam/ primary scan appended every physical row. Each **ResolveSeam** writes a new MVCC version of the same ULID, so **SeamCount** (and resolved/unresolved splits) inflated after resolution. The checker now groups versioned keys by ULID and applies [**mvcc.SelectVisible**](internal/mvcc/version_suffix_cell_key.go) at the view’s **read_seq**, matching [**getSeamVisibleRaw**](mvcc.go). Regression: **TestHealthCheck_MVCC_seam_resolve_not_double_counted**.

- **HealthCheck false positives on MVCC tag/source indexes** — [HealthCheck](health.go) used to flag every secondary key whosePackedCoord lacked a **visible head** cell. Under MVCC, [PutCell](primitives.go) keeps older `tag/` and `source/` rows per commit sequence so [ViewAt](tx.go) stays consistent; after multiple updates at one coord followed by **DeleteCell**, those historical rows are still legitimate while the primary tombstone hides the live head. The checker now parses the MVCC suffix on physical keys (via [ParseTagKeyWithSeq](internal/index/tag_key.go) / [ParseSourceKeyWithSeq](internal/index/source_key.go)), loads `cell/<coord><seq>`, and validates the decoded cell carries the indexed tag/source. Regression: **TestHealthCheck_MVCC_churn_then_delete_cleanSecondaries**.

## [0.3.0] - 2026-04-29

### Added

- **Exported walk alias types for embedding apps** — `FacetWalkRecord` and `EdgeWalkRecord` in [`walk_export_aliases.go`](./walk_export_aliases.go) alias `internal/record` wire structs so MCP/adapters outside the module can type `AscendFacetsForCell` / `AscendEdgesFrom` closures without importing `internal/`.
- **External-call helpers** — `NewProvenanceWire` (timestamps `now`) and `NewFacetDerived` for modules that cannot name `internal/record` types when calling `Tx.LinkCells` / `Tx.PutFacet`.

## [0.2.0] - 2026-04-27

### Fixed

- **WAL unbounded growth** — both `CommitWriteTxn` (classic path) and `applyGroupBatch` (group WAL path, used by all `DB.Update` calls) now truncate the WAL to zero after all pages are durably applied to the primary. Previously the WAL was only truncated on the next `Open`, causing it to accumulate all redo records indefinitely (25 MB for a 128 KB DB after 20 embedding inserts). The WAL is now always zero-length between transactions.
- **B+ tree leaf-page-full on large inline value updates** — `insertIntoLeaf` unconditionally called `buildLeafPage` when replacing an existing key's value without checking whether the updated page still fit within `pageSize`. When HNSW node neighbor lists grow during `PutEmbedding` (e.g. 128-dim embeddings, 32-dim at >12 entries), the updated value is larger than the original, causing the page to overflow and returning `ErrCorruptTree: leaf page full`. Fix: added a `leafSerializedSize` guard on the update-in-place path; pages that exceed `pageSize` after an in-place update now fall through to the existing split path. Regression tests added: `TestPutEmbedding_HighCount_32d` (600 entries) and `TestPutEmbedding_HighCount_128d` (150 entries) in `btree_regression_test.go`.
- **`leafSplitIndex` right-half overflow** — hardened the split-point algorithm to scan until the left half would exceed `pageSize` (not `pageSize/2`), ensuring the right half always fits. Previously, with very large entries, `leafSplitIndex` could return a `mid` where the right half alone exceeded `pageSize`.

### Added (llm-context-engine example)

- New `examples/llm_context_engine` — realistic LLM memory retrieval demo
  - Scenario 1: Ingest 20 conversation turns with Ollama all-minilm embeddings
  - Scenario 2: Semantic retrieval — 3 distinct queries showing HNSW differentiation
  - Scenario 3: Multi-signal retrieval — embeddings + tag filters + confidence + source
  - Scenario 4: Preference supersession — MarkSupersedes + FilterSuperseded in context assembly
  - Scenario 5: Full LLM prompt assembly pipeline — search → preferences → LoadContextPackFrom
  - Scenario 6: Comparison table — what HexxlaDB enables vs stateless LLMs
- Moved embedding functionality out of conversational_memory demo (reverted to 12 phases)

### Added (benchmarks-docs)

- Embedding search benchmarks: `BenchmarkSearchByEmbedding_HNSW` (500×32d, 200×64d, 100×128d), `BenchmarkQueryCells_Embedding` (500×32d)
- Updated `doc.go` with embedding/HNSW entrypoints
- Updated `HEXXLA_DB.md` with HNSW keyspace layout and query engine integration
- Updated `API_REFERENCE.md` with HNSW-accelerated search and query planner integration
- Updated `ROADMAP.md` to mark embeddings keyspace as complete

### Added (query-engine-embedding)

- `CellQuery.Embedding` and `CellSearchConfig.Embedding` trigger ANN-accelerated seed selection
- `QueryCells` planner picks embedding index when `Embedding` is set (highest priority)
- Embedding similarity score added to composite relevance score alongside lexical scoring
- `scanByEmbedding` over-fetches 2× to leave room for post-filter narrowing
- All existing predicates (tags, temporal, spatial, confidence) apply as post-filters on embedding results
- 3 new integration tests: QueryCells + Embedding, Embedding + tag filter, SearchCells + Embedding

### Added (hnsw-graph)

- **HNSW graph** (`hnsw/` keyspace): sub-linear approximate nearest-neighbor search persisted in the B+ tree
- `internal/hnsw` package: `Node` and `Meta` encode/decode, `Graph` with Insert/Search/Delete
- `hnsw/meta`, `hnsw/entry`, `hnsw/node/<packed_coord>` keyspace (keys in `internal/index/hnsw_key.go`)
- HNSW insert with random layer selection, greedy descent, ef-bounded beam search, bidirectional linking
- HNSW search with greedy layer descent and ef-bounded beam at layer 0
- HNSW delete with neighbor repair and entry point promotion
- `SearchByEmbedding` uses HNSW when graph exists, flat-scan fallback otherwise
- `PutEmbedding`/`DeleteEmbedding`/`DeleteCell` cascade maintain HNSW graph automatically
- `Tx.getDirect` helper for internal reads bypassing public API guards
- `txHNSWStorage` adapter bridges `Tx` to `hnsw.Storage` interface
- 7 graph tests (insert, recall, delete, delete-all, delete-entry, update, empty) + 6 node/meta encoding tests

### Added (embeddings-keyspace)

- **Embedding keyspace** (`embed/<packed_coord>`): fixed-dimension float32 vector storage per cell
- `Options.EmbeddingDimension` / `Options.DistanceMetric` — dimension and metric locked at creation, persisted in file header (offsets 104–106)
- `DistanceMetric` type with `DistanceCosine`, `DistanceDotProduct`, `DistanceL2` constants
- Distance functions: cosine similarity, dot product, Euclidean distance (pure math, `internal/engine`)
- `DB.EmbeddingDimension()` / `DB.EmbeddingMetric()` introspection accessors
- `Tx.PutEmbedding`, `Tx.GetEmbedding`, `Tx.DeleteEmbedding` — embed/ keyspace CRUD
- `Tx.SearchByEmbedding` — flat-scan nearest-neighbor search with goroutine parallelism and min-heap top-K
- `Tx.ReindexEmbeddings` — bulk recompute all embeddings via user-supplied callback (model switch support)
- `DeleteCell` cascades to remove the cell's embedding automatically
- `ErrEmbeddingsDisabled`, `ErrEmbeddingDimension` sentinel errors
- 14 new tests covering: put/get round-trip, delete, dimension mismatch, disabled DB, cascade, search (top-K, empty, min-score), reindex, reindex-skip, DB accessors, persistence across reopen, dimension mismatch on reopen
- Distance function unit tests and benchmarks (384-dim, 768-dim)

### Added (content-compression)

- **Always-on transparent per-value DEFLATE compression** via `compress/flate` (Go stdlib, zero external dependencies)
- Compressed values carry a 5-byte `0xFE` envelope; uncompressed values coexist transparently
- Compression runs before overflow check — compressible values may fit inline even if raw size exceeds the threshold
- Values < 64 bytes and incompressible values stored raw (no overhead)
- 10 new engine tests: round-trip, skip-short, mixed long/short, AscendRange, overflow+compression, incompressible, reopen, delete
- No configuration required — compression is always-on with no public API surface

### Added (overflow-pages)

- **Overflow pages**: values exceeding the inline leaf threshold are automatically stored in a chain of overflow pages; reads, scans, deletes, and compact all resolve overflow transparently
- `Options.MaxValueBytes` now accepts 32768, 65536, 131072, 262144, 524288, 1048576 (up to 1 MiB)
- 10 new engine tests: parametric round-trip at all page sizes, multi-page chain, overwrite, inline→overflow transition, delete, AscendRange, many-key stress
- Overflow pages are ordinary data pages — encryption, WAL, and compact work without changes

### Added (storage-contract-tests)

- `internal/domain/storagecontract` package — 22 reusable contract tests for the `domain.Storage` port interface
- Covers all port methods: cells, seams, facets, edges, walks, context assembly, time buckets, tags
- `RunAll(t, Factory)` harness: any adapter can validate conformance by providing a factory function
- Real `hexxladbout.Storage` adapter passes all contracts
- `record.UniqueSortedTags` extracted from root package for reuse; `cell_secondary.go` / `seam_secondary.go` documented in-place

### Added (efficient-storage)

- **Configurable page size**: `Options.PageSize` selects 4096, 8192, 16384, or 65536 bytes for new databases (default 4 KiB); existing databases read page size from the file header on open
- `DB.PageSize()` introspection method returns the active page size
- `engine.IsValidPageSize` public helper for callers that need to validate before Open
- Fill-based B+ tree leaf splitting (replaces fixed `maxLeafEntries=32`); leaves split when serialized size exceeds 50% of page capacity
- Dynamic internal node capacity derived from page size
- WAL record size adapts to runtime page size
- Instance-level page buffer pool sized to the database's page size
- `CompactTo` preserves source page size in destination database
- Parametric engine tests at all four valid page sizes

### Added (delete-compact)

- `Tx.DeleteCell` — remove cell + secondary indexes + facets + outbound edges atomically; idempotent (missing cell returns nil)
- MVCC tombstone support: zero-length value at `cell/<packed>/<writeSeq>` treated as deleted by visibility layer; facets tombstoned likewise
- `tx.cellDeleted` overlay for same-tx delete→get correctness; cleared on re-put
- `changelog.OpDeleteCell` / `ChangelogOpDeleteCell` — stable op code `6` for changefeed consumers
- `domain.Storage.DeleteCell` port + adapter + `app.Service` delegation (hex boundary)
- `DB.Compact` — copy-compact open database to destPath (holds read lock, preserves all data)
- `CompactTo` — standalone copy-compaction from srcPath to destPath; propagates format version, MVCC flag, encryption, MaxValueBytes
- Comprehensive tests for both features: v1/v2, MVCC snapshot isolation, facet/edge cleanup, same-tx overlay, encrypted compact, context cancellation, file size reduction, HealthCheck validation
- Demo Phase 12 in `examples/conversational_memory` — exercises DeleteCell (MVCC tombstone, ViewAt snapshot isolation, idempotent re-delete) and Compact (bulk write→delete→prune→compact with file size reduction)

### Fixed (delete-compact)

- `HealthCheck` on MVCC databases now correctly excludes tombstoned cells from `CellCount` — previously zero-length tombstone values were counted as live cells

### Added (tui-audit)

- `cmd/tui` interactive database explorer — tabs: Dashboard, Cells, Hex Grid, Inspector, Analytics, Seams, Health, Diff; lexical search in Cells tab (`/` to open, `Enter` to execute, `Esc` to clear); Inspector with context pack assembly and explain panel; neon-on-dark colour scheme
- `Consuming() bool` method on `view` interface — tabs signal text-input mode so global shortcuts (`q`, `1-8`, `Tab`) don't intercept keystrokes during search
- `noConsume` embedded struct — zero-overhead default `Consuming() false` for all non-input views

### Changed (tui-audit)

- `Init()` now batches `tea.WindowSize()` + `tabActivatedMsg` — ensures window dimensions and initial tab load fire correctly on startup
- Replaced all `tea.Tick(time.Millisecond, ...)` one-shot load patterns with plain `tea.Cmd` closures — eliminates spurious 1 ms delays and scheduler round-trips
- All view `Update` methods handle `tabActivatedMsg` explicitly for lazy loads; removed fallthrough `!v.loaded` guards that could fire duplicate load goroutines on every message
- Content area uses `MaxHeight` hard-clip — tab bar and status bar can never be pushed off screen by overflowing view content
- Tab bar height derived from `lipgloss.Height(renderTabBar())` — no hardcoded row count
- `renderContent` passes full terminal width to `lipgloss.Place` with `WithWhitespaceBackground` — right edge always filled regardless of inner content width
- Card-interior text styles (`styleCardDim`, `styleCardHeader`, `styleCardKey`, `styleCardValue`) added — eliminates `colorBg1` leaking into `colorBg2` stat/info cards across all views
- `barGraphBg` helper added — bar characters inside cards rendered with correct card background
- Removed unused `styleKey`, `styleValue`, `stylePink` variables

### Changed (docs)

- README rewritten — sharper introduction framing the spatial-locality-as-physical-property thesis; condensed to ~180 lines; benchmark section summarised with key bullet points; full tables remain in `OPERATIONS.md`
- `FUNDING.yml` added under `.github/` — enables GitHub Sponsors button on repo page
- Badges added to README header: CI, Integration, Go Reference, Go Report Card, Go version, License

### Added (health-check-rewrite)

- `BenchmarkAPI_HealthCheck` — measures full integrity scan; O(n) forward-scan implementation; 512 cells → 445 µs, 2000 cells → 1.6 ms

### Changed (health-check-rewrite)

- `DB.HealthCheck` — replaced O(ScanRadius²) `WalkRings`+`GetCell` cell scan with a single `cell/` prefix `AscendRange`; replaced `FindSeams` spatial call with `seam/` primary-key scan (covers all seams, no radius limit); replaced `ListExistingTopics`+`AscendCellsByTag`-per-tag O(tags×cells) loop with single `tag/` family `AscendRange`; replaced `AscendCellsBySource`-per-source loop with single `source/` prefix scan; all `GetCell` presence checks replaced with O(1) `liveCells` map lookup built during the initial cell scan — overall complexity reduced from O(ScanRadius²+tags×n+sources×n) to O(n)
- `HealthCheckConfig.ScanRadius` — deprecated; field retained for backward compatibility but has no effect; cell scan now covers all cells regardless of coordinate

### Added (bench-improvements)

- `CellQuery.MaxScanRows` — additive field to bound the number of index rows examined by `scanByTag` and `scanBySource`; zero = unlimited (existing behaviour unchanged)
- `lattice.RingInto(dst, center, k)` — buffer-reuse variant of `Ring`; eliminates per-ring heap allocation in tight loops; `Ring` unchanged for backward compatibility
- `BenchmarkAPI_BatchPutCells` — batch write throughput benchmark; sizes 10/100/500; reports `cells/op` metric

### Changed (bench-improvements)

- `mortonPack63` — replaced 21-iteration scalar bit loop with 128-entry lookup table (`mortonExpand7`); 3 passes × 7 bits = 21 axis bits; `mortonUnpack63` unchanged (scalar); wire format identical
- `collectCandidates` — pre-sizes `items` and `seen` with `min(3r²+3r+1, capCells)`; reuses a single `ringBuf` via `lattice.RingInto` across ring iterations; eliminates ~7 growth doublings at r=5
- `LoadContextWithBudgeting` eviction loop — O(1) token subtraction instead of O(n) full recalculation after each dropped item
- `AssembleCellView` — removed defensive `Tags` copy (`append([]string(nil), rec.Tags...)`); `CellView.Tags` is read-only post-assembly (all callers confirmed)
- `findSeams` — replaced `lattice.WalkRings` materialisation with inline `for ring / for _, c := range lattice.Ring` two-level loop (lazy iteration); added pre-flight presence check using `SeamByCellsScanUpperBound()` — a single `AscendRange` confirms index is empty, saving 74–182 B+ tree traversals at r=3–5 in seam-free databases

### Changed

- Deleted orphaned `internal/config` package — `config.Load()` had zero callers; `cmd/tui` already handles log level inline
- `views.go` view-assembly logic extracted to `internal/views` — `TxReader` port interface (`GetCell`, `AscendFacetsForCell`, `AscendEdgesFrom`, `FindSeams`) breaks the import cycle; all types (`CellView`, `ContextPack`, `FacetView`, `EdgeView`, `SeamRef`, `CellExplanation`, `ContextPackStats`, `TokenBudgeter`, `ByteLenBudgeter`, `AssembleCellViewOpts`, `LoadContextBudgetConfig`, `CellViewPredicate`) re-exported as type aliases — **zero public API change**; `*Tx` methods are thin wrappers delegating to `internal/views`; `cell_secondary.go`, `seam_secondary.go`, `rotation.go` remain at repo root (reclassified to Future)

### Added

- `BenchmarkAPI_QueryCells` — 4 predicate shapes (tag-only, source-only, spatial, combined) × 2 preload sizes; performance baseline for query engine work
- `BenchmarkAPI_LoadContextPack` — radii 1/3/5 × 2 preload sizes; performance baseline for budgeting work
- `BenchmarkAPI_MVCCVersionResolution` — 10/50/100/500 versions of same coord; isolates `SelectVisible` O(n) scan under realistic MVCC load
- `make demo` Makefile target — runs `examples/conversational_memory` with DB defaulting to `.tmp/demo/memory.db`; override via `make demo DEMO_DB=/path/to/my.db`; DB reused across runs (seed skipped if file exists)
- `conversational_memory` demo expanded — corpus moved to `seed_data.go` (84 turns, 5 thematic sessions: preferences/workflow, HexxlaDB internals, Go patterns, LLM systems, security/ops); `-db` CLI flag for custom DB path; DB defaults to `.tmp/demo/memory.db` (no root pollution); `printSubHeader`/`printNote` helpers; all phase descriptions, metrics, and explain outputs improved for readability; `spiralCoord` widened to 11-column grid for 84-cell corpus; budgets increased to 600/800 bytes; Phase 11 demonstrates `DB.HealthCheck`, `AfterPutCell` telemetry, and `DB.SnapshotDiff` end-to-end

- `Options.MaxValueBytes uint32` — per-database maximum B+ tree value size; accepted values: 512, 1024, 2048, 4096, 8192, 16384 bytes; default 0 = 8192 (8 KB); persisted in the file header; enforced on every write via `BTree.Put`; readable via `DB.MaxValueBytes()`; `ErrInvalidArgument` on invalid value; 9 tests
- `(*DB).MaxValueBytes() uint32` — returns the effective limit read from the file header at `Open`

- `DB.SnapshotDiff(ctx, fromSeq, toSeq, SnapshotDiffConfig) (SnapshotDiff, error)` — MVCC change diff; returns all cell/seam writes in `(fromSeq, toSeq]`; `ErrMVCCRequired` on v1 databases; `ErrReadSeqFuture` if `toSeq` > head; `SnapshotDiff{Cells []CellDiff, Seams []SeamDiff}`; 9 tests
- `ErrMVCCRequired` — new sentinel error

- `AfterPutCellHook` / `AfterPutCellHookFunc` — post-write callback fired after each successful `Tx.PutCell`; error propagates to caller; set via `Options.AfterPutCell`
- `AfterPutSeamHook` / `AfterPutSeamHookFunc` — post-write callback fired after `Tx.PutSeam`, `Tx.MarkConflict`, `Tx.MarkSupersedes`; set via `Options.AfterPutSeam`; 9 tests

- `app.Service` use-case layer completed — all 23 `domain.Storage` port methods now delegated; compile-time interface satisfaction check (`var _ domain.Storage = (*Service)(nil)`) added to catch future drift; `ErrNoStorage` returned by every method when storage port not wired; 2 tests

- `DB.TagSnapshot(label string) error` — pin the current head `CommitSeq` under a human-friendly label; stored in B+ tree under `__meta/snap-tag/<label>`; overwrites existing tag with same name; label max 200 bytes
- `DB.ViewAtTag(label string, fn func(*Tx) error) error` — open a read-only snapshot pinned to the commit recorded by `TagSnapshot`; returns `ErrSnapshotTagNotFound` if label absent
- `DB.ListSnapshotTags() ([]SnapshotTag, error)` — enumerate all tags sorted by label
- `DB.DeleteSnapshotTag(label string) error` — remove a tag entry without affecting underlying data
- `SnapshotTag` — `Label string`, `CommitSeq uint64`
- `ErrSnapshotTagNotFound`, `ErrSnapshotTagLabelTooLong` — new sentinel errors; 11 tests

- `Tx.QueryCells(ctx, CellQuery) ([]CellQueryResult, error)` — composable query engine with index-aware planner; predicates: `Query` (lexical), `RequireTags` (AND), `AnyTags` (OR), `ExcludeTags` (NOT), `SourceID`, `MinConfidence`/`MaxConfidence`, `After`/`Before` (temporal via `time/` week-bucket index), `Center`+`Radius` (spatial), `MaxResults`, `SortBy`, `Explain`; 17 tests
- `CellQuery`, `CellQueryResult`, `SortOrder` — query predicate types; `SortByScore`, `SortByConfidence`, `SortByRecency`, `SortByCoord`
- `SearchCells` refactored to thin wrapper over `QueryCells` — no breaking change
- Temporal Range Queries delivered via `CellQuery.After`/`Before` (closes TODOS.md item)

- `DB.HealthCheck(ctx, HealthCheckConfig) (HealthReport, error)` — integrity scan: visible cell count, seam resolution summary (resolved/unresolved), orphaned seam detection, tag index consistency, source index consistency, MVCC stats snapshot; configurable `ScanRadius` and `MaxErrors`
- `HealthReport`, `HealthCheckConfig`, `DefaultHealthCheckConfig` — types and constructor for health check
- `Tx.SearchCells(ctx, CellSearchConfig) ([]CellSearchResult, error)` — scored full-scan search over visible cells; matches `RawContent` (substring), `Tags` (exact + prefix), `SourceID`; supports `RequireTags` (AND), `AnyTags` (OR), confidence range, spatial radius, and `MaxResults` cap; returns `[]CellSearchResult` sorted by composite score, each carrying a `Coord` for direct use as a context-pack seed
- `CellSearchConfig`, `CellSearchResult` — forward-compatible search API; `Embedding []float32` can be added later without breaking callers
- `Tx.LoadMultiContextPack(ctx, MultiContextConfig) (ContextPack, error)` — expand multiple seed coords, merge resulting cell views under a shared token budget, optionally deduplicate shared-neighbourhood cells; companion to `SearchCells` for multi-seed retrieval
- `MultiContextConfig` — `Centers []Coord`, `MaxR`, `MaxTokens`, `Budgeter`, `AssemblyConfig`, `DeduplicateCoords`
- `SeamTypeSupersedes` constant (`"supersedes"`) for directional supersession seams
- `Tx.MarkSupersedes(superseder, superseded Coord, reason string)` — records that a cell is the current truth and another is stale
- `LoadContextBudgetConfig.FilterSuperseded bool` — when true, `LoadContextWithBudgeting` / `LoadContextPack` walk supersession chains and replace stale cells with their current-truth successors (or exclude them if no live successor exists)
- Cycle detection and depth limit (16 hops) in supersession chain walks
- `CellView.SupersededFrom *Coord` — set when context assembly substituted this cell for a stale one
- `CellExplanation.SupersededBy *Coord` and `Reason: "superseded"` — Explain mode now records superseded exclusions and substitutions
- `conversational_memory` example Phase 4 demonstrates seam-aware assembly visually

## [0.1.0] - 2026-04-24

_First release._

[Unreleased]: https://github.com/hexxla/hexxladb/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/hexxla/hexxladb/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/hexxla/hexxladb/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/hexxla/hexxladb/releases/tag/v0.1.0
