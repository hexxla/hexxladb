# Changelog

## [Unreleased]

### Added

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

[0.1.0]: https://github.com/hexxla/hexxladb/releases/tag/v0.1.0
