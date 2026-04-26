# Roadmap

## v0.1.0 Blockers (Active)

Must complete before release. Core functionality gaps.

_(All blockers resolved — ready to release v0.1.0.)_

## Completed (post-v0.1.0)

Shipped after v0.1.0 release on branch `feat/tier1-search-health`.

- **Database Health Check API** — `DB.HealthCheck(ctx, HealthCheckConfig) (HealthReport, error)`; cell count via ring walk, seam resolution summary, orphaned seam detection, tag/source index consistency, MVCC stats snapshot; `DefaultHealthCheckConfig`; 5 regression tests
- **Content Search** — `Tx.SearchCells(ctx, CellSearchConfig) ([]CellSearchResult, error)`; composite scoring (tag exact +1.0, tag prefix +0.8, content verbatim +0.6, content case-insensitive +0.5, source ID +0.3, confidence bonus); filters: `RequireTags` (AND), `AnyTags` (OR), confidence range, spatial radius, `MaxResults`; forward-compatible (`Embedding []float32` addable without breaking callers); 9 tests
- **Multi-seed context assembly** — `Tx.LoadMultiContextPack` + `MultiContextConfig`; expands N seed coords, merges under shared token budget, cross-seed confidence re-ranking, `DeduplicateCoords`; `Tx.LoadContextPackFrom` unified variadic entry point (dispatches single→`LoadContextPack`, multi→`LoadMultiContextPack` with zero overhead for 1-seed case); 5 tests
- **conversational_memory demo Phase 10** — `SearchCells` → seeds → `LoadContextPackFrom` pipeline demonstrated end-to-end with budget breakdown
- **`app.Service` use-case layer** — all 23 `domain.Storage` port methods delegated in `internal/app/app.go`; compile-time interface satisfaction check (`var _ domain.Storage = (*Service)(nil)`); every method returns `ErrNoStorage` when storage port not wired; 2 tests
- **Snapshot Tags/Labels** — `DB.TagSnapshot(label)`, `DB.ViewAtTag(label, fn)`, `DB.ListSnapshotTags()`, `DB.DeleteSnapshotTag(label)`; `SnapshotTag{Label, CommitSeq}`; tags stored under `__meta/snap-tag/<label>` in B+ tree; persist across reopen; `ErrSnapshotTagNotFound` / `ErrSnapshotTagLabelTooLong`; 11 tests
- **Composable Query Engine** — `Tx.QueryCells(ctx, CellQuery) ([]CellQueryResult, error)`; index-aware planner (tag, source, time, spatial, full-scan fallback); predicates: lexical, `RequireTags`/`AnyTags`/`ExcludeTags`, `SourceID`, confidence range, `After`/`Before` (temporal via `time/` bucket index), spatial radius, `MaxResults`, `SortBy` (score/confidence/recency/coord), `Explain`; `SearchCells` refactored to thin wrapper; Temporal Range Queries delivered; 17 tests

## Completed (v0.1.0)

Shipped with v0.1.0 release.

- **Seam-aware context assembly** — `SeamTypeSupersedes`, `Tx.MarkSupersedes`, `LoadContextBudgetConfig.FilterSuperseded`; walks supersession chains, replaces stale cells with current truth, excludes cells with no live successor; cycle detection at 16 hops; `CellView.SupersededFrom`, `CellExplanation.SupersededBy/Reason:"superseded"` for full observability; `API_REFERENCE.md` updated; `conversational_memory` demo Phase 4 added
- **Increase max cell value to 8KB** — 8192 bytes handles typical prompts/conversation turns; updated `btree_page.go`, `API_REFERENCE.md` (no format_version bump needed - runtime validation only)
- **Rename `internal/mvccspike` → `internal/mvcc`** — promoted production MVCC visibility algorithm to stable package name ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Extract prune profile helper** — `profileToMaxDelete` deduplicates `MVCCPrunePlan` and `PruneCellVersionsByProfile` switch blocks ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Move MVCC key validation out of `Tx.Put`** — added `putDirect` for internal primitives; MVCC cell key guard stays on public `Tx.Put` only ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- **Refactor `goto assembled`** — extracted `collectCandidates` helper from `LoadContextWithBudgeting` for cleaner control flow and isolated testability ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
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

_(Empty — all quick wins shipped or reclassified.)_

## Near-term

Requires design + benchmarks before implementation.

