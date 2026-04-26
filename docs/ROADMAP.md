# Roadmap

## v0.1.0 Blockers (Active)

Must complete before release. Core functionality gaps.

_(All blockers resolved — ready to release v0.1.0.)_

## v0.2.0 Candidate

All items below are shipped and in `[Unreleased]`. Ready to tag once remote push is done.

- **Demo expansion** — `seed_data.go` (84 turns, 5 thematic sessions); `-db` CLI flag; `make demo` Makefile target (default `.tmp/demo/memory.db`, override via `DEMO_DB`); `printSubHeader`/`printNote` helpers; readability pass across all 11 phases; `spiralCoord` widened to 11-column grid
- **Per-database MaxValueBytes** — `Options.MaxValueBytes`, `DB.MaxValueBytes()`, header offset 100; 9 tests
- **MVCC Snapshot Diff** — `DB.SnapshotDiff`, `CellDiff`/`SeamDiff`/`DiffOp`/`SnapshotDiffConfig`, `ErrMVCCRequired`; 9 tests
- **Event Hooks** — `AfterPutCellHook`/`AfterPutSeamHook` + `Func` adapters, `Options.AfterPutCell`/`AfterPutSeam`; 9 tests
- **`app.Service` completion** — all 23 `domain.Storage` delegations; compile-time interface check; 2 tests
- **Snapshot Tags/Labels** — `DB.TagSnapshot`/`ViewAtTag`/`ListSnapshotTags`/`DeleteSnapshotTag`; 11 tests
- **Composable Query Engine** — `Tx.QueryCells`, temporal/spatial/tag/confidence predicates, `SortOrder`, `Explain`; 17 tests

## Completed (post-v0.1.0)

Shipped after v0.1.0 release on branch `feat/tier1-search-health`.

- **Database Health Check API** — `DB.HealthCheck(ctx, HealthCheckConfig) (HealthReport, error)`; cell count via ring walk, seam resolution summary, orphaned seam detection, tag/source index consistency, MVCC stats snapshot; `DefaultHealthCheckConfig`; 5 regression tests
- **Content Search** — `Tx.SearchCells(ctx, CellSearchConfig) ([]CellSearchResult, error)`; composite scoring (tag exact +1.0, tag prefix +0.8, content verbatim +0.6, content case-insensitive +0.5, source ID +0.3, confidence bonus); filters: `RequireTags` (AND), `AnyTags` (OR), confidence range, spatial radius, `MaxResults`; forward-compatible (`Embedding []float32` addable without breaking callers); 9 tests
- **Multi-seed context assembly** — `Tx.LoadMultiContextPack` + `MultiContextConfig`; expands N seed coords, merges under shared token budget, cross-seed confidence re-ranking, `DeduplicateCoords`; `Tx.LoadContextPackFrom` unified variadic entry point (dispatches single→`LoadContextPack`, multi→`LoadMultiContextPack` with zero overhead for 1-seed case); 5 tests
- **conversational_memory demo Phase 10** — `SearchCells` → seeds → `LoadContextPackFrom` pipeline demonstrated end-to-end with budget breakdown
- **`views.go` extraction to `internal/views`** — narrow `TxReader` port interface; `AssembleCellView`, `LoadContextWithBudgeting`, `collectCandidates`, `resolveSupersession`, `LoadMultiContextPack` moved to `internal/views`; root `views.go` reduced to type aliases + thin `*Tx` wrappers; zero public API break; CI + hex boundary checks pass
- **Per-database MaxValueBytes** — `Options.MaxValueBytes uint32`; accepted values 512/1024/2048/4096/8192/16384; default 8192 (8 KB); persisted in file header at offset 100 (unconditional, all format versions); enforced in `BTree.Put` via `Engine.maxValueBytes`; readable via `DB.MaxValueBytes()`; `ErrInvalidArgument` on invalid value; 9 tests
- **MVCC Snapshot Diff** — `DB.SnapshotDiff(ctx, fromSeq, toSeq, SnapshotDiffConfig) (SnapshotDiff, error)`; scans MVCC version keys for `(fromSeq, toSeq]`; `CellDiff`/`SeamDiff`/`DiffOp`; `ErrMVCCRequired` on v1 databases; `SnapshotDiffConfig{IncludeCells, IncludeSeams *bool}`; `ErrMVCCRequired` sentinel; 9 tests
- **Event Hooks** — `AfterPutCellHook`/`AfterPutCellHookFunc` (`Options.AfterPutCell`) fires after `Tx.PutCell`; `AfterPutSeamHook`/`AfterPutSeamHookFunc` (`Options.AfterPutSeam`) fires after `Tx.PutSeam`, `Tx.MarkConflict`, `Tx.MarkSupersedes`; error propagates; nil hook is zero-cost; 9 tests
- **`app.Service` use-case layer** — all 23 `domain.Storage` port methods delegated in `internal/app/app.go`; compile-time interface satisfaction check (`var _ domain.Storage = (*Service)(nil)`); every method returns `ErrNoStorage` when storage port not wired; 2 tests
- **Snapshot Tags/Labels** — `DB.TagSnapshot(label)`, `DB.ViewAtTag(label, fn)`, `DB.ListSnapshotTags()`, `DB.DeleteSnapshotTag(label)`; `SnapshotTag{Label, CommitSeq}`; tags stored under `__meta/snap-tag/<label>` in B+ tree; persist across reopen; `ErrSnapshotTagNotFound` / `ErrSnapshotTagLabelTooLong`; 11 tests
- **Composable Query Engine** — `Tx.QueryCells(ctx, CellQuery) ([]CellQueryResult, error)`; index-aware planner (tag, source, time, spatial, full-scan fallback); predicates: lexical, `RequireTags`/`AnyTags`/`ExcludeTags`, `SourceID`, confidence range, `After`/`Before` (temporal via `time/` bucket index), spatial radius, `MaxResults`, `SortBy` (score/confidence/recency/coord), `Explain`; `SearchCells` refactored to thin wrapper; Temporal Range Queries delivered; 17 tests

