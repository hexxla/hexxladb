# HexxlaDB – Ideal Native Database Specification

**Version 1.3**
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

The physical layer may still use **B+-trees, leveled runs, or SSTable-style** components you own and tune for Morton keys—that is not the same as shipping a generic LSM wrapper.

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

**Key design:** Morton-packed coordinates and the prefixes in **Storage Layout** make ring enumeration, super-hex sharding, and locality-preserving operations first-class in the engine—not bolted on after the fact.

**v1 minimal engine shape:** Fixed-size pages (e.g. 16 or 64 KiB); B+-tree or leveled/SSTable structures with **Morton-prefixed keys** so spatial locality maps to on-disk locality; WAL for durability and crash recovery.

## Core Data Model

### 1. Cell

- Primary key: `PackedCoord` (128-bit or wider as needed, defined below).
- Fields:
  - `RawContent` (immutable).
  - `Provenance`.
  - `Validity`.
  - `Tags`.
  - `ClusterHint`.
- Indexes: coordinate (primary), `source_id`, tags, validity ranges.

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
- Additional indexes: resolution status, detection time (see secondary index above for cell participation).

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
- `seam-time/<valid_bucket>/<ulid>` → temporal index for seams (week bucket from seam validity `ValidFrom`)
- `seam-source/<source_id>/<ulid>` → source index for seams (length-prefixed `source_id` + ULID)
- `embed/<partition>/<vector_ref>` → **optional** future hybrid index (ANN pointer for seed selection); **not required in v1**—HexxlaDB has **no vector columns** and **no ANN indexes** in the minimal engine; seed selection is a **separate** orchestration concern (embeddings, lexical search, tags, or explicit coordinates are all valid **outside** the core store API—see **Hybrid Retrieval Path**).

`nbr/` keys are optional as noted above.

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

1. **Seed phase:** orchestration layer (not the storage engine) may use semantic similarity, lexical search, vector search, or explicit coordinates to choose candidate coordinates.
2. **Spatial expansion phase:** `walk_ring` or bounded radius traversal from seeds.
3. **Filtering and ranking phase:** apply provenance, validity, facet, seam, and tag filters.
4. **Context packing phase:** `load_context` assembles token-aware neighborhood.

### Example Ring Walk

SELECT cells in ring_keys(center, radius)
WHERE validity overlaps as_of_time
AND optional seam filters apply

## Scalability and Concurrency Features

- Locality-preserving sharding by super-hex regions.
- MVCC snapshots for consistent lattice views.
- Append-only event log or changefeed for full provenance.
- Materialized views for summary cells and cluster promotions.
- Hot/cold tiering: active rings in memory, cold data on SSD or object storage.

## Built-in Lattice Operations

- Axial to cube coordinate conversion.
- Hex distance calculation, as defined in **HEXXLA.md** (Geometric Model).
- Ring enumeration and spiral traversal.
- Super-hex clustering.
- Six-direction neighbor traversal.
- Facet rotation as a lightweight view operation.

## v1 Scope and Non-Goals

### v1 Scope

- Core objects with defined packing scheme.
- Ring walking and basic neighborhood operations.
- Cells, facets, edges, seams, and validity.
- **Embedded persistence** via the **custom HexxlaDB engine** (pages, WAL, Morton-keyed trees/levels); durable, crash-recoverable; **no** third-party ordered-KV/SQL core or SQLite.
- Core query primitives.
- **No embedding or ANN requirement:** operations are keyed by **`PackedCoord`** and non-vector indexes; optional `embed/` keyspace and hybrid retrieval are **future/optional**—v1 remains useful with explicit coords, tags, or external seeding only.

### Non-Goals

- Replacement for general-purpose vector databases.
- Replacement for general-purpose graph databases.
- Automatic truth assessment or global ranking.
- Experimental dynamics layer, which is reserved for post-v1.

## Data Flow Summary

Semantic Seed → Spatial Expansion (`walk_ring`) → Filter (validity, seams, facets) → Token-Aware Packing (`load_context`) → Prompt Context with optional seam highlights.

## Recommended Implementation Path

- **v1:** Implement the **custom engine** described in **HexxlaDB Architecture Position (Locked)**: **fixed pages (16 or 64 KiB)**, **WAL**, **Morton-prefixed B+-tree or SSTable-style levels**, and the keyspace in this document. Ship production-shaped durability and recovery from the first version; lattice ops stay in **`internal/lattice/`** and **`internal/engine/`**, not in a third-party KV adapter layer.
- **Later:** Optional replication, tiering (hot rings vs cold tiers), or distributed front-ends where operational needs require them—still on top of the same hex-native key and index contracts.

This keeps the hexagonal lattice as the organizing principle of storage, not a translation layer over a generic database.
