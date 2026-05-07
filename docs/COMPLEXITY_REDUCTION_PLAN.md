# Complexity Reduction Plan for hexxladb

**Branch:** `refactor/complexity-reduction`
**Date:** 2026-05-07
**Thresholds:** (from template, kept as-is for good code quality)
- Domain: cyclomatic 5, cognitive 10
- App: cyclomatic 10, cognitive 15
- Adapters In: cyclomatic 15, cognitive 20
- Adapters Out: cyclomatic 12, cognitive 18
- Pkg Root: cyclomatic 12, cognitive 18
- Cmd: cyclomatic 15, cognitive 20
- Default: cyclomatic 10, cognitive 15
- CRAP threshold: 30

**Current state:** 244 violations across 1096 functions (22% violation rate)
- 138 functions with violations
- 8 CRAP violations (>30)
- 99 cognitive complexity violations (>15)
- 137 cyclomatic complexity violations (>10)

---

## Priority 1: CRAP Score Violations (8 functions)

**CRAP = (cyclomatic² × (1 - coverage/100)³) + cyclomatic**
*These are complex AND poorly tested — dangerous to change*

### P1.1: Examples (Lower Priority - Demo Code)
1. `examples/conversational_memory/main.go` — `run` — CRAP 114, Cyclo 30, Coverage 114%
   - **Strategy:** Extract helper functions, reduce nesting, simplify demo logic
   - **Estimated effort:** 1-2 hours

2. `examples/llm_context_engine/main.go` — `run` — CRAP 49, Cyclo 30, Coverage 49%
   - **Strategy:** Extract helper functions, reduce nesting, simplify demo logic
   - **Estimated effort:** 1-2 hours

### P1.2: Tests (High Priority - Test Quality)
3. `internal/domain/storagecontract/contract_test.go` — `RunAll` — CRAP 96, Cyclo 30, Coverage 96%
   - **Strategy:** Use table-driven tests, extract setup/teardown helpers
   - **Estimated effort:** 2-3 hours

### P1.3: Core Engine/DB (Highest Priority - Core Logic)
4. `internal/engine/engine.go` — `Open` — CRAP 62, Cyclo 30, Coverage 62%
   - **Strategy:** Extract option builders, separate initialization steps
   - **Estimated effort:** 3-4 hours

5. `health.go` — `(*DB).HealthCheck` — CRAP 57, Cyclo 30, Coverage 57%
   - **Strategy:** Extract check functions, reduce nesting, early returns
   - **Estimated effort:** 2-3 hours

### P1.4: HNSW Graph (High Priority - Core Algorithm)
6. `internal/hnsw/graph.go` — `(*Graph).Insert` — CRAP 44, Cyclo 30, Coverage 44%
   - **Strategy:** Extract neighbor selection, graph maintenance helpers
   - **Estimated effort:** 4-6 hours

### P1.5: DB Configuration (Medium Priority)
7. `db_open.go` — `buildEngineOptions` — CRAP 35, Cyclo 30, Coverage 35%
   - **Strategy:** Extract option builders, use functional options pattern
   - **Estimated effort:** 2-3 hours

### P1.6: TUI (Medium Priority - UI Code)
8. `cmd/tui/view_cells.go` — `(*cellsView).Update` — CRAP 31, Cyclo 30, Coverage 31%
   - **Strategy:** Extract update logic, separate view rendering
   - **Estimated effort:** 2-3 hours

---

## Priority 2: Cognitive Complexity Violations >50

*These are the hardest to understand and maintain*

### P2.1: Tests (9 functions)
1. `internal/domain/storagecontract/contract_test.go` — `RunAll` — 194
2. `scale_integration_test.go` — `TestIntegration_putManyCells_survivesReopen` — 49
3. `stress_integration_test.go` — `TestStress_putManyCells_survivesReopen` — 48
4. `internal/engine/pagesize_test.go` — `TestParametricPageSize_putGetDelete` — 46
5. `mvcc_lattice_prune_integration_test.go` — `TestIntegration_MVCC_latticeAndHighChurnPrune` — 45
6. `internal/engine/btree_test.go` — `TestBTree_hexxladbLatticeChurnPruneShape` — 32
7. `embedding_test.go` — `TestQueryCells_EmbeddingWithTagFilter` — 24
8. `internal/mvcc/version_suffix_cell_key_test.go` — `TestCellPhysicalKeyWithVersionSuffix_twoCommitsVisibleByReadSeq` — 24
9. `internal/engine/pagesize_test.go` — `TestParametricPageSize_reopenPreservesPageSize` — 22

**Strategy for tests:** Use table-driven patterns, extract setup/teardown, reduce nested loops