## Completed (v0.1.0)

Shipped with v0.1.0 release.

- **Seam-aware context assembly** — `SeamTypeSupersedes`, `Tx.MarkSupersedes`, `LoadContextBudgetConfig.FilterSuperseded`; walks supersession chains, replaces stale cells with current truth, excludes cells with no live successor; cycle detection at 16 hops; `CellView.SupersededFrom`, `CellExplanation.SupersededBy/Reason:"superseded"` for full observability; `API_REFERENCE.md` updated; `conversational_memory` demo Phase 4 added
- **Increase max cell value to 8KB** — 8192 bytes handles typical prompts/conversation turns; updated `btree_page.go`, `API_REFERENCE.md` (no format_version bump needed - runtime validation only)
- **Rename `internal/mvccspike` → `internal/mvcc`** — promoted production MVCC visibility algorithm to stable package name
- **Extract prune profile helper** — `profileToMaxDelete` deduplicates `MVCCPrunePlan` and `PruneCellVersionsByProfile` switch blocks
- **Move MVCC key validation out of `Tx.Put`** — added `putDirect` for internal primitives; MVCC cell key guard stays on public `Tx.Put` only
- **Refactor `goto assembled`** — extracted `collectCandidates` helper from `LoadContextWithBudgeting` for cleaner control flow and isolated testability
- **Cell Template Factory** — `NewUserMessageCell`, `NewAssistantResponseCell`, `NewSystemPromptCell`, `NewFactCell` in `templates.go`
- **Tag Analytics** — `TagCounts`, `TagCooccurrences`, `UntaggedCells` in `tag_analytics.go`
- **RingDensity API** — `RingDensityMap`, `TotalDensity` in `ring_density.go`
- **Filtered Changelog Reading** — `ReadChangelogFiltered` with `ChangelogFilter` (op codes + key prefix) in `db_changelog.go`
- **Cell Validation Hooks** — `CellValidator` interface + `CellValidatorFunc` adapter on `Options.CellValidator`, wired into `PutCell`
- **ASCII Hex Grid Renderer** — `RenderHexGrid` + `RenderHexGridFromDB` in `hex_render.go`
- **Batch PutCell with Progress** — `BatchPutCells` with `BatchPutCellOptions` (batch size, progress, continue-on-error) in `batch_put.go`
- **QueryStats on ContextPack** — `ContextPackStats` (candidates, evicted, max ring) on `ContextPack.Stats`
- **Context Pack Explain Mode** — `CellExplanation` per-cell reasons via `LoadContextBudgetConfig.Explain`
- **Bulk Cell Import/Export (JSON)** — `ExportCellsJSON` + `ImportCellsJSON` in `bulk_io.go`
- **Secondary index btree coupling fix** — added `tx.deleteDirect` mirroring `tx.putDirect`; `cell_secondary.go` and `seam_secondary.go` no longer reach through to `tx.db.btree.Delete` directly

