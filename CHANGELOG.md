# Changelog

## [Unreleased]

### Added

- `SeamTypeSupersedes` constant (`"supersedes"`) for directional supersession seams
- `Tx.MarkSupersedes(superseder, superseded Coord, reason string)` — records that a cell is the current truth and another is stale
- `LoadContextBudgetConfig.FilterSuperseded bool` — when true, `LoadContextWithBudgeting` / `LoadContextPack` walk supersession chains and replace stale cells with their current-truth successors (or exclude them if no live successor exists)
- Cycle detection and depth limit (16 hops) in supersession chain walks

## [0.1.0] - 2026-04-24

_First release._

[0.1.0]: https://github.com/hexxla/hexxladb/releases/tag/v0.1.0
