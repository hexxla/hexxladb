# HexxlaDB – Ideal Native Database Specification

**Version 1.4**
**Date:** April 2026
**Project Name:** Hexxla

## Vision

The ideal database for Hexxla is a hex-native, graph-aware, temporal, provenance-first, hybrid-retrieval storage engine. It treats the hexagonal lattice as a first-class citizen, with axial coordinates as the primary addressing mechanism.

The design centers on five core objects that directly implement the Hexxla memory model: Cell, Edge, Facet, Seam, and Validity Window.

All operations are optimized for coordinate-based locality, bounded-radius neighborhood traversal, seam inspection, validity-filtered reads, and hybrid semantic-to-spatial retrieval.

This document is the canonical implementation specification.

## HexxlaDB Architecture Position (Locked)

**HexxlaDB** is a **custom, from-scratch, production-ready embedded database**: durable on-disk format, crash recovery, embeddable in Go binaries. It is **not** a third-party ordered-KV or SQL engine with lattice-shaped adapters on top—the storage engine is **hex-native**, so lattice operations are primitives, not translated queries.

**No SQLite; no third-party ordered-KV or SQL engine as the storage core** (e.g. embedding Pebble, RocksDB, or SQLite and adapting the lattice on top). Build a **hex-native** engine toward:

- `PackedCoord` keyspace with **Morton (Z-order)** ordering as a first-class encoding
- **Ring walks** and neighborhood loads as **native prefix/range scans** over that keyspace
- **Edge** volume and **Seam** conflict patterns as **two distinct storage families** with purpose-built indexes
- **MVCC** for `as_of` snapshots and consistent lattice views

The reference implementation uses a single **B+-tree** over Morton-prefixed keys ([`ORDERED_STORE.md`](../../internal/engine/ORDERED_STORE.md)). Alternative **leveled / SSTable-style** structures remain a **future** exploration if profiling demands—they are **not** part of the shipped v1 engine.

**SQLite** is not used.

**Core package layout** (illustrative):

```text
hexxladb/
├── Open(), DB, Batch — stable public API
├── internal/engine/   — pages, WAL, compaction, I/O
├── internal/lattice/  — coord packing, ring/range operations
├── internal/record/   — Cell, Edge, Seam, Facet serialization
└── internal/index/    — time, source, embed, seam-by-cell, etc.
```

Import **`hexxladb`** as the root package; keep the stable surface (`Open`, `DB`, `Batch`, options, query primitives) at the root.

**Edge vs Seam:** Distinct logical concepts and **distinct storage families**. **Edges** are lightweight, high-volume, typed/weighted relations for traversal and expansion. **Seams** are first-class contradiction artifacts with lifecycle, audit trail, and conflict-oriented indexing. **`Cell.Edges`** and seam lists in the API are **read aggregates** (materialized views), not denormalized primary storage.

**Seam primary key:** `seam/<ulid>` where `<ulid>` is a **ULID** (time-ordered 128-bit identifier). **Secondary:** `seam-by-cells/<packed_a>/<packed_b>/<ulid>` — endpoints in **canonical order** (e.g. lower `PackedCoord` first), i.e. a normalized pair plus id, for efficient `find_seams` by cell participation and radius.

**Concurrency and temporal support:** MVCC-style snapshot isolation is native, enabling `as_of` queries and consistent lattice views.

**Key design:** Morton-packed coordinates and the prefixes in **Storage Layout** make ring enumeration and locality-preserving scans first-class—**super-hex sharding** as an operational routing layer is **not** required for v1 (reserved high bits in `PackedCoord` support future partitioning ideas; see [`docs/ROADMAP.md`](../ROADMAP.md)).