### P2.2: Examples (2 functions)
10. `examples/conversational_memory/main.go` — `run` — 167
11. `examples/llm_context_engine/main.go` — `run` — 92

**Strategy for examples:** Extract helper functions, simplify demo logic

### P2.3: Core Engine/DB (2 functions)
12. `health.go` — `(*DB).HealthCheck` — 163
13. `internal/engine/engine.go` — `Open` — 81

**Strategy:** Extract check functions, separate initialization steps, early returns

### P2.4: HNSW Graph (3 functions)
14. `internal/hnsw/graph.go` — `(*Graph).Insert` — 93
15. `internal/hnsw/graph.go` — `(*Graph).removeNode` — 55
16. `internal/hnsw/graph.go` — `(*Graph).searchLayer` — 28

**Strategy:** Extract neighbor selection, graph maintenance, layer search helpers

### P2.5: Primitives/Query (2 functions)
17. `primitives.go` — `(*Tx).findSeams` — 55
18. `embedding_search.go` — `(*Tx).SearchByEmbedding` — 38

**Strategy:** Extract query builders, reduce nesting, early returns

### P2.6: B-Tree (2 functions)
19. `internal/engine/btree_delete.go` — `(*BTree).rebalanceLeaf` — 49
20. `internal/engine/btree_delete.go` — `(*BTree).Delete` — 26

**Strategy:** Extract rebalancing logic, separate case handlers

### P2.7: DB Configuration (1 function)
21. `db_open.go` — `buildEngineOptions` — 47

**Strategy:** Extract option builders, use functional options pattern

### P2.8: Views/Budget (4 functions)
22. `internal/views/budget.go` — `collectCandidates` — 45
23. `internal/views/budget.go` — `LoadContextWithBudgeting` — 44
24. `internal/views/budget.go` — `resolveSupersession` — 34
25. `internal/views/budget.go` — `LoadMultiContextPack` — 24

**Strategy:** Extract budget calculation helpers, reduce nesting

### P2.9: TUI (2 functions)
26. `cmd/tui/view_cells.go` — `(*cellsView).Update` — 43
27. `cmd/tui/view_cells.go` — `(*cellsView).View` — 36

**Strategy:** Extract update logic, separate view rendering

### P2.10: Other Core Functions (13 functions)
28. `internal/engine/engine.go` — `(*Engine).readPagePooled` — 39
29. `internal/engine/engine.go` — `(*Engine).applyGroupBatch` — 37
30. `internal/views/views.go` — `AssembleCellView` — 36
31. `rotation.go` — `RotateEncryptionWithOptions` — 36
32. `hex_render.go` — `RenderHexGrid` — 34
33. `internal/engine/btree.go` — `(*BTree).AscendRange` — 34
34. `internal/engine/btree.go` — `(*BTree).GetUsingRoot` — 34
35. `primitives.go` — `(*Tx).WalkRingFacets` — 31
36. `primitives.go` — `(*Tx).putSeamWithOp` — 31
37. `api_bench_test.go` — `BenchmarkAPI_QueryCells` — 29
38. `embedding_reindex.go` — `(*Tx).ReindexEmbeddings` — 29
39. `mvcc_lifecycle.go` — `(*DB).PruneCellVersions` — 29
40. `primitives.go` — `(*Tx).PutCell` — 28

**Strategy:** Extract helper functions, reduce nesting, early returns

---

## Priority 3: Cyclomatic Complexity Violations >20

*Functions with too many branching paths*

