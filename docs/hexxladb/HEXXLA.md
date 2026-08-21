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

#### Coord

```go
type Coord struct {
    Q int
    R int
}
```

Methods: `Cube()`, `Distance(other Coord) int`, `Neighbors() []Coord`, `Ring(radius int) []Coord`.

#### Cell

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

#### FacetView

```go
type FacetView struct {
    ID             int
    DerivedContent string
    LastRotated    time.Time
    DerivationHash string
}
```

#### Seam

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

#### ValidityWindow

```go
type ValidityWindow struct {
    ValidFrom *time.Time
    ValidTo   *time.Time
}
```

#### Provenance

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
| 0        | Raw verbatim source (conceptual anchor)  | Stored as `CellRecord.RawContent`     |
| 1        | Semantic summary                         | Derived; updated only if hash matches |
| 2        | Conflict notes and seams                 | Application-derived from seam records |
| 3        | Temporal validity window                 | Mirrors cell validity                 |
| 4        | Procedural or action-oriented derivative | LLM-derived                           |
| 5        | User or project-specific lens            | Custom                                |

### Facet Lifecycle

- Creation: `put_cell` stores `RawContent` on the cell. Stored facet rows are explicit `PutFacet` writes and represent derived views.
- Update: `update_facet` succeeds only if `DerivationHash` matches the current `RawContent` hash. Otherwise reject the update or create a seam and a new cell.
- Invalidation: applications that treat raw content as immutable create a successor cell and link it with `MarkSupersedes`; HexxlaDB also permits replacing a cell at the same coordinate.
- Rotation: `FacetRecord.LastRotated` tracks freshness. `CellView.ActiveFacet` is an assembled-view field, not a dedicated on-disk pointer.

## Retrieval and Context Orchestration

Retrieval is hybrid.

1. Seed selection via embedding similarity, lexical search, or explicit coordinate.
2. Deterministic spatial expansion using `walk_ring` or bounded radius traversal.
3. Filtering and ranking by validity, confidence, tags, seams, and active facets.
4. Token-budget-aware packing into the prompt via `load_context`.

N-ring loading returns the minimal relevant local neighborhood instead of a generic summary. Hierarchical super-hex clustering provides natural levels: L0 cells, L1 working rings, and L2+ summaries.

The current library includes `SuperHexSummaryIndex`, a rebuildable aperture-7 occupancy prototype. It takes one consistent cell snapshot and then follows the logical changelog, providing constant-time occupancy reads without changing the stable coordinate encoding. Content-bearing or persistent cluster summaries remain product-tier work.

### Example Primitives

- `walk_ring(center Coord, radius int, facetMask uint, filters Filter)` returns cell views.
- `load_context(center Coord, maxTokens int, filters Filter)` returns a token-capped `ContextPack`.
- `find_seams(center Coord, radius int, unresolvedOnly bool)` returns seams.

### `load_context` Output Shape

`load_context` returns a structure like this:

```go
type ContextPack struct {
    Cells        []CellView
    TotalTokens  int
    Seams        []SeamRecord
    Stats        ContextPackStats
    Explanations []CellExplanation
}
```

Candidate order and selection depend on `LoadContextConfig`: standard loading starts with concentric rings, while LOD, multi-seed, and edge traversal use their corresponding expansion strategies. Token-budget eviction prefers high-confidence, nearer cells; explanation records are populated when `Assembly.Explain` is enabled.