**v1 minimal engine shape:** Configurable pages (**4 KiB** default, 4/8/16/64 KiB supported—[`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)); **one B+-tree** primary store + WAL for durability and crash recovery.

## Core Data Model

### 1. Cell

- Primary key: `PackedCoord` (128-bit or wider as needed, defined below).
- Fields:
  - `RawContent` (immutable).
  - `Provenance`.
  - `Validity`.
  - `Tags`.
  - `ClusterHint`.
- Secondary indexes (btree families): **`source/<source_id>/…`**, **`time/<valid_bucket>/…`**, and **`tag/<tag>/…`** for cells (see **Storage Layout**). **`Tags`** are stored on the cell record and mirrored into **`tag/`** for **[`Tx.AscendCellsByTag`](../../cell_secondary.go)** (same MVCC suffix rules as other cell secondaries when format v2 is enabled). Validity filtering uses **`time/`** plus record validity fields.

### 2. Facet

- Composite key: `facet/<packed_coord>/<facet_id>` where `facet_id` is 0 to 5.
- Fields:
  - `DerivedContent`.
  - `LastRotated`.
  - `DerivationHash`.
- Lifecycle follows rules defined in **HEXXLA.md** (Facets).

### 3. Edge

- Composite key: `edge/<packed_from>/<packed_to>/<type>`.
- Fields:
  - `RelationType`.
  - `Weight`.
  - `Provenance`.

### 4. Seam

- **Primary key:** `seam/<ulid>` — `<ulid>` is a time-ordered 128-bit identifier (ULID encoding recommended).
- **Secondary index:** `seam-by-cells/<packed_a>/<packed_b>/<ulid>` — `packed_a` and `packed_b` use **canonical ordering** (e.g. lower `PackedCoord` first) so each pair appears once.
- Fields (in the primary record):
  - `CellA`.
  - `CellB`.
  - `SeamType`.
  - `Reason`.
  - `ConfidenceDelta`.
  - `DetectedAt`.
  - `ResolutionStatus`.
  - `ResolutionNote`.
- Additional btree indexes for seams: **`seam-time/<bucket>/<ulid>`** (temporal bucket by validity / detection), **`seam-source/<source_id>/<ulid>`** (lexicographic by seam provenance source). **`ResolutionStatus`** lives on the **primary** `seam/<ulid>` record (filter after read or scan)—**not** a separate btree index family in v1.

Seam creation, detection, and resolution rules are defined normatively in **HEXXLA.md** (Contradiction Engine). This document covers only storage layout and indexing.

### 5. Validity and Temporal Layer

- Every Cell and Seam carries its own validity window.
- Queries support:
  - `as_of` system time using MVCC snapshot semantics.
  - `valid_during` real-world time range.

Neighbor pointers in `nbr/<packed_coord>/<direction>` are optional in minimal v1. They can be computed on the fly from the six fixed neighbor deltas for simplicity and smaller storage footprint. Precomputed pointers may be added later for performance without schema breakage.

## Coordinate Packing Scheme

To support efficient ring scans and super-hex sharding while aiming for good spatial locality:

1. Convert axial (q, r) to cube coordinates (q, r, s) where s = -q - r.
2. Zigzag-encode each signed coordinate to unsigned values.
3. Compute a Morton (Z-order) code by interleaving the bits of the encoded values.

The resulting PackedCoord (typically 128-bit or wider) is used as the primary key. High-order bits naturally support super-hex region identifiers for sharding.

Morton ordering is chosen because space-filling curves of this family map spatially close points to nearby keys with high probability, which is expected to benefit neighborhood operations. However, actual locality improvements for hex ring scans and sharding must be validated through benchmarks before claiming superiority over simpler schemes.

## Storage Layout

- `cell/<packed_coord>` → full cell record
- `facet/<packed_coord>/<facet_id>` → facet data
- `edge/<packed_from>/<packed_to>/<type>`
- `seam/<ulid>` → full seam record
- `seam-by-cells/<packed_a>/<packed_b>/<ulid>` → secondary index (canonical cell pair + id)
- `time/<valid_bucket>/<packed_coord>` → temporal index (cells)
- `source/<source_id>/<packed_coord>` (cells; encoding: length-prefixed `source_id` + packed coord — see implementation)
- `tag/<tag>/<packed_coord>` → tag index for cells (length-prefixed UTF-8 tag + packed coord — see **`internal/index/tag_key.go`**)
- `seam-time/<valid_bucket>/<ulid>` → temporal index for seams (week bucket from seam validity `ValidFrom`)
- `seam-source/<source_id>/<ulid>` → source index for seams (length-prefixed `source_id` + ULID)
- `embed/<packed_coord>` → fixed-dimension float32 vector (little-endian). Enabled when [`Options.EmbeddingDimension`](../../options.go) > 0; dimension and distance metric are persisted in the file header and immutable after creation. One embedding per cell. **[`Tx.PutEmbedding`](../../tx_embedding.go)** / **[`Tx.GetEmbedding`](../../tx_embedding.go)** / **[`Tx.DeleteEmbedding`](../../tx_embedding.go)**; **[`Tx.SearchByEmbedding`](../../embedding_search.go)** for HNSW-accelerated nearest-neighbor search (flat-scan fallback); **[`Tx.ReindexEmbeddings`](../../embedding_reindex.go)** for bulk recompute. **[`DeleteCell`](../../delete_cell.go)** cascades to remove the embedding and HNSW node.
- `hnsw/meta` → HNSW graph metadata (M, efConstruction, maxLayer, count). Created automatically on first `PutEmbedding`.
- `hnsw/entry` → HNSW entry point coordinate (16 bytes big-endian PackedCoord).
- `hnsw/node/<packed_coord>` → HNSW node (maxLayer + per-layer neighbor lists). Maintained automatically by `PutEmbedding`/`DeleteEmbedding`.
- **[`CellQuery.Embedding`](../../query.go)** / **[`CellSearchConfig.Embedding`](../../search.go)** triggers ANN-accelerated seed selection in **`QueryCells`** / **`SearchCells`**; embedding similarity is added to the composite relevance score.

`nbr/` keys are optional as noted above.

### MVCC physical keys (`format_version` ≥ 2)

Logical layout above describes **logical** families. When **[`Options.EnableMVCC`](../../options.go)** opens a **format v2** database, **version suffixes** are appended to physical btree keys for cells, facets, edges, seams, and secondaries (including **`source/`**, **`time/`**, **`tag/`**) so multiple committed versions coexist; visibility is **`commit_seq`**-scoped (**[`ViewAt`](../../tx.go)**, **`ViewAtTime`**). Snapshot semantics live in **[`TX.md`](./TX.md)**; encoding helpers include **[`cell_version.go`](../../internal/index/cell_version.go)**, **[`facet_key.go`](../../internal/index/facet_key.go)** (`FacetKeyWithVersion`), **[`seam_version_key.go`](../../internal/index/seam_version_key.go)**, **[`tag_key.go`](../../internal/index/tag_key.go)**, and secondary key helpers with version suffixes in **`internal/index`**.

Timeline keys **`__meta/commit-time/<wall_nanos>/<commit_seq>`** map wall-clock **`as_of`** to snapshots (**[`TX.md`](./TX.md)**). They are **not** optional for **`ViewAtTime`** on format v2.

### Logical changefeed (auxiliary file)

Optional append-only **changelog** for consumers lives in a **sidecar file** `{primary}-changelog` when enabled—not a btree prefix. See **[`CHANGEFEED.md`](./CHANGEFEED.md)** and **`internal/changelog`**.

## Native Query Primitives

- `put_cell(coord, raw_content, provenance, validity_window, initial_tags)`
- `update_facet(coord, facet_id, derived_content)` anchored to current raw hash.
- `link_cells(from, to, relation_type, weight)`
- `walk_ring(center_coord, radius, facet_mask, filters)` returning cells and selected facets in exact rings.
- `load_context(center_coord, max_tokens, filters)` returning a token-aware neighborhood.
- `find_seams(center_coord, radius, unresolved_only)`
- `resolve_seam(seam_id, resolution_strategy, optional_new_content)` — `seam_id` is the ULID string matching the `seam/<ulid>` primary key.
- `mark_conflict(coord_a, coord_b, reason)` for explicit manual seam creation.

## Hybrid Retrieval Path

**Seed selection is orthogonal to HexxlaDB’s core primitives:** the system only needs a starting **`Coord`** (or small set of candidates). Embeddings are **one** supported option for step 1; others include **explicit coordinates**, **lexical or tag-based lookup**, **`source_id`**, or **agent-driven navigation**. Everything **after** the seed is **deterministic** on the lattice—no embeddings required inside the DB.

**Content search as seed selection:** `Tx.SearchCells` provides ranked `[]CellSearchResult`; each carries a scored `Coord` suitable for direct use as a seed in step 2. `CellSearchConfig` supports lexical/substring `Query` and optional `Embedding []float32` for ANN-accelerated seed selection. Pass multiple result coordinates through `LoadContextConfig.Seeds` for merged, deduplicated context assembly under a shared token budget.

1. **Seed phase:** orchestration layer (not the storage engine) may use semantic similarity, lexical search (`SearchCells`), vector search, or explicit coordinates to choose candidate coordinates.
2. **Spatial expansion phase:** `walk_ring` or bounded radius traversal from seeds.
3. **Filtering and ranking phase:** apply provenance, validity, facet, seam, and tag filters.
4. **Context packing phase:** `Tx.LoadContext` assembles a token-aware neighborhood from one or multiple `LoadContextConfig.Seeds` under a shared budget.

### Example Ring Walk

SELECT cells in ring_keys(center, radius)
WHERE validity overlaps as_of_time
AND optional seam filters apply

## Scalability and Concurrency Features

**Shipped in v1 (reference implementation):**

- **MVCC** snapshots for consistent lattice views (see [`TX.md`](./TX.md) § MVCC temporal semantics).
- Optional **logical changefeed** (`{db}-changelog`) when configured ([`CHANGEFEED.md`](./CHANGEFEED.md)).
- Rebuildable in-memory **aperture-7 super-hex occupancy summaries** derived from a consistent snapshot and incrementally maintained from that changefeed. They add no B+ tree key family and do not alter `PackedCoord`.

**Future / product-tier (not required for embedded v1):**

- Locality-preserving **sharding** by super-hex or region prefixes.
- Persistent/content-bearing **materialized views** for summary cells and cluster promotions. The shipped in-memory occupancy prototype validates hierarchy and incremental-consumer semantics; persistent summary payloads remain future work.
- **Hot/cold tiering**: active rings in memory, cold data on SSD or object storage.

**Repository tracking** of out-of-scope items, near-term candidates, and **spec vs implementation** notes: **[`../ROADMAP.md`](../ROADMAP.md)**.

## Built-in Lattice Operations

- Axial to cube coordinate conversion.
- Hex distance calculation, as defined in **HEXXLA.md** (Geometric Model).
- Ring enumeration and spiral traversal.
- Six-direction neighbor traversal.
- **`ClusterHint`** on cells for product-level clustering hints. `SuperHexSummaryIndex` provides rebuildable in-memory aperture-7 occupancy aggregation; persistent/content-bearing super-hex pages are not shipped in v1.
- Facet rotation as a **client/view** concern (`ActiveFacet` is not a dedicated disk field; the library exposes raw facet APIs via **`Tx.PutFacet`** / **`Tx.GetFacet`**).

## v1 Scope and Non-Goals

### v1 Scope

- Core objects with defined packing scheme.
- Ring walking and basic neighborhood operations.
- Cells, facets, edges, seams, and validity.
- **Embedded persistence** via the **custom HexxlaDB engine** (pages, WAL, Morton-keyed **B+-tree**); durable, crash-recoverable; **no** third-party ordered-KV/SQL core or SQLite.
- Core query primitives.
- **Embedding keyspace (HNSW):** optional `embed/<packed_coord>` stores one fixed-dimension float32 vector per cell (dimension + metric locked at creation). HNSW-backed approximate nearest-neighbor search with flat-scan fallback. `CellQuery.Embedding` / `CellSearchConfig.Embedding` integrate vector search into the query planner. v1 remains useful without embeddings—explicit coords, tags, lexical search, or external seeding are all supported.

### Non-Goals

- Replacement for general-purpose vector databases.
- Replacement for general-purpose graph databases.
- Automatic truth assessment or global ranking.
- Experimental dynamics layer, which is reserved for post-v1.

## Data Flow Summary

Semantic Seed → Spatial Expansion (`walk_ring`) → Filter (validity, seams, facets) → Token-Aware Packing (`load_context`) → Prompt Context with optional seam highlights.

## Recommended Implementation Path

- **v1 (reference):** **configurable page size** (4/8/16/64 KiB; default 4 KiB), **WAL**, **Morton-prefixed B+-tree**, keyspace in this document; lattice ops in **`internal/lattice/`**, persistence in **`internal/engine/`**. See **[`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)** for on-disk header and versioning.
- **Later:** Optional SSTable/leveled tiers if benchmarks justify; replication, tiering, distributed front-ends—still on the same hex-native **logical** key contracts.

This keeps the hexagonal lattice as the organizing principle of storage, not a translation layer over a generic database.