### P3.1: Highest Cyclomatic (>50)
1. `examples/conversational_memory/main.go` — `run` — 114
2. `internal/domain/storagecontract/contract_test.go` — `RunAll` — 96
3. `internal/engine/engine.go` — `Open` — 62
4. `health.go` — `(*DB).HealthCheck` — 57
5. `examples/llm_context_engine/main.go` — `run` — 49
6. `internal/hnsw/graph.go` — `(*Graph).Insert` — 44
7. `db_open.go` — `buildEngineOptions` — 35
8. `cmd/tui/view_cells.go` — `(*cellsView).Update` — 31
9. `internal/views/budget.go` — `LoadContextWithBudgeting` — 30
10. `scale_integration_test.go` — `TestIntegration_putManyCells_survivesReopen` — 28
11. `mvcc_lattice_prune_integration_test.go` — `TestIntegration_MVCC_latticeAndHighChurnPrune` — 27
12. `stress_integration_test.go` — `TestStress_putManyCells_survivesReopen` — 27
13. `embedding_search.go` — `(*Tx).SearchByEmbedding` — 25
14. `cmd/tui/view_cells.go` — `(*cellsView).View` — 25
15. `primitives.go` — `(*Tx).putSeamWithOp` — 24
16. `internal/hnsw/graph.go` — `(*Graph).removeNode` — 24
17. `internal/engine/btree_delete.go` — `(*BTree).rebalanceLeaf` — 24
18. `internal/engine/group_wal.go` — `(*Engine).applyGroupBatch` — 24
19. `internal/engine/btree_test.go` — `TestBTree_hexxladbLatticeChurnPruneShape` — 24
20. `query_exec.go` — `applyPredicates` — 23
21. `query_exec.go` — `(*Tx).QueryCells` — 23
22. `primitives.go` — `(*Tx).findSeams` — 23
23. `internal/engine/btree_delete.go` — `(*BTree).Delete` — 23
24. `rotation.go` — `RotateEncryptionWithOptions` — 22
25. `mvcc_churn_integration_test.go` — `TestIntegration_MVCC_sustainedPutCellSameKey` — 22
26. `internal/hnsw/graph.go` — `(*Graph).Search` — 22
27. `internal/views/views.go` — `AssembleCellView` — 22
28. `internal/engine/engine.go` — `(*Engine).readPagePooled` — 22
29. `db.go` — `Open` — 21
30. `mvcc_lifecycle.go` — `(*DB).PruneCellVersions` — 21
31. `primitives.go` — `(*Tx).PutCell` — 20
32. `secondary_indexes_test.go` — `TestPutCell_secondaryIndexes` — 20
33. `primitives.go` — `(*Tx).WalkRingFacets` — 19
34. `hex_render.go` — `RenderHexGrid` — 19
35. `internal/record/record_test.go` — `cellEqual` — 19
36. `tx.go` — `(*DB).Update` — 18
37. `internal/engine/writetxn.go` — `(*Engine).CommitWriteTxn` — 18
38. `internal/engine/pagesize_test.go` — `TestParametricPageSize_putGetDelete` — 18
39. `embedding_reindex.go` — `(*Tx).ReindexEmbeddings` — 17
40. `cmd/tui/view_seams.go` — `(*seamsView).View` — 17
41. `internal/engine/btree.go` — `(*BTree).AscendRange` — 17
42. `mvcc_test.go` — `TestMVCC_StatsAndPruneCellVersions` — 16
43. `seam_secondary_test.go` — `TestTx_PutSeam_secondaryIndexes` — 16
44. `internal/hnsw/graph.go` — `(*Graph).searchLayer` — 16
45. `internal/mvcc/version_suffix_cell_key_test.go` — `TestCellPhysicalKeyWithVersionSuffix_twoCommitsVisibleByReadSeq` — 16
46. `internal/record/seam.go` — `decodeSeamPayloadV1` — 16
47. `internal/engine/btree.go` — `(*BTree).insertIntoLeaf` — 16
48. `internal/engine/btree_delete.go` — `(*BTree).removeInternalChild` — 16

**Strategy:** Extract helper functions, use early returns, reduce nested conditionals

---

## Priority 4: Remaining Cyclomatic Complexity (11-15)

*Lower priority but should be addressed for consistency*

- 89 functions with cyclomatic 11-15
- Many are test functions, TUI views, and internal engine methods
- **Strategy:** Incremental cleanup during normal development

---

## Refactoring Strategies by File Type

### Test Files (`*_test.go`)
- Use table-driven test patterns
- Extract setup/teardown into helper functions
- Reduce nested loops and conditionals
- Use `t.Helper()` for test helpers

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

---

## Progress Tracking

- [ ] P1.1: Examples (2 functions)
- [ ] P1.2: Tests (1 function)
- [ ] P1.3: Core Engine/DB (2 functions)
- [ ] P1.4: HNSW Graph (1 function)
- [ ] P1.5: DB Configuration (1 function)
- [ ] P1.6: TUI (1 function)
- [ ] P2.1: Tests (9 functions)
- [ ] P2.2: Examples (2 functions)
- [ ] P2.3: Core Engine/DB (2 functions)
- [ ] P2.4: HNSW Graph (3 functions)
- [ ] P2.5: Primitives/Query (2 functions)
- [ ] P2.6: B-Tree (2 functions)
- [ ] P2.7: DB Configuration (1 function)
- [ ] P2.8: Views/Budget (4 functions)
- [ ] P2.9: TUI (2 functions)
- [ ] P2.10: Other Core Functions (13 functions)
- [ ] P3: Cyclomatic >20 (48 functions)
- [ ] P4: Cyclomatic 11-15 (89 functions)

---

## Notes

- **fail_on_violation** is currently `false` in `.complexity.yml` — violations will be reported but won't block CI
- Once all violations are addressed, set `fail_on_violation: true` to enforce thresholds going forward
- Focus on Priority 1 (CRAP) and Priority 2 (cognitive >50) first for maximum impact
- Test and example files can be addressed with less rigor than core logic
- New code should adhere to thresholds from the start
