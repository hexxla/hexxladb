# Spatial Algorithm Improvements — Implementation Plan

**Scope:** Internal-only changes. No public API surface modifications.
**Branch target:** `feat/spatial-improvements`
**Priority:** Non-API improvements first; API-surface improvements deferred to a separate plan.

---

## Background

After cross-referencing hexxgrid's spatial primitives against hexxladb's `internal/lattice` and
`internal/views` layers, five improvements were identified that require zero public API changes.
Each improves correctness or performance internally; callers get better behaviour automatically.

---

## Phase 1 — `SpiralRange` annular walk (`internal/lattice/ring.go`)

### Problem

`WalkRings(dst, center, maxR)` always starts from ring 0. There is no way to walk rings
`[minR, maxR]` without walking rings `[0, minR-1]` first and discarding them. This causes:

- Wasted allocations in LOD's `collectLODCoords` (walks fine rings then outer rings separately
  with manual deduplication via a `seen` map).
- Any caller needing an annular slice (e.g. "only cells between ring 3 and ring 6") must
  over-fetch and filter.

### Implementation

Add to `internal/lattice/ring.go`:

```go
// SpiralRange appends all coordinates in rings [minR, maxR] from center into dst.
// If minR <= 0 the center cell is included. Rings are emitted in ascending order.
// Returns dst unchanged when maxR < 0 or minR > maxR.
func SpiralRange(dst []Coord, center Coord, minR, maxR int) []Coord
```

Implementation is a simple loop from `max(0, minR)` to `maxR` calling `RingInto`. When
`minR <= 0` prepend the center. No new data structures.

### Test plan

- `SpiralRange(nil, c, 0, 3)` must equal `WalkRings(nil, c, 3)`.
- `SpiralRange(nil, c, 2, 4)` must contain no coords with distance < 2 or > 4.
- Empty cases: `minR > maxR`, `maxR < 0`.
- Count: rings [minR, maxR] contain `sum(6*k for k in [minR..maxR])` cells
  (plus 1 for center if minR == 0).

### Impact

Zero API surface change. Pure addition to `internal/lattice`.

---

## Phase 2 — Simplify `collectLODCoords` using `SpiralRange` (`internal/views/load_context.go`)

### Problem

`collectLODCoords` (lines ~165–229) has two distinct loops:

1. Walk rings 0..fineR with `WalkRingsPacked`, collect fine coords.
2. Walk rings fineR+1..maxR, coarsen each coord, deduplicate into a `seen` map.

The `seen` map allocation and the two-phase walk are direct consequences of the missing
`SpiralRange`. The code is correct but more complex than needed.

### Implementation

After Phase 1, replace the inner loop body to use `SpiralRange(nil, center, fineR+1, maxR)`
for the coarse phase instead of the manual `ringBuf` loop. The `seen` map is still needed for
coarsened-coord deduplication (multiple fine coords map to the same coarse coord), but the ring
iteration becomes one call. No behaviour change — existing LOD tests must pass unchanged.

### Test plan

Run existing `lod_test.go` and `lod_context_test.go` without modification. All must pass.
Add one test confirming the coarse-phase set size matches the old implementation on a known grid.

### Impact

Internal refactor only. No observable behaviour change.

---

## Phase 3 — `FieldOfViewShadowcast` symmetric shadowcasting (`internal/lattice/fov.go`)

### Problem

The current `FieldOfView` uses **ray-casting**: for every candidate hex in the radius a
`HexLine` is traced from origin. This has two issues:

1. **Correctness:** Ray-casting with cube-coordinate `cubeLerp` + `cubeRound` can produce
   asymmetric results — cell A can see cell B but B cannot see A, due to floating-point rounding
   placing the ray on different intermediate hexes depending on direction. This is a known flaw
   of the Red Blob Games raycasting approach on hex grids.

2. **Performance:** O(r²) candidates × O(r) ray length = **O(r³)** total checks.
   At r=5: ~91 rays × ~5 hops = ~455 checks. At r=10: ~331 rays × ~10 hops = ~3310 checks.

### Algorithm: Symmetric Shadowcasting (Albert Ford, 2021)

Source: https://www.albertford.com/shadowcasting/

The algorithm divides the space around the origin into **6 sextants** (matching the 6
directions of a hex grid). Each sextant is processed independently. Within a sextant, rows
are scanned outward from the origin. Shadow slopes track which angular ranges are blocked.

