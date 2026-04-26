# Active Work

Immediate next steps. Update after each session.

## Current

- [ ] Ready for v0.2.0 / v0.3.0 release push to remote

## Pending (next sessions)

- [ ] `Tx.DeleteCell` — primary + secondaries + facets + edges; MVCC tombstone on v2; regression tests
- [ ] `DB.Compact` — copy-compaction to shrink file; `AscendRange` walk → fresh BTree → atomic swap; no lattice reorg needed
- [ ] Monitor for v1.0.0 graduation criteria (per VERSIONING.md)

### Benchmark-identified concerns (added 2026-04-26)

- [ ] **`FindSeams` empty-neighbourhood cost** — 2.3 ms with zero seams (full ring scan runs regardless); profile to find exact hot spot before designing fix (bloom filter / presence flag are candidates)
- [ ] **`LoadContextPack` allocations at large radii** — r=5/2000 cells: 2.28 MB / 11,756 allocs per call; pre-size candidate slice with `lattice.RingArea(r)` and evaluate `sync.Pool` for decode buffers
- [ ] **`QueryCells` source scan is O(n) unbounded** — 10 ms/512 cells → 54 ms/2000 cells (5.2× for 4× data); consider `MaxScanRows` field on `CellQuery` to bound worst-case latency
- [ ] **`BenchmarkAPI_BatchPutCells` missing** — `PutCell` at 8.3 ms/op is single-fsync cost; `BatchPutCells` amortises but has no bench; add sizes 10/100/500

---

## Recently Completed

- 2026-04-26: LEAN quick wins — deleted orphaned `internal/config` package (zero callers); added `BenchmarkAPI_QueryCells` (4 predicate shapes × 2 sizes), `BenchmarkAPI_LoadContextPack` (radii 1/3/5 × 2 sizes), `BenchmarkAPI_MVCCVersionResolution` (10/50/100/500 versions) to `api_bench_test.go`