## Quick Wins

Low effort, high value. No design required.

- **Inline `internal/config` into `cmd/tui`** — `internal/config/config.go` is a 29-line single-file package used only by `cmd/tui`; inline `Load()` directly into `cmd/tui/main.go` to eliminate the unnecessary internal package boundary ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- **Benchmark additions** — add `QueryCells` benchmarks with varying predicate complexity, `LoadContextPack` with varying radii, and MVCC version resolution with high version counts; baselines needed before any performance work ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))

## Near-term

- **`Tx.DeleteCell`** — remove a cell and all its associated data atomically: primary key (`cell/<packed>`), all secondary indexes (`source/`, `time/`, `tag/`), all facets (`facet/<packed>/<id>`), all edges (`edge/<packed>/...`). Seam cleanup is caller-controlled (seams reference two cells; deleting one endpoint does not auto-resolve the seam — caller should `ResolveSeam` or let the orphan surface via `HealthCheck`). New sentinel: `ErrCellNotFound` on delete of a missing cell (or silent no-op — needs decision). MVCC: on v2 databases, the primary key is tombstoned rather than hard-deleted so `as_of` snapshots before the delete remain consistent. Regression tests required covering: primary removal, secondary index cleanup, facet/edge cleanup, MVCC tombstone behaviour.
- **`DB.Compact`** — offline copy-compaction that shrinks the database file to the minimum size needed for live data. Walks all live B+ tree keys via `AscendRange`, writes them sequentially into a fresh file using a new `BTree`, then atomically swaps. No lattice reorganisation required — hex coordinates are encoded in keys (Morton-packed), not page positions; a page-level rewrite is sufficient. Should honour `Options` (encryption, `MaxValueBytes`). API: `DB.Compact(ctx, destPath string, opts *Options) error` or `DB.CompactInPlace(ctx) error` (write to temp file, rename). Produces an identical-content but smaller database; safe to run offline (caller closes DB first) or online with a read lock depending on design chosen.

## Future

Spec exists; implementation deferred.

- **Relocate `cell_secondary.go` / `seam_secondary.go` to `internal/`** — both files contain no exported symbols and only unexported `*Tx` methods; btree coupling is already cleanly abstracted via `putDirect`/`deleteDirect`; a `TxIndexWriter` interface would add indirection with no architectural gain at this point; reclassified from Near-term.
- **Move `rotation.go` to `internal/tooling/rotation`** — uses `DB.Open`, `Tx.putDirect`, root error sentinels; cycle is hard to break without significant restructuring or exposing `UnsafePut`; in-root placement is not architecturally wrong; reclassified from Near-term.
- `embed/` keyspace for ANN/hybrid retrieval — vector storage and similarity search for semantic seed selection ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md)). When implemented, `CellSearchConfig.Embedding []float32` field will be added to Content Search API — existing `Query string` callers unaffected.
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy
- Changelog Subscription (push mode) — real-time reactions via channels
- Cell Relationship Graph Export — nodes/edges/seams for external analysis
- Confidence Decay Policy — time-based confidence reduction with audit trail
- **MVCC version chain optimisation** — for cells with many versions (>100), the current O(n) linear scan in `SelectVisible` may become a bottleneck; consider skip list or tree structure; defer until profiling shows this is a real hot path ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- **`domain.Storage` interface contract tests** — `internal/domain/storage_test.go` with a fake implementation to validate port contracts independently of the adapter; low urgency since `internal/app` tests cover the interface indirectly ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- Hot Cell Tracking — LRU-based access frequency tracking for cache warming (overhead concerns)
- Content Compression — gzip/zstd compression for large cells >512B (benchmark first)
- **Record encoding allocation reduction** — `AppendEnvelope` in `internal/record` allocates a fresh buffer on every encode; pre-sizing via pool or capacity hint could reduce GC pressure under write-heavy workloads; needs benchmark validation before committing ([LEAN audit](./context/audits/LEAN_ARCHITECTURE_AUDIT.md))
- Edge Weight Decay — connections strengthen with traversal, weaken with disuse (speculative)
- Facet Diff/Compare — see what changed between facet versions (audit utility)
- Shortest Path Between Cells — graph traversal via edges (BFS implementation)

