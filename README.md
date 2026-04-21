# HexxlaDB

**HexxlaDB** is an embedded storage engine for **[Hexxla](https://github.com/hexxla/hexxla)**: a hex-native, graph-aware, temporal, provenance-first database that treats hexagonal lattice as a first-class addressing model (Morton-packed keys, ring walks as native range scans, distinct Edge vs Seam storage families, MVCC for `as_of`). It is a custom on-disk engine—not a third-party ordered-KV or SQL core with adapters on top.

## What is HexxlaDB?

HexxlaDB is a **purpose-built embedded database** designed specifically for Hexxla memory model. Unlike generic key-value stores or SQL databases, HexxlaDB understands:

- **Hexagonal lattice geometry** as a first-class addressing model
- **Temporal semantics** with MVCC snapshot isolation (`as_of` queries)
- **Graph relationships** with native support for cells, edges, facets, and seams
- **Provenance tracking** with immutable audit trails
- **Spatial queries** via ring walks (native range scans on Morton-packed keys)

## What Problems Does HexxlaDB Solve?

### Traditional Database Limitations

- **No spatial awareness:** Generic databases treat coordinates as opaque strings
- **No temporal semantics:** No built-in support for historical queries or snapshots
- **No graph understanding:** Relationships require complex joins or external graph databases
- **No provenance:** Data lineage and audit trails are manual implementations

### HexxlaDB Solutions

- **✅ Native hex addressing:** Morton-packed coordinates enable efficient spatial queries
- **✅ Temporal MVCC:** Built-in snapshot isolation for historical data access
- **✅ Graph primitives:** First-class support for cells, edges, facets, and seams
- **✅ Spatial queries:** Ring walks as native range scans (O(log n) performance)
- **✅ Provenance tracking:** Immutable audit trails with changelog support
- **✅ Embedded deployment:** Single binary deployment with no external dependencies

## Problems This System Explicitly Aims to Fix

Current LLM long-term memory approaches have well-known failure modes that Hexxla directly targets:

### Token inefficiency & context bloat

Vector DB top-k or naive RAG dumps huge irrelevant chunks. Hexxla's concentric N-ring + token-budget dropping from outer rings gives deterministic, minimal, spatially coherent context.

### No inspectable locality / "black box" neighborhoods

You can't easily see "what is near this memory?" or walk outward in a predictable way. Hexxla makes neighborhood traversal a primitive operation with exact rings and spiral ordering.

### Contradictions are invisible or lossy

Most systems either overwrite, average embeddings, or silently keep conflicting facts. Hexxla makes contradictions explicit, queryable, visible in context pack, and resolvable with audit trail (merge / supersede / archive).

### Knowledge evolution is ad-hoc

No built-in temporal validity, facet rotation, or immutable raw anchor. Hexxla treats evolution as first-class (ValidityWindow + facets + seams + provenance).

### Update cost & re-embedding hell

Changing one fact often requires global re-indexing or expensive re-embedding. Hexxla's immutable raw cells + local seam creation + lazy facet derivation keeps updates cheap and local.

### No native spatial hierarchy

Pure vector or graph stores have no natural "zoom levels" or clustering that maps cleanly to token budgets. Super-hex regions + ring enumeration give exactly that.

## Key Improvements / Innovations Hexxla Delivers

- **Spatial determinism as organizing principle** (instead of pure semantic similarity)
- **Explicit contradiction engine** with visible, first-class Seams (post-v1 extensions like pollen diffusion etc. are explicitly out of scope for v1)
- **Multi-facet views** with strict derivation rules tied to raw content hash
- **Bi-temporal + provenance** baked in at cell/seam level
- **Token-aware context orchestration** that is geometrically principled rather than heuristic
- **Production-grade embedded persistence** that is hex-native from day one (the "locked" architecture position is very clear on this)

This is a genuinely different architecture from the current crop of memory systems (Mem0, GraphRAG, LangGraph memory, vector-only RAG, etc.). It trades some global semantic flexibility for inspectability, determinism, token efficiency, and contradiction transparency — exactly the properties that matter when an LLM is trying to maintain coherent long-term knowledge.

## Key Features

### Core Data Model

- **Cells:** Hexagonal lattice nodes with content and metadata
- **Edges:** Directed relationships between cells with temporal validity
- **Facets:** Derived content with hash-based validation
- **Seams:** Contradiction detection and resolution tracking
- **Validity Windows:** Temporal bounds for all data objects

### Query Primitives

```go
// Native spatial queries
LoadContext(center lattice.Coord, maxR, maxCells int) ([]CellRecord, error)
WalkRing(center lattice.Coord, radius int, filter Filter) ([]CellRecord, error)

// Temporal queries
ViewAtTime(asOf time.Time) (*Tx, error)
LoadContextAt(center lattice.Coord, maxR, maxCells int, asOf *time.Time) ([]CellRecord, error)

// Graph queries
FindSeams(cellA, cellB lattice.Coord, filter SeamFilter) ([]SeamRecord, error)
AscendCellsBySource(sourceID string) ([]CellRecord, error)
```

### Storage Engine

- **Custom B+ tree:** Optimized for write-heavy workloads with 64 KiB pages
- **WAL (Write-Ahead Log):** Durability with crash recovery
- **Morton-packed keys:** Efficient spatial locality and range scans
- **MVCC implementation:** Multi-version concurrency control with snapshot isolation

## Performance Characteristics

### Spatial Query Performance

| Operation   | Complexity   | Description                      |
| ----------- | ------------ | -------------------------------- |
| Ring Walk   | O(log n)     | Native range scan on sorted keys |
| Point Query | O(log n)     | Direct B+ tree lookup            |
| Range Query | O(log n + k) | B+ tree range scan               |

### Temporal Performance

| Operation        | Complexity | Description                                |
| ---------------- | ---------- | ------------------------------------------ |
| Snapshot         | O(1)       | Read-only transaction with consistent view |
| Historical Query | O(log n)   | MVCC version lookup                        |
| Version Cleanup  | O(n)       | Configurable retention policies            |

## Why Not Use Traditional Databases?

### SQLite/PostgreSQL

- **No hexagonal awareness:** Coordinates are opaque strings
- **Complex spatial queries:** Requires spatial extensions or custom functions
- **Limited temporal support:** No native MVCC or snapshot isolation

### Redis/BoltDB

- **No graph semantics:** Only key-value storage
- **No spatial queries:** No range scans or geometric operations
- **No temporal features:** No historical data access

### Graph Databases (Neo4j, etc.)

- **Heavyweight:** Complex setup and management overhead
- **Not embedded:** External services required
- **Performance overhead:** General-purpose graph algorithms vs. optimized spatial queries

## Use Cases

### LLM Memory Systems

- **Context retrieval:** Hexagonal neighborhoods around query points
- **Temporal reasoning:** Historical state reconstruction with `as_of` queries
- **Contradiction detection:** Seam identification and resolution tracking
- **Knowledge graphs:** Spatial relationships with provenance

### Geospatial Applications

- **Location-based queries:** Ring walks for neighborhood searches
- **Range queries:** Efficient spatial filtering and selection
- **Spatial indexing:** Morton codes for optimal data locality

### Temporal Applications

- **Version control:** Historical data access and rollback
- **Audit trails:** Complete provenance tracking
- **Snapshot isolation:** Consistent read-only views

## Getting Started

### Installation

```bash
go get github.com/hexxla/hexxladb
```

### Quick Start

```go
package main

import (
    "context"
    "log/slog"
    "github.com/hexxla/hexxladb"
    "github.com/hexxla/hexxladb/lattice"
)

func main() {
    // Open database
    db, err := hexxladb.Open("memory.db", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Write a cell at hexagonal coordinate
    center := lattice.Coord{Q: 0, R: 0}
    cell := hexxladb.CellRecord{
        Key:        lattice.MustPack(center),
        RawContent: "Hello Hexxla!",
        Provenance: hexxladb.ProvenanceWire{
            SourceID:   "example",
            Confidence: 1.0,
            CreatedAt:  time.Now().UnixNano(),
        },
    }

    err = db.Update(func(tx *hexxladb.Tx) error {
        return tx.PutCell(context.Background(), cell)
    })
    if err != nil {
        log.Fatal(err)
    }

    // Query hexagonal neighborhood
    cells, err := db.View(func(tx *hexxladb.Tx) error {
        return tx.LoadContext(context.Background(), center, 2, 10, hexxladb.Filter{})
    })
    if err != nil {
        log.Fatal(err)
    }

    slog.Info("Found cells in neighborhood", "count", len(cells))
}
```

## Architecture

### Hexagonal Memory Model

```
      / \ / \ / \
     / \ / \ / \
    / \ / \ / \
    / \ / \ / \
    / \ / \ / \
   / \ / \ / \
    / \ / \ / \
   / \ / \ / \
    \ / \ / \ / \
    \ / \ / \ / \
     \ / \ / \ / \
      \ / \ / \ / \
        \ / \ / \ / \
         \ / \ / \ / \
          \ /
```

### Storage Layout

```
┌─────────────────────────────────┐
│           B+ Tree Index            │
├─────────────────────────────────┤
│         WAL (Write-Ahead)         │
├─────────────────────────────────┤
│      64 KiB Pages (Primary)        │
├─────────────────────────────────┤
│     Morton-Packed Keys               │
└─────────────────────────────────┘
```

## Projects Using HexxlaDB

Below is a list of known projects that use HexxlaDB:

- **[Hexxla](https://github.com/hexxla/hexxla)** - LLM memory system and spatial reasoning platform
- **[Your Project Here](https://github.com/your/project)** - Add your project to this list!

## Contributing

If you're interested in contributing to HexxlaDB see [CONTRIBUTING.md](CONTRIBUTING.md).

### Development Setup

```bash
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go mod tidy
make test
```

## License

[Apache License 2.0](LICENSE)

## Contact

- Please use [Github issues](https://github.com/hexxla/hexxladb/issues) for filing bugs.
- Please use [discussions](https://github.com/hexxla/hexxladb/discussions) for questions and feature requests.
- Follow us on Twitter [@hexxla](https://twitter.com/hexxla) for updates.

**Quality gates:** **`make`** or **`make ci`** (runs **`scripts/ci.sh`** — same as GitHub Actions: format, **`go vet`**, tests with **`-race`**, **`govulncheck`**, **`golangci-lint`**, **`go mod tidy`**). Dependency update PRs: **`.github/dependabot.yml`**. Optional **[pre-commit](https://pre-commit.com)** hooks in **`.pre-commit-config.yaml`** — install with **`make pre-commit-install`**.