- 2026-04-26: `views.go` extracted to `internal/views` — `TxReader` port (4-method interface); `AssembleCellView`, `LoadContextWithBudgeting`, `collectCandidates`, `resolveSupersession`, `LoadMultiContextPack` moved; root `views.go` is type aliases + thin `*Tx` wrappers; zero API break; `cell_secondary.go`/`seam_secondary.go`/`rotation.go` reclassified to Future
- 2026-04-26: Demo expansion — `seed_data.go` (84 turns, 5 sessions); `-db` flag; `make demo` Makefile target; `printSubHeader`/`printNote` helpers; aesthetic + readability pass all 11 phases; DB defaults to `.tmp/demo/memory.db` (no root pollution); `spiralCoord` 11-column grid; API_REFERENCE "Live demos" updated
- 2026-04-26: Per-database MaxValueBytes — `Options.MaxValueBytes` (512/1024/2048/4096/8192/16384); persisted in header at offset 100; `DB.MaxValueBytes()`; `ErrInvalidArgument` on invalid value; 9 tests; API_REFERENCE + CHANGELOG + ROADMAP updated
- 2026-04-26: MVCC Snapshot Diff — `DB.SnapshotDiff`; `SnapshotDiff`/`CellDiff`/`SeamDiff`/`DiffOp`/`SnapshotDiffConfig`; `ErrMVCCRequired`; scans MVCC version keys for (fromSeq, toSeq]; 9 tests; API_REFERENCE + CHANGELOG + ROADMAP updated
- 2026-04-26: Event Hooks — `AfterPutCellHook`/`AfterPutCellHookFunc` + `AfterPutSeamHook`/`AfterPutSeamHookFunc`; `Options.AfterPutCell`/`AfterPutSeam`; fires after `PutCell`, `PutSeam`, `MarkConflict`, `MarkSupersedes`; error propagates; 9 tests; API_REFERENCE + CHANGELOG + ROADMAP updated
- 2026-04-26: `app.Service` use-case layer completed — all 23 `domain.Storage` port methods delegated; compile-time interface check; 2 tests
- 2026-04-26: Snapshot Tags/Labels — `DB.TagSnapshot`/`ViewAtTag`/`ListSnapshotTags`/`DeleteSnapshotTag`; `SnapshotTag` type; `ErrSnapshotTagNotFound`/`ErrSnapshotTagLabelTooLong`; stored under `__meta/snap-tag/<label>` in B+ tree; persists across reopen; 11 tests; API_REFERENCE + CHANGELOG + ROADMAP updated
- 2026-04-26: Composable Query Engine (`feat/cell-query`) — `Tx.QueryCells` + `CellQuery`/`CellQueryResult`/`SortOrder`; predicates: lexical, RequireTags/AnyTags/ExcludeTags, SourceID, confidence range, temporal After/Before (time/ index, no full scan), spatial radius, MaxResults, SortBy (score/confidence/recency/coord), Explain; `SearchCells` refactored to thin wrapper; Temporal Range Queries delivered; 17 tests; API_REFERENCE + CHANGELOG updated
- 2026-04-26: Tier 1 features — `DB.HealthCheck` + `HealthReport`/`HealthCheckConfig`; `Tx.SearchCells` + `CellSearchConfig`/`CellSearchResult` (composite scoring, tag/content/source/spatial filters, forward-compatible for embeddings); `Tx.LoadMultiContextPack` + `MultiContextConfig` (multi-seed, shared budget, deduplication); `Tx.LoadContextPackFrom` (unified variadic: 1 coord → LoadContextPack, N coords → LoadMultiContextPack, zero overhead); demo Phase 10; 19 tests; API_REFERENCE + CHANGELOG + ROADMAP updated; branch feat/tier1-search-health
- 2026-04-25: Seam-aware API surface + demo — `CellView.SupersededFrom`, `CellExplanation.SupersededBy`, `Reason:"superseded"`, API_REFERENCE updated, conversational_memory demo Phase 4 added
- 2026-04-25: Seam-aware context assembly (v0.1.0 blocker resolved) — `SeamTypeSupersedes`, `MarkSupersedes`, `resolveSupersession`, `LoadContextBudgetConfig.FilterSuperseded`; 4 regression tests
- 2026-04-24: Increased max cell value to 8KB (v0.1.0 blocker resolved)
- 2026-04-24: Added tag discovery API to conversational example
- 2026-04-24: Added comparison section to README (vs vector/graph/temporal DBs)
- 2026-04-24: Identified seam-aware context assembly as v0.1.0 blocker
- 2026-04-24: SoC audit validated; mvccspike rename, prune dedup, MVCC key guard added as v0.1.0 blockers
- 2026-04-24: Completed mvccspike→mvcc rename, prune profile dedup, MVCC key guard relocation
- 2026-04-24: Refactored goto assembled into collectCandidates helper
- 2026-04-24: Closed commit-time meta-key finding as false (already in correct location in DB.Update)
- 2026-04-24: Quick wins batch: Cell Template Factory, Tag Analytics, RingDensity API, Filtered Changelog, Cell Validation Hooks
- 2026-04-24: Deferred rotation.go move (import cycle requires interface extraction first)
- 2026-04-24: Quick wins batch 2: ASCII Hex Renderer, Batch PutCell, QueryStats, Explain Mode, Bulk JSON I/O, API docs update
- 2026-04-24: Fixed secondary index btree coupling: added `tx.deleteDirect`, replaced direct `tx.db.btree.Delete` calls
- 2026-04-24: Reclassified MaxValueBytes, secondary index relocation, views.go extraction to Near-term (all need design/interface extraction)

---

## Usage Notes

- **Active**: Work in progress or next immediate task
- **Pending**: Backlog for future sessions
- Move items to ROADMAP.md when they become formal roadmap features
- Create GitHub issues for bugs or external collaboration needs
- This file is intentionally lightweight and disposable