## Out of Scope

Intentional boundaries for embedded library v1.

- Distributed replication / HA — product-tier orchestration
- Freelist / primary file shrink — extend-only allocator by design ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md))
- Online re-encryption — offline rotation only ([`VERSIONING.md`](../VERSIONING.md))
- Third-party KV backends (SQLite, etc.) — Hex-native engine is the direction

---

## Audit Log

| Date       | Scope                                                                                                                                                                                                           |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | v0.1.0 initial release                                                                                                                                                                                          |
| 2026-04-24 | Roadmap consolidated to priority-based format                                                                                                                                                                   |
| 2026-04-24 | Added HEXXLA service quick wins from audit (QueryStats, RingDensity, Templates, Bulk ops, Health check, Hooks)                                                                                                  |
| 2026-04-24 | **v0.1.0 scope locked:** 8KB cell size increase as release blocker                                                                                                                                              |
| 2026-04-24 | **v0.1.0 scope updated:** seam-aware context assembly added as release blocker — contradictions must be actionable                                                                                              |
| 2026-04-24 | **SoC audit validated:** mvccspike rename, prune profile dedup, MVCC key guard relocation added as v0.1.0 blockers; secondary index relocation, views.go extraction, app.Service completion added as Quick Wins |
| 2026-04-25 | Seam-aware context assembly shipped: `SeamTypeSupersedes`, `MarkSupersedes`, `FilterSuperseded`; all v0.1.0 blockers resolved                                                                                   |
| 2026-04-25 | Seam observability additions: `CellView.SupersededFrom`, `CellExplanation.SupersededBy`, `Reason:"superseded"`; API_REFERENCE and demo updated                                                                  |
| 2026-04-26 | Tier 1 features shipped: Health Check API, Content Search (`SearchCells`), Multi-seed assembly (`LoadMultiContextPack`, `LoadContextPackFrom`); 19 tests; API_REFERENCE, CHANGELOG, ROADMAP, TODOS updated      |
| 2026-04-26 | Per-database MaxValueBytes shipped: `Options.MaxValueBytes`, `DB.MaxValueBytes()`, header offset 100, engine enforcement; 9 tests                                                                               |
| 2026-04-26 | MVCC Snapshot Diff shipped: `DB.SnapshotDiff`, `SnapshotDiff`/`CellDiff`/`SeamDiff`/`DiffOp`/`SnapshotDiffConfig`, `ErrMVCCRequired`; 9 tests                                                                   |
| 2026-04-26 | Event Hooks shipped: `AfterPutCellHook`, `AfterPutSeamHook`, `Func` adapters, `Options.AfterPutCell`/`AfterPutSeam`; 9 tests                                                                                    |
| 2026-04-26 | `app.Service` use-case layer completed: all 23 `domain.Storage` delegations; compile-time interface check; 2 tests                                                                                              |
| 2026-04-26 | Snapshot Tags/Labels shipped: `DB.TagSnapshot`, `ViewAtTag`, `ListSnapshotTags`, `DeleteSnapshotTag`; `__meta/snap-tag/` B+ tree key prefix; 11 tests                                                           |
| 2026-04-26 | Composable Query Engine shipped: `Tx.QueryCells`, `CellQuery`, temporal/spatial/tag/confidence predicates, `SortOrder`, `Explain`; `SearchCells` wrapper; Temporal Range Queries closed; 17 tests               |
| 2026-04-26 | Demo expansion: `seed_data.go` 84-turn corpus, 5 sessions; `-db` flag; `make demo` target; `printSubHeader`/`printNote`; 11-column `spiralCoord`; readability pass all phases; API_REFERENCE updated            |
| 2026-04-26 | v0.2.0 candidate locked: all Unreleased items shipped; ready to tag                                                                                                                                             |
| 2026-04-26 | `views.go` extracted to `internal/views`: `TxReader` port breaks import cycle; type aliases preserve public API; `cell_secondary.go`/`seam_secondary.go`/`rotation.go` reclassified to Future                   |