**HexxlaDB Go library:** The embedded module **`github.com/hexxla/hexxladb`** re-exports **`CellRecord`** and related wire types at the root package. **[`Tx.LoadContext`](https://pkg.go.dev/github.com/hexxla/hexxladb#Tx.LoadContext)** is the unified token-budgeted **`ContextPack`** entry point: configure seeds, radius, validity time, graph traversal, budgeter, and assembly through **`LoadContextConfig`**. Filtering helpers include **`FilterCellViews`** and **`TruncateCellViewsToTokenBudget`** ([**`views.go`**](../../views.go)). See **[API_REFERENCE.md](./API_REFERENCE.md)** for the full exported surface.

## Contradiction Engine (Normative Source)

Conflicts are modeled as explicit seams: visible, queryable relations between cells.

The current library exposes explicit seam creation and resolution primitives.

- Manual: `MarkConflict`, `MarkSupersedes`, or `PutSeam` writes the seam.
- Automatic detection: a consumer may implement embedding/provenance policy in its domain or application layer and call the same explicit APIs.

HexxlaDB does not perform truth assessment or background contradiction detection itself.

Resolution is an explicit LLM-guided operation:

- `merge`
- `supersede`
- `archive`

`ResolveSeam` records status and notes. MVCC preserves older seam versions when enabled; changelog consumers can maintain an external audit trail when the changefeed is enabled.

Seams stay first-class and visible. They are never collapsed into ordinary edges.

(See **HEXXLA_DB.md** for storage layout, seam keys (`seam/<ulid>`, `seam-by-cells/...`), cell secondaries (`source/`, `time/`, `tag/`), and indexing.)

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
- **Stable import:** **`github.com/hexxla/hexxladb`** — root package holds **`Open`**, **`DB`**, **`Tx`**, lattice-aware primitives, re-exported wire records, assembled **`CellView`**, and token-budgeted **`ContextPack`** APIs.
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

## HexxlaDB and HEXXLA (how to use the library today)

**Scope:** This repository ships **HexxlaDB** (`package hexxladb`) — the embedded engine — not the full HEXXLA product (HTTP/JSON gateway, embedding/index services, dashboards). Those live in a **consumer service** that imports `hexxladb` and implements orchestration in **domain** / **app** layers (see **[HEXAGONAL_ARCHITECTURE.md](../context/HEXAGONAL_ARCHITECTURE.md)**). The sections below describe how **today’s** HexxlaDB API fulfills the **normative requirements** in this document until HEXXLA is built as a separate composition root.

### Mapping: HEXXLA intent → HexxlaDB primitives

| HEXXLA concept (this doc)                  | HexxlaDB surface (stable, root package)                                                                                                             | Notes                                                                                                                           |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Spatial addressing `(q,r)` / packed keys   | **`Coord`**, **`Pack`**, **`Unpack`**, **`Ring`**, **`WalkRings`**                                                                                  | Stable root-package geometry and Morton-key helpers.                                                                            |
| Persist a memory cell                      | **`Tx.PutCell`** → stores **`record.CellRecord`** (wire types in **`internal/record`**)                                                             | Immutable raw + provenance + validity + tags; secondaries **`source/`**, **`time/`**, **`tag/`** maintained automatically.      |
| Read one cell                              | **`Tx.GetCell`**                                                                                                                                    | Snapshot-visible version when MVCC enabled.                                                                                     |
| Neighborhood + token-budget `ContextPack`  | **`Tx.LoadContext`**, **`LoadContextConfig`**, **`TokenBudgeter`** (for example **`ByteLenBudgeter`**)                                              | `Seeds`, `MaxRing`, `MaxTokens`, optional validity `AsOf`, edge BFS, and assembly controls; distinct from MVCC **`DB.ViewAt`**. |
| Facets (derived views, hash discipline)    | **`Tx.PutFacet`**, **`Tx.UpdateFacet`**, **`Tx.GetFacet`**, **`Tx.AscendFacetsForCell`**, **`Tx.WalkRingFacets`**                                   | Product maps **FacetView** from assembled **`CellView`**.                                                                       |
| Edges (adjacency / conversation graph)     | **`Tx.LinkCells`** (sugar) or **`Tx.PutEdge`**, **`Tx.GetEdge`**, **`Tx.AscendEdgesFrom`**                                                          | Distinct from seams.                                                                                                            |
| Weighted edge routing                      | **`Tx.FindEdgePath`**, **`FindEdgePathConfig`**, **`Tx.WalkEdges`**                                                                                 | Dijkstra shortest path and BFS reachability over directed stored edges.                                                         |
| Visibility / region context                | **`Tx.LoadContextFOV`**, **`Tx.LoadContextVoronoi`**                                                                                                | Deterministic shadowcasting FOV and weighted multi-source Voronoi.                                                              |
| Hierarchical occupancy                     | **`NewSuperHexSummaryIndex`**, **`SuperHexSummaryIndex`**                                                                                           | Rebuildable in-memory aperture-7 summaries maintained from the changelog.                                                       |
| Contradictions / seams                     | **`Tx.PutSeam`**, **`Tx.FindSeams`**, **`Tx.FindSeamsAt`**, **`Tx.ResolveSeam`**, **`Tx.MarkConflict`**                                             | Storage: **`seam/<ulid>`** + **`seam-by-cells/…`**; seam secondaries **`AscendSeamsBySource`**, **`AscendSeamsInTimeBucket`**.  |
| Secondary discovery (no full lattice walk) | **`Tx.AscendCellsBySource`**, **`Tx.AscendCellsInTimeBucket`**, **`Tx.AscendCellsByTag`**, **`Tx.AscendDistinctTags`**, **`Tx.ListExistingTopics`** | Same for seams where indexed.                                                                                                   |
| MVCC “as of” snapshot (engine time)        | **`DB.ViewAt`**, **`DB.ViewAtTime`**, **`DB.Update`**                                                                                               | Orthogonal to **validity** windows on records; see **MVCC temporal semantics** in [`TX.md`](./TX.md).                           |
| Retention / observability (optional)       | **`DB.StatsMVCC`**, **`DB.PruneCellVersions`**, **`DB.SuggestedPruneBeforeSeq`**, **`DB.ReadChangelogSince`** (if enabled)                          | Operational; not required for core memory semantics.                                                                            |
| At-rest encryption (optional)              | **`Options`** encryption fields, **`DeriveKeyFromPassphrase`**, **`RotateEncryption`**                                                              | See **ENCRYPTION.md**.                                                                                                          |
| Raw btree escape hatch                     | **`Tx.Get`**, **`Tx.Put`**, **`Tx.AscendRange`**                                                                                                    | Rare; prefer lattice primitives for HEXXLA-shaped code.                                                                         |

Canonical key layout and primitive behavior: **[HEXXLA_DB.md](./HEXXLA_DB.md)**. Every exported **`hexxladb`** symbol: **[API_REFERENCE.md](./API_REFERENCE.md)**.

### Reference examples

- **`go run ./examples/conversational_memory`** — self-contained 14-phase memory lifecycle, query, MVCC, FOV, and graph walkthrough.
- **`go run ./examples/spatial_algorithms`** — self-contained shadowcasting, LOD, Voronoi, Dijkstra, BFS, and edge-context walkthrough.
- **`go run ./examples/llm_context_engine`** — embedding-backed retrieval and prompt assembly; requires Ollama with `all-minilm`.

The examples are scenario-oriented rather than an exhaustive symbol test. Use **[API_REFERENCE.md](./API_REFERENCE.md)** as the complete root-package inventory.

### Building HEXXLA on top

1. **Choose seed coordinates** outside the engine (embeddings, lexical hit, or explicit user anchor) — HexxlaDB does not pick seeds for you.
2. **Write path:** **`Update`** → **`PutCell`** / **`PutFacet`** / **`LinkCells`** / **`PutSeam`** (or **`MarkConflict`**) with policies from **domain** rules.
3. **Read path:** **`View`** → **`LoadContext(LoadContextConfig{...})`** for prompt assembly, plus **`AscendCellsByTag`** / **`AscendCellsBySource`** for dashboards and analytics.
4. **Operational:** enable MVCC/changelog/encryption only when the service needs those deployment modes.

This staged use of HexxlaDB matches **HEXXLA.md**’s separation of **memory model** (here) from **storage layout** (**HEXXLA_DB.md**) and keeps the future HEXXLA binary free of **`internal/engine`** imports (**[HEXAGONAL_ARCHITECTURE.md](../context/HEXAGONAL_ARCHITECTURE.md)**).