Key properties (all 6 of Adam Milazzo's desirable FOV properties):

- **Symmetry** — if A sees B then B sees A. Enforced by `isSymmetric` slope check.
- **Expansive walls** — all walls of a convex room are visible from inside it.
- **Expanding pillar shadows** — thin obstacles cast widening shadows at range.
- **No blind corners** — corners adjacent to walls are correctly revealed.
- **No artifacts** — no stray visible cells behind solid walls.
- **Efficiency** — O(visible cells) rather than O(r³). Each cell visited at most once.

### Hex adaptation

The standard algorithm is designed for square grids with 4 cardinal quadrants. For a hex grid
we use **6 sextants** corresponding to the 6 cube directions. Each sextant maps the
`(depth, col)` row/column abstraction onto axial coordinates via a per-sextant transform.

The 6 sextant transforms use the 6 cube direction vectors already defined in `ring.go`:

```
cubeDirections = [6]Cube{
    {1, 0, -1},  // sextant 0: +Q face
    {1, -1, 0},  // sextant 1
    {0, -1, 1},  // sextant 2
    {-1, 0, 1},  // sextant 3: -Q face
    {-1, 1, 0},  // sextant 4
    {0, 1, -1},  // sextant 5
}
```

For sextant `i`, at `depth d` and column offset `col`, the target coord is:

```
target = origin + d * primaryDir[i] + col * secondaryDir[i]
```

where `primaryDir` steps outward and `secondaryDir` steps laterally within the sextant row.

Slopes are rational numbers (numerator/denominator as integers) to avoid floating-point
drift. Use integer fractions: `slope = (2*col - 1) / (2*depth)` as per Ford's formulation,
stored as `[2]int{num, den}` and compared with cross-multiplication.

### Signature

New unexported function added alongside existing `FieldOfView`:

```go
// FieldOfViewShadowcast computes visible coordinates using symmetric shadowcasting.
// Identical contract to FieldOfView; prefer this for accuracy and performance at r > 3.
func FieldOfViewShadowcast(origin Coord, maxRadius int, opaque func(Coord) bool) []Coord
```

`FieldOfView` is then updated to **delegate to `FieldOfViewShadowcast`** — same public
function, better internals. The old raycasting body moves to `fieldOfViewRaycast` (unexported)
and is kept for the regression test comparison.

### Test plan

- **Symmetry test:** for every pair (A, B) in result, B must also see A. Run against multiple
  obstacle configurations.
- **Regression comparison:** for r ≤ 5 on a fully-open grid, `FieldOfViewShadowcast` and
  `fieldOfViewRaycast` must return identical coordinate sets.
- **Occlusion test:** single opaque cell at (1,0) from origin must block (2,0), (3,0), etc.
- **Expansive walls:** in a ring-1 fully-opaque border, all 6 wall cells must be visible.
- **Pillar shadow:** a single opaque cell at mid-range must shadow cells directly behind it.
- **Performance benchmark:** compare r=10, r=20, r=50 against raycasting baseline.

### Impact

`LoadContextFOV` calls `FieldOfView` — it gets the fix automatically. No API change anywhere.

---

## Phase 4 — Weighted Voronoi region growing (`internal/lattice/voronoi.go`)

### Problem

`Voronoi(seeds, maxRadius)` uses uniform-cost multi-source BFS: every hex expansion step
costs 1.0 regardless of what is at that coord. This means region boundaries are purely
geometric (Voronoi diagram by hex distance).

For LLM memory context assembly, this is suboptimal: a high-confidence cell 4 hops from
seed A but 5 hops from seed B gets assigned to A even if B is semantically closer. The DB
has confidence and recency data that the geometry layer ignores.

### Algorithm: Dijkstra multi-source (weighted BFS)

Replace the FIFO queue in `processBFS` with a **min-heap priority queue** — the same `pq`
type already implemented in `internal/pathfind/pq.go`. Each expansion step costs
`1.0 + weightPenalty(coord)` where `weightPenalty` comes from an optional caller-supplied
function. When `nil`, penalty = 0 and behaviour is identical to current uniform BFS.

The priority is `dist + penalty` accumulated along the path from the seed. Lower priority
= claimed first. This is exactly Dijkstra's multi-source shortest path.

### Signature change (internal only)

```go
// WeightFunc returns an additional traversal cost for coord (must be >= 0).
// Return 0 for uniform cost (identical to current BFS behaviour).
// Nil is equivalent to returning 0 for all coords.
type WeightFunc func(coord Coord) float64

// Voronoi computes a weighted hex-grid Voronoi diagram via multi-source Dijkstra.
// When weightFn is nil, behaviour is identical to the previous uniform-BFS implementation.
func Voronoi(seeds []Coord, maxRadius int, weightFn WeightFunc) (cells []VoronoiCell, owner map[Coord]int)
```

All callers of `lattice.Voronoi` are internal. Update the one call site in
`internal/views/fov_voronoi.go` (or wherever `LoadContextVoronoi` delegates) to pass `nil`.

### Correctness note

Dijkstra multi-source is correct when all weights are non-negative. `WeightFunc` must be
documented as requiring non-negative return values. Negative weights would require
Bellman-Ford and are not a use case here.

### Test plan

- `Voronoi(seeds, r, nil)` must produce identical output to the old uniform BFS (regression).
- Weighted test: seed A at (0,0), seed B at (3,0). Cell at (1,0) has `weightFn` returning
  10.0. With uniform BFS, (1,0) goes to A (distance 1 < 2). With weight=10, effective cost
  from A = 1 + 10 = 11 > cost from B = 2. So (1,0) should go to B.
- Tie-breaking: equal-cost cells go to lower seed index (same as current behaviour).

### Impact

All call sites pass `nil` — zero behaviour change at call sites. Internal callers (service
layer, Mosaic) can later inject confidence-based weights without any further API change.

---

## Phase 5 — `EuclideanHeuristic` + heuristic selection (`internal/pathfind/`) ✅

### Problem

`FindEdgePath` hardcoded `pathfind.HexDistanceHeuristic` in `pathfind_api.go`. For
hex grids this is the optimal heuristic for unit-cost edges, but on diagonal paths the
Euclidean distance provides a tighter lower bound and can reduce node expansions.

### Implementation (as built)

Added `EuclideanHeuristic` to `internal/pathfind/types.go`:

```go
// EuclideanHeuristic returns the Euclidean distance in the flat axial 2D embedding.
// For pointy-top hex grids the metric is sqrt(dq² + dr² + dq·dr).
// Admissible for edge weights >= 1; provides a tighter lower bound than
// HexDistanceHeuristic on diagonal paths, reducing A* node expansions.
func EuclideanHeuristic(current, goal lattice.Coord) float64 {
    dq := float64(goal.Q - current.Q)
    dr := float64(goal.R - current.R)
    return math.Sqrt(dq*dq + dr*dr + dq*dr)
}
```

`FindEdgePath` in `pathfind_api.go` now uses `EuclideanHeuristic` by default.
`HexDistanceHeuristic` is retained as an available alternative.

### Admissibility

`EuclideanHeuristic ≤ HexDistanceHeuristic` for all axial coords (verified by test
`TestEuclideanHeuristic_admissible` over a radius-10 ball). Admissible for any edge
weight ≥ 1. For graphs with weights < 1 (sub-unit costs), callers should use
`HexDistanceHeuristic` or a zero heuristic (pure Dijkstra).

### Test plan (as built)

- `TestEuclideanHeuristic_admissible`: verified ≤ hex distance for all ring-10 coords.
- `TestEuclideanHeuristic_samePathAsHex`: same path length as `HexDistanceHeuristic`.
- `BenchmarkAStar_EuclideanHeuristic` vs `BenchmarkAStar_HexDistanceHeuristic`.

### Impact

Public `FindEdgePath` signature unchanged. Better node-expansion efficiency on diagonal
paths.

---

## Summary table

| Phase                  | File(s)                                         | Status  | API change    |
| ---------------------- | ----------------------------------------------- | ------- | ------------- |
| 1 — SpiralRange        | `internal/lattice/ring.go`                      | ✅ Done | None          |
| 2 — LOD refactor       | `internal/views/load_context.go`                | ✅ Done | None          |
| 3 — Shadowcasting FOV  | `internal/lattice/fov.go`                       | ✅ Done | None          |
| 4 — Weighted Voronoi   | `internal/lattice/voronoi.go`                   | ✅ Done | Internal only |
| 5 — EuclideanHeuristic | `internal/pathfind/types.go`, `pathfind_api.go` | ✅ Done | None          |

All phases: `make ci` must pass after each phase before proceeding to the next.

---

## Deferred (require API surface change — separate plan)

- **~~`FindEdgePath` caller-supplied `CostFunc`~~** ✅ Done — `FindEdgePathConfig.CostFunc` added; signature changed to config struct.
- **~~`VoronoiContextConfig.WeightFunc`~~** ✅ Done — `VoronoiWeightFunc` type alias exposed; wired through to `lattice.Voronoi`.
- **`IntersectRanges`** — new exported function on public lattice API.
- **`PartitionKMeans`** for unknown seed discovery — new public method or query option.

---

## References

- Albert Ford, _Symmetric Shadowcasting_ (2021): https://www.albertford.com/shadowcasting/
- Adam Milazzo, _Roguelike Vision Algorithms_: http://www.adammil.net/blog/v125_Roguelike_Vision_Algorithms.html
- Red Blob Games, _Hexagonal Grids_: https://www.redblobgames.com/grids/hexagons/
- Red Blob Games, _2D Visibility_: https://www.redblobgames.com/articles/visibility/
- hexxgrid spatial primitives: `spatial.go`, `internal/core/domain/spatial/`
