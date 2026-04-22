# Hexxla

## A Hexagonal Spatial Memory Operating System for LLMs

**Version 1.9**
**Date:** April 2026
**Project Name:** Hexxla

## Vision

Hexxla is a deterministic hexagonal lattice that serves as a spatial operating layer for long-term LLM memory. Semantic or lexical methods select a seed cell; the lattice then governs local expansion, neighborhood-based context loading, explicit contradiction visibility, and structured multi-view memory.

This hybrid approach delivers token-efficient context packing, inspectable organization, and manageable knowledge conflicts.

### Why Hexagonal?

A hexagonal lattice provides 6-neighbor connectivity, natural ring enumeration, exact deterministic distance, and hierarchical clustering via super-hex regions. These properties make locality, neighborhood traversal, and summarization first-class and efficient.

## HexxlaDB Architecture Position (Locked)

Persistence for Hexxla is provided by **HexxlaDB**, a **custom, from-scratch, production-ready embedded database** (durable on-disk format, crash recovery), shipped as a standalone Go library. Canonical storage, keys, and primitives: **HEXXLA_DB.md**.

**Engine stance:** **Hex-native**, not a third-party ordered-KV or SQL wrapper (e.g. Pebble/RocksDB/SQLite as the storage core). The engine treats **Morton `PackedCoord`** keyspace, **ring walks as native prefix/range scans**, distinct **Edge** vs **Seam** storage families, and **MVCC** for `as_of` as first-class concerns. Illustrative layout: `internal/engine/` (pages, WAL, compaction), `internal/lattice/`, `internal/record/`, `internal/index/` — see **HEXXLA_DB.md**.

**SQLite** is not used.

**Module layout:** Import **`hexxladb`** at the root; stable API (`Open`, `DB`, `Batch`, options, query primitives) at the package root.

**Edge vs Seam:** Distinct logical concepts and **distinct storage families**. **`Cell.Edges`** and seam lists in the API are **read aggregates**, not denormalized primary storage.

**Seam primary key:** `seam/<ulid>` with **ULID** for `<ulid>`. Secondary: `seam-by-cells/<packed_a>/<packed_b>/<ulid>` (canonically ordered endpoint pair). See **HEXXLA_DB.md** for full keyspace detail.

**Concurrency and temporal support:** MVCC-style snapshot isolation for `as_of` and consistent lattice views.

**Relationship:** **HEXXLA.md** (this document) defines the **memory model**; **HEXXLA_DB.md** defines **keys, indexes, and query primitives** that implement it.

## Core Architecture

### Geometric Model

Memory cells are addressed by axial coordinates (q, r) with implicit cube coordinate s = -q - r.

**Hex distance formula** (axial / cube Manhattan):

```text
distance(a, b) = (abs(a.q - b.q) + abs(a.r - b.r) + abs((a.q + a.r) - (b.q + b.r))) / 2
```

This is the standard axial hex distance derived from cube Manhattan distance. It returns the exact number of steps between any two cells and defines ring boundaries precisely.

Each cell has exactly six symmetric neighbors. Ring walks and radius-bounded expansion are deterministic operations.

### Core Objects

**Coord**

```go
type Coord struct {
    Q int
    R int
}
```

Methods: `Cube()`, `Distance(other Coord) int`, `Neighbors() []Coord`, `Ring(radius int) []Coord`.

**Cell**

```go
type Cell struct {
    Coord        Coord
    RawContent   string
    Provenance   Provenance
    Validity     ValidityWindow
    Tags         []string
    ClusterHint  *Coord
    Facets       map[int]FacetView
    ActiveFacet  int
    Edges        []Edge
    Seams        []SeamRef
}
```

**FacetView**

```go
type FacetView struct {
    ID             int
    DerivedContent string
    LastRotated    time.Time
    DerivationHash string
}
```

**Seam**

```go
type Seam struct {
    ID               string // ULID string when persisted via HexxlaDB
    CellA            Coord
    CellB            Coord
    SeamType         string
    Reason           string
    ConfidenceDelta  float64
    DetectedAt       time.Time
    ResolutionStatus string
    ResolutionNote   string
}
```

**ValidityWindow**

```go
type ValidityWindow struct {
    ValidFrom *time.Time
    ValidTo   *time.Time
}
```

**Provenance**

