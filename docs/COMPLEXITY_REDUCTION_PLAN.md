# Complexity Reduction Plan for hexxladb

**Branch:** `refactor/complexity-reduction`
**Date:** 2026-05-07 (last updated: 2026-05-07)
**Thresholds:** (from template, kept as-is for good code quality)

- Domain: cyclomatic 5, cognitive 10
- App: cyclomatic 10, cognitive 15
- Adapters In: cyclomatic 15, cognitive 20
- Adapters Out: cyclomatic 12, cognitive 18
- Pkg Root: cyclomatic 12, cognitive 18
- Cmd: cyclomatic 15, cognitive 20
- Default: cyclomatic 10, cognitive 15
- CRAP threshold: 30

---

## Current State (post-refactoring round 1)

**Before:** 244 violations, 138 functions, 8 CRAP violations
**After:** 243 violations, 141 functions, 4 CRAP violations

Key improvements from round 1:

- `health.go` `HealthCheck`: Cyclo 57→14, Cog 163→27 ✅
- `internal/engine/engine.go` `Open`: Cyclo 62→15, Cog 81→(eliminated from top) ✅
- `internal/hnsw/graph.go` `Insert`: Cyclo 44→15, Cog 93→16 ✅
- `db_open.go` `buildEngineOptions`: Cyclo 35→14, Cog 47→(eliminated from top) ✅
- `internal/hnsw/graph.go` `Search`: Cyclo 22→14 (reused `greedyDescend`) ✅

Note: Total violation count stayed similar because extracted helpers each contribute
smaller violations, but the _maximum per-function_ complexity dropped dramatically.

---

## Priority 1: Remaining CRAP Score Violations (4 functions)

**CRAP = (cyclomatic² × (1 - coverage/100)³) + cyclomatic**

| #   | File                                               | Function              | CRAP | Cyclo | Status         |
| --- | -------------------------------------------------- | --------------------- | ---- | ----- | -------------- |
| 1   | `examples/conversational_memory/main.go`           | `run`                 | 114  | 114   | Pending (demo) |
| 2   | `internal/domain/storagecontract/contract_test.go` | `RunAll`              | 96   | 96    | Pending (test) |
| 3   | `examples/llm_context_engine/main.go`              | `run`                 | 49   | 49    | Pending (demo) |
| 4   | `cmd/tui/view_cells.go`                            | `(*cellsView).Update` | 31   | 31    | **Next**       |

### Completed (round 1):

- ~~`internal/engine/engine.go` `Open` — CRAP 62~~ → Cyclo 15 ✅
- ~~`health.go` `HealthCheck` — CRAP 57~~ → Cyclo 14 ✅
- ~~`internal/hnsw/graph.go` `Insert` — CRAP 44~~ → Cyclo 15 ✅
- ~~`db_open.go` `buildEngineOptions` — CRAP 35~~ → Cyclo 14 ✅

---

## Priority 2: Next Refactoring Candidates (non-test, non-example production code)

### Tier A: Cognitive >40 + Cyclomatic >20 (highest impact)

| #   | File                           | Function                   | Cyclo | Cog | Strategy                    |
| --- | ------------------------------ | -------------------------- | ----- | --- | --------------------------- |
| 1   | `internal/hnsw/graph.go`       | `removeNode`               | 24    | 55  | Extract link-repair helpers |
| 2   | `primitives.go`                | `findSeams`                | 23    | 55  | Extract scan + filter logic |
| 3   | `internal/views/budget.go`     | `collectCandidates`        | 14    | 45  | Extract scoring/filter      |
| 4   | `internal/views/budget.go`     | `LoadContextWithBudgeting` | 30    | 44  | Extract phase helpers       |
| 5   | `cmd/tui/view_cells.go`        | `(*cellsView).Update`      | 31    | 43  | Extract msg handlers        |
| 6   | `internal/engine/engine.go`    | `readPagePooled`           | 22    | 39  | Extract decrypt/decompress  |
| 7   | `embedding_search.go`          | `SearchByEmbedding`        | 25    | 38  | Extract flat-scan path      |
| 8   | `internal/engine/group_wal.go` | `applyGroupBatch`          | 24    | 37  | Extract apply/notify phases |

### Tier B: Cognitive 30-39 (moderate impact)

| #   | File                       | Function                      | Cyclo | Cog | Strategy                |
| --- | -------------------------- | ----------------------------- | ----- | --- | ----------------------- |
| 9   | `cmd/tui/view_cells.go`    | `View`                        | 25    | 36  | Extract render sections |
| 10  | `internal/views/views.go`  | `AssembleCellView`            | 22    | 36  | Extract assembly steps  |
| 11  | `rotation.go`              | `RotateEncryptionWithOptions` | 22    | 36  | Extract rewrite/verify  |
| 12  | `internal/engine/btree.go` | `GetUsingRoot`                | 14    | 34  | Extract page navigation |
| 13  | `internal/engine/btree.go` | `AscendRange`                 | 17    | 34  | Extract cursor logic    |
| 14  | `internal/views/budget.go` | `resolveSupersession`         | 15    | 34  | Extract walk/mark       |
| 15  | `hex_render.go`            | `RenderHexGrid`               | 19    | 34  | Extract row/cell render |
| 16  | `primitives.go`            | `putSeamWithOp`               | 24    | 31  | Extract index updates   |
| 17  | `primitives.go`            | `WalkRingFacets`              | 19    | 31  | Extract facet iteration |

### Tier C: B-Tree internals (algorithmic — handle with care)

