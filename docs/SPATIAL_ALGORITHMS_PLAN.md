# Spatial Algorithms Implementation Plan

Port selected algorithms from hexxgrid into hexxladb to improve query speed and context loading quality.

**Reference codebase:** `/home/anon/Documents/GitHub/hexxgrid/`

Key source directories:
- `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/spatial/` — rings, voronoi, watershed, LOD, FOV
- `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/pathfind/` — A*, Dijkstra, D* Lite, JPS
- `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/index/` — spatial indices, encoding
- `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/coord/` — coordinate types

## Phase 1: Concurrent Cell Fetch (Low effort, High speed)

**Goal:** Parallelize B+ tree lookups in `scanByRadius` and `LoadContext`.

**Current bottleneck:** Sequential `GetCell` loop over 3000+ coords at radius=32. Each lookup is ~10µs → 30ms total. Parallelism can reduce to ~5ms with 8 workers.

**Changes:**
- `internal/lattice/` — add `ParallelWalkRings` (generate coords concurrently for large radii)
- `primitives.go` — add `Tx.LoadContextParallel` using goroutine worker pool over coord batches
- `query_exec.go` — update `scanByRadius` to use parallel fetch when radius > threshold

**Tests:** Benchmark `LoadContext` vs `LoadContextParallel` at radius 8, 16, 32.

## Phase 2: Pathfinding Over Edges (Medium effort, High quality)

**Goal:** A* traversal of the `EdgeRecord` graph for semantic context loading.

**Current gap:** Context loading blindly expands concentric rings. With edge connectivity, we can follow semantic relationships (edges) to find related cells regardless of spatial distance.

**Changes:**
- `internal/pathfind/` — new package with A*, Dijkstra adapted for hexxladb's `Coord`/`PackedCoord`
  - `astar.go` — A* with hex distance heuristic, configurable cost via edge weights
  - `dijkstra.go` — uniform-cost graph walk (for cases without good heuristic)
  - `types.go` — `CostFunc`, `NeighborFunc`, `Path` types
- `primitives.go` — add `Tx.WalkEdgePath(ctx, from, to Coord) ([]Coord, error)`
- `views.go` — add `Tx.LoadContextByEdges(ctx, center, maxHops, maxTokens, budgeter, cfg)` — follows edges outward instead of ring expansion
- Wire into `QueryCells` planner: when edges exist from center, prefer edge-walk over ring-scan

**Reference:** `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/pathfind/astar.go`

**Tests:** Unit tests for A*/Dijkstra on small graphs. Integration test: put cells with edges, verify edge-walk finds connected cells that ring-walk misses.

## Phase 3: LOD Grid / Multi-Resolution Context (High effort, High value)

**Goal:** Coarsened summaries at outer rings for budget-aware context loading.

**Current gap:** `LoadContextWithBudgeting` evicts outer-ring cells entirely when over budget. With LOD, outer cells get summarized (not dropped), preserving spatial awareness.

**Changes:**
- `internal/lattice/lod.go` — LOD coordinate math: `CoarsenCoord(c Coord, factor int) Coord`, `RefineCoord`, `LODLevel`
- `internal/views/lod.go` — `LODContextLoader` that reads full cells at ring ≤ N, summarized cells at ring > N
- API: `Tx.LoadContextWithLOD(ctx, center, maxR, maxTokens, levels []LODLevel, budgeter, cfg)`
- Requires: coarsened cell storage (aggregate on write or compute on read)

**Reference:** `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/spatial/lod.go`

**Tests:** Unit tests for coordinate coarsening. Integration: verify LOD context returns summarized outer cells.

## Phase 4: Voronoi Partitioning for Multi-Seed (Medium effort, Medium quality)

**Goal:** Better multi-seed context loading via Voronoi assignment.

**Current gap:** `LoadMultiContextPack` unions overlapping ring scans and deduplicates. Voronoi assigns each cell to its nearest seed — no overlap, fairer distribution.

**Changes:**
- `internal/lattice/voronoi.go` — `HexVoronoi(seeds []Coord, maxR int) map[Coord]Coord` (BFS assignment)
- `internal/views/multi.go` — use Voronoi partitioning in `LoadMultiContextPack` to allocate token budget per region proportionally

**Reference:** `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/spatial/voronoi.go`

**Tests:** Unit tests for Voronoi partitioning. Integration: multi-seed context with overlapping radii uses Voronoi assignment.

## Phase 5: Field of View (Low effort, Nice-to-have)

**Goal:** Skip cells behind "walls" of empty space during context loading.

**Changes:**
- `internal/lattice/fov.go` — hex shadow-casting FOV
- Optional integration into `collectCandidates` when grid is sparse

**Reference:** `/home/anon/Documents/GitHub/hexxgrid/internal/core/domain/spatial/fov.go`

---

## Implementation Order

```
Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5
 (now)     (next)    (later)   (later)   (optional)
```

## Success Criteria

| Phase | Metric |
|-------|--------|
| 1 | `BenchmarkLoadContext/r32` ≥ 3× faster than sequential |
| 2 | Edge-connected cells found at hop distance > ring radius |
| 3 | Token budget utilization ≥ 90% (vs ~60% with eviction) |
| 4 | Multi-seed dedup eliminated; fair budget allocation |
| 5 | Sparse grid context load skips ≥ 30% of coords |