```go
type Provenance struct {
    SourceID   string
    Confidence float64
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

## Facets

Facet IDs are fixed in v1.

| Facet ID | Purpose                                  | Lifecycle Notes                       |
| -------- | ---------------------------------------- | ------------------------------------- |
| 0        | Raw verbatim source (immutable anchor)   | Created on `put_cell`; never updated  |
| 1        | Semantic summary                         | Derived; updated only if hash matches |
| 2        | Conflict notes and seams                 | Auto-populated on seam creation       |
| 3        | Temporal validity window                 | Mirrors cell validity                 |
| 4        | Procedural or action-oriented derivative | LLM-derived                           |
| 5        | User or project-specific lens            | Custom                                |

### Facet Lifecycle

- Creation: Automatic on `put_cell`. Facet 0 holds `RawContent`; other facets are lazy or derived.
- Update: `update_facet` succeeds only if `DerivationHash` matches the current `RawContent` hash. Otherwise reject the update or create a seam and a new cell.
- Invalidation: `RawContent` is immutable, so any change creates a new cell linked by seam.
- Rotation: A simple pointer flip on `ActiveFacet`. `LastRotated` tracks freshness for cache management.

## Retrieval and Context Orchestration

Retrieval is hybrid.

1. Seed selection via embedding similarity, lexical search, or explicit coordinate.
2. Deterministic spatial expansion using `walk_ring` or bounded radius traversal.
3. Filtering and ranking by validity, confidence, tags, seams, and active facets.
4. Token-budget-aware packing into the prompt via `load_context`.

N-ring loading returns the minimal relevant local neighborhood instead of a generic summary. Hierarchical super-hex clustering provides natural levels: L0 cells, L1 working rings, and L2+ summaries.

### Example Primitives

- `walk_ring(center Coord, radius int, facetMask uint, filters Filter)` returns cell views.
- `load_context(center Coord, maxTokens int, filters Filter)` returns a token-capped `ContextPack`.
- `find_seams(center Coord, radius int, unresolvedOnly bool)` returns seams.

### `load_context` Output Shape

`load_context` returns a structure like this:

```go
type ContextPack struct {
    Cells       []CellView
    TotalTokens int
    Seams       []Seam
}
```

Ordering rule: concentric rings from center outward, then axial spiral order within each ring starting from the positive-q direction. If token budget is exceeded, the system drops the lowest-confidence items from outer rings first.

**HexxlaDB Go library:** The embedded module **`github.com/hexxla/hexxladb`** exposes **`record.CellRecord`** (and related wire types), not the nominal `Cell` / `CellView` structs above. Neighborhood loading is **[`Tx.LoadContext`](https://pkg.go.dev/github.com/hexxla/hexxladb#Tx.LoadContext)** with a **`maxCells`** cap (see **[HEXXLA_DB.md](HEXXLA_DB.md)** primitives)—integrators implement token budgeting and assemble a **`ContextPack`**-shaped value if needed. Optional higher-level view helpers are described in **[HEXXLA_READINESS_ROADMAP.md](HEXXLA_READINESS_ROADMAP.md)** (Optional API Surface Improvements).

## Contradiction Engine (Normative Source)

Conflicts are modeled as explicit seams: visible, queryable relations between cells.

Seam creation in v1 supports both automatic and manual modes.

- Automatic: a light synchronous check during `put_cell` or `link_cells` can create a seam when embedding delta plus provenance or confidence mismatch exceeds a threshold.
- Manual: an explicit `mark_conflict` call can create a seam.

Detection is hybrid: synchronous on write, plus optional background scanning. Heavy analysis remains post-v1.

Resolution is an explicit LLM-guided operation:

- `merge`
- `supersede`
- `archive`

All resolutions create a full audit trail. Superseded seams remain queryable for history.

Seams stay first-class and visible. They are never collapsed into ordinary edges.

(See **HEXXLA_DB.md** for storage layout, seam keys (`seam/<ulid>`, `seam-by-cells/...`), and indexing.)

## Time and Evolution

Core temporal support relies on explicit timestamps and validity intervals on every cell and seam. A rotating time-wheel is a visualization and navigation aid for temporal slicing.

Evolution is driven by local lattice operations:

- Facet view switching.
- Seam creation and resolution.
- Cluster promotion to tighter rings or summary layers.
- Optional local relevance propagation.

## Non-Goals

- Automatic semantic relevance judgment or truth adjudication.
- General-purpose graph database behavior.
- General-purpose vector database behavior.
- Automatic global re-indexing on every update.
- Experimental dynamics in v1, including pollen diffusion, crystallization, resonance retrieval, and lattice buckling.

## Baseline Comparison

| Aspect                      | Hexxla (v1)              | Pure Vector DB        | Pure Graph DB            |
| --------------------------- | ------------------------ | --------------------- | ------------------------ |
| Token efficiency            | High via N-ring locality | Medium via top-k      | Variable                 |
| Contradiction visibility    | Explicit queryable seams | Implicit              | Links only               |
| Locality and inspectability | Deterministic rings      | Approximate           | High traversal cost      |
| Update cost                 | Local with immutable raw | Requires re-embedding | Often global propagation |
| Temporal support            | Built-in bi-temporal     | Add-on                | Add-on                   |

## Experimental Extensions

These are post-v1 only.

- Pollen-style relevance diffusion.
- Crystallization for archival stabilization.
- Resonance-style fuzzy retrieval.
- Lattice buckling as a high-conflict heuristic.

These extensions will be evaluated only after v1 benchmarks confirm baseline value.

## Implementation Notes

- **Language:** Go
- **Persistence:** **HexxlaDB** (**HEXXLA_DB.md**) — custom embedded engine (pages, WAL, Morton-keyed storage); durable and crash-recoverable; **not** a third-party ordered-KV/SQL core or SQLite.
- **Stable import:** **`github.com/hexxla/hexxladb`** — root package holds **`Open`**, **`DB`**, **`Tx`**, and lattice-aware primitives; **`internal/record`** types are returned by queries. Product-level **`Cell`**, **`CellView`**, and token-based **`ContextPack`** in this document are **normative shapes** for integrators; map them from **`record.CellRecord`** and related APIs unless/until optional assembly helpers ship (see roadmap link above).
- **Process cache:** Optional in-memory structures (e.g. `map[Coord]*Cell`) for hot paths; durable state remains in HexxlaDB.
- **API:** HTTP/JSON tool interface for LLM clients.
- **Visualization:** Lightweight honeycomb dashboard with seam highlighting.

## v1 Scope

- Coordinates and ring walking.
- **HexxlaDB** persistence and core query primitives (aligned with **HEXXLA_DB.md**).
- Basic cells, facets, edges, and seams.

## Evaluation

Evaluation focuses on:

- Token efficiency.
- Contradiction handling quality.
- Neighborhood operation latency.
- Dashboard interpretability.

## Final Positioning

Hexxla is a hexagonal spatial memory operating system for LLMs. Embeddings or lexical search find the starting point; the lattice governs local structure, context packing, and conflict handling. This delivers a practical, original, and buildable improvement to long-term memory architectures.