| #   | File                              | Function              | Cyclo | Cog | Strategy                    |
| --- | --------------------------------- | --------------------- | ----- | --- | --------------------------- |
| 18  | `internal/engine/btree_delete.go` | `rebalanceLeaf`       | 24    | 49  | Extract merge/redistribute  |
| 19  | `internal/engine/btree_delete.go` | `Delete`              | 23    | 26  | Extract leaf/internal cases |
| 20  | `internal/engine/btree_delete.go` | `removeInternalChild` | 16    | 23  | Extract sibling logic       |

---

## Priority 3: Remaining Cyclomatic >15 (production code only)

| File                          | Function               | Cyclo | Cog |
| ----------------------------- | ---------------------- | ----- | --- |
| `query_exec.go`               | `applyPredicates`      | 23    | 28  |
| `query_exec.go`               | `QueryCells`           | 23    | 25  |
| `db.go`                       | `Open`                 | 21    | 26  |
| `mvcc_lifecycle.go`           | `PruneCellVersions`    | 21    | 29  |
| `primitives.go`               | `PutCell`              | 20    | 28  |
| `tx.go`                       | `(*DB).Update`         | 18    | 23  |
| `internal/engine/writetxn.go` | `CommitWriteTxn`       | 18    | 21  |
| `embedding_reindex.go`        | `ReindexEmbeddings`    | 17    | 29  |
| `internal/engine/btree.go`    | `AscendRange`          | 17    | 34  |
| `cmd/tui/view_seams.go`       | `(*seamsView).View`    | 17    | 20  |
| `internal/hnsw/graph.go`      | `searchLayer`          | 16    | 28  |
| `internal/record/seam.go`     | `decodeSeamPayloadV1`  | 16    | —   |
| `internal/engine/btree.go`    | `insertIntoLeaf`       | 16    | 24  |
| `query_exec.go`               | `scanByTimeRange`      | 15    | 22  |
| `seam_secondary.go`           | `AscendSeamsBySource`  | 15    | 22  |
| `mvcc.go`                     | `getCellVisibleRaw`    | 15    | 19  |
| `internal/views/budget.go`    | `LoadMultiContextPack` | 15    | 24  |
| `internal/views/budget.go`    | `resolveSupersession`  | 15    | 34  |

---

## Priority 4: Test/Example Functions (lower priority)

These are complex but don't affect production code quality:

| File                                               | Function | Cyclo | Cog   |
| -------------------------------------------------- | -------- | ----- | ----- |
| `examples/conversational_memory/main.go`           | `run`    | 114   | 167   |
| `internal/domain/storagecontract/contract_test.go` | `RunAll` | 96    | 194   |
| `examples/llm_context_engine/main.go`              | `run`    | 49    | 92    |
| `scale_integration_test.go`                        | various  | 28    | 49    |
| `stress_integration_test.go`                       | various  | 27    | 48    |
| Many `*_test.go` functions                         | various  | 11-24 | 15-46 |

**Strategy:** Table-driven tests, extract helpers, reduce during normal maintenance.

---

## Refactoring Strategies by File Type

### Core Engine Files (`internal/engine/`)

- Extract option builders (functional options pattern)
- Separate initialization steps
- Extract page management helpers
- Reduce nesting with early returns

### HNSW Graph (`internal/hnsw/graph.go`)

- Extract neighbor selection logic
- Separate graph maintenance operations
- Extract layer search helpers
- Use small, focused functions

### Primitives/Query (`primitives.go`, `query_exec.go`)

- Extract query builders
- Reduce nesting in predicate application
- Use early returns for error cases
- Extract scan helpers

### TUI (`cmd/tui/`)

- Extract view update logic
- Separate rendering from business logic
- Extract message handlers
- Use component-based architecture

### DB Configuration (`db_open.go`, `db.go`)

- Use functional options pattern
- Extract option builders
- Separate initialization steps
- Reduce configuration complexity

### Views/Budget (`internal/views/`)

- Extract budget calculation helpers
- Reduce nesting in candidate collection
- Extract resolution logic
- Use early returns

### B-Tree (`internal/engine/btree*.go`)

- Handle with care — algorithmic correctness critical
- Extract rebalancing case handlers
- Extract merge/redistribute into focused helpers
- Ensure comprehensive test coverage before changes

---

## Progress Tracking

- [x] P1.3: `health.go` `HealthCheck` — Cyclo 57→14 ✅
- [x] P1.3: `internal/engine/engine.go` `Open` — Cyclo 62→15 ✅
- [x] P1.4: `internal/hnsw/graph.go` `Insert` — Cyclo 44→15 ✅
- [x] P1.5: `db_open.go` `buildEngineOptions` — Cyclo 35→14 ✅
- [ ] P1.6: `cmd/tui/view_cells.go` `(*cellsView).Update` — CRAP 31
- [ ] P2 Tier A: `removeNode`, `findSeams`, `collectCandidates`, `LoadContextWithBudgeting`
- [ ] P2 Tier A: `readPagePooled`, `SearchByEmbedding`, `applyGroupBatch`
- [ ] P2 Tier B: Views, rotation, btree traversal, primitives
- [ ] P2 Tier C: B-Tree deletion internals
- [ ] P3: Remaining cyclomatic >15 (18 production functions)
- [ ] P4: Test/example functions (lower priority)
- [ ] P1.1: Examples (2 functions — deferred)
- [ ] P1.2: Tests (1 function — deferred)

---

## Notes

- **fail_on_violation** is currently `false` in `.complexity.yml` — violations will be reported but won't block CI
- Once all violations are addressed, set `fail_on_violation: true` to enforce thresholds going forward
- Focus on Tier A (cognitive >40, production code) for maximum impact
- B-Tree refactoring requires extra caution due to algorithmic correctness requirements
- Test and example files can be addressed with less rigor than core logic
- New code should adhere to thresholds from the start
- Extracted helpers may individually still violate thresholds but at much lower severity