- Per-database MaxValueBytes — store limit in engine header (default 8KB), expose in Options for 2KB/4KB/16KB; requires header format change and migration path (reclassified from Quick Wins)
- Relocate secondary index files to `internal/` — `cell_secondary.go` and `seam_secondary.go` are methods on `*Tx`; moving to `internal/` creates import cycle same as `rotation.go`; needs interface extraction first (reclassified from Quick Wins; btree coupling already fixed via `deleteDirect`)
- Extract `views.go` to `internal/views` or `internal/app` — `LoadContextWithBudgeting` calls `tx.GetCell`/`tx.AscendRange`, so it's not pure app-layer; moving requires interface extraction to break import cycle (reclassified from Quick Wins)
- Move `rotation.go` to `internal/tooling/rotation` — rotation uses `DB.Open`, `Tx.putDirect`, error sentinels; moving to `internal/` creates an import cycle; needs interface extraction first ([audit](./context/audits/SOC_MODULARITY_AUDIT.md))
- Event Hooks / Callbacks — react to cell writes, seam detection, facet rotation (needs architecture RFC)

## Future

Spec exists; implementation deferred.

- `embed/` keyspace for ANN/hybrid retrieval — vector storage and similarity search for semantic seed selection ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md)). When implemented, `CellSearchConfig.Embedding []float32` field will be added to Content Search API — existing `Query string` callers unaffected.
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy
- Changelog Subscription (push mode) — real-time reactions via channels
- MVCC Snapshot Diff — compare state between two `CommitSeq` values
- Cell Relationship Graph Export — nodes/edges/seams for external analysis
- Confidence Decay Policy — time-based confidence reduction with audit trail

## Future exploration

Interesting but unvalidated. Needs user demand or benchmark data before committing.

- Hot Cell Tracking — LRU-based access frequency tracking for cache warming (overhead concerns)
- Content Compression — gzip/zstd compression for large cells >512B (benchmark first)
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

| Date       | Scope                                                                                                                                                                                                                                                               |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-24 | v0.1.0 initial release                                                                                                                                                                                                                                              |
| 2026-04-24 | Roadmap consolidated to priority-based format                                                                                                                                                                                                                       |
| 2026-04-24 | Added HEXXLA service quick wins from audit (QueryStats, RingDensity, Templates, Bulk ops, Health check, Hooks)                                                                                                                                                      |
| 2026-04-24 | **v0.1.0 scope locked:** 8KB cell size increase as release blocker                                                                                                                                                                                                  |
| 2026-04-24 | **v0.1.0 scope updated:** seam-aware context assembly added as release blocker — contradictions must be actionable                                                                                                                                                  |
| 2026-04-24 | **SoC audit validated:** mvccspike rename, prune profile dedup, MVCC key guard relocation added as v0.1.0 blockers; secondary index relocation, views.go extraction, app.Service completion added as Quick Wins ([audit](./context/audits/SOC_MODULARITY_AUDIT.md)) |
| 2026-04-25 | Seam-aware context assembly shipped: `SeamTypeSupersedes`, `MarkSupersedes`, `FilterSuperseded`; all v0.1.0 blockers resolved                                                                                                                                       |
| 2026-04-25 | Seam observability additions: `CellView.SupersededFrom`, `CellExplanation.SupersededBy`, `Reason:"superseded"`; API_REFERENCE and demo updated                                                                                                                      |
| 2026-04-26 | Tier 1 features shipped: Health Check API, Content Search (`SearchCells`), Multi-seed assembly (`LoadMultiContextPack`, `LoadContextPackFrom`); 19 tests; API_REFERENCE, CHANGELOG, ROADMAP, TODOS updated                                                          |
| 2026-04-26 | `app.Service` use-case layer completed: all 23 `domain.Storage` delegations; compile-time interface check; 2 tests                                                                                                                                                  |
| 2026-04-26 | Snapshot Tags/Labels shipped: `DB.TagSnapshot`, `ViewAtTag`, `ListSnapshotTags`, `DeleteSnapshotTag`; `__meta/snap-tag/` B+ tree key prefix; 11 tests                                                                                                               |
| 2026-04-26 | Composable Query Engine shipped: `Tx.QueryCells`, `CellQuery`, temporal/spatial/tag/confidence predicates, `SortOrder`, `Explain`; `SearchCells` wrapper; Temporal Range Queries closed; 17 tests                                                                   |
