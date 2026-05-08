<div align="center">

<img src="assets/images/hexxladb_logo_shadow.svg" alt="HexxlaDB" width="240">

# HexxlaDB

**Embedded database for LLM memory — hex grid, vector search, contradiction tracking.**

[![CI](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml)
[![Integration](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexxla/hexxladb.svg)](https://pkg.go.dev/github.com/hexxla/hexxladb)
[![Go Report Card](https://goreportcard.com/badge/github.com/hexxla/hexxladb)](https://goreportcard.com/report/github.com/hexxla/hexxladb)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](https://go.dev/doc/go1.26)
[![Version](https://img.shields.io/github/v/tag/hexxla/hexxladb?label=version&color=7c3aed)](https://github.com/hexxla/hexxladb/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

---

HexxlaDB stores memories on a hexagonal coordinate grid where spatial locality is part of the on-disk format. Retrieval expands outward in deterministic rings, bounded by token budgets. Every memory carries provenance, confidence, and a validity window. Contradictions are stored as seams, not silently overwritten.

Built-in HNSW vector index, spatial algorithms (FOV, LOD, Voronoi, A\* pathfinding), MVCC snapshots, and token-budgeted context assembly — single embedded Go library, zero network dependencies.

---

## How it works

Memories live at `(q, r)` hex coordinates. Related memories sit nearby. Context retrieval walks outward ring by ring from seed coordinates, staying within your token budget and filtering out superseded or low-confidence content.

| Primitive     | Description                                                                   |
| ------------- | ----------------------------------------------------------------------------- |
| **Cell**      | A memory at `(q, r)` — content, tags, provenance, confidence, validity window |
| **Seam**      | Conflict/supersession marker linking two cells                                |
| **Edge**      | Directed relationship between cells (graph overlay)                           |
| **Facet**     | Summary or annotation cryptographically bound to a cell                       |
| **Embedding** | Vector stored alongside a cell for HNSW similarity search                     |

---

## Quick start

```bash
go get github.com/hexxla/hexxladb
```

Complete runnable version: [`examples/llm_context_engine`](examples/llm_context_engine/). Every block below is copy-pasteable.

### 1. Open a database

Full options reference: [`CONFIGURATION.md`](docs/hexxladb/CONFIGURATION.md).

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC: true, // snapshot isolation + time-travel
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
// Embedding dimension is auto-detected from the first PutEmbedding call.
// To pre-set: Options{EmbeddingDimension: 384, DistanceMetric: hexxladb.DistanceCosine}
```

### 2. Store a conversation turn with its embedding

```go
db.Update(func(tx *hexxladb.Tx) error {
    coord := hexxladb.Coord{Q: 3, R: 1}
    pk, _ := lattice.Pack(coord)

    // Store the memory
    err := tx.PutCell(ctx, record.CellRecord{
        Key:        pk,
        RawContent: "Use testcontainers-go for integration tests with real Postgres.",
        Tags:       []string{"fact", "testing", "database", "best-practice"},
        Provenance: record.ProvenanceWire{SourceID: "session-2", Confidence: 0.95},
    })
    if err != nil {
        return err
    }

    // Store its embedding (HNSW index is maintained automatically)
    return tx.PutEmbedding(pk, vectorFromYourModel)
})
```

### 3. Find relevant memories by meaning

```go
db.View(func(tx *hexxladb.Tx) error {
    results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
        Embedding:     queryVector,          // "How do I test my HTTP handlers?"
        ExcludeTags:   []string{"preference"}, // keep preferences separate
        MinConfidence: 0.5,
        MaxResults:    8,
        SortBy:        hexxladb.SortByScore,
    })
    // results: ranked cells with score, content, tags, provenance
})
```

### 4. Retrieve user preferences

```go
db.View(func(tx *hexxladb.Tx) error {
    prefs, err := tx.QueryCells(ctx, hexxladb.CellQuery{
        RequireTags: []string{"preference"},
        MaxResults:  5,
        SortBy:      hexxladb.SortByConfidence,
    })
    // prefs: "concise responses", "table-driven tests", etc.
})
```

### 5. Assemble a token-budgeted context window

```go
db.View(func(tx *hexxladb.Tx) error {
    // Use the top-3 search results as seeds
    seeds := []hexxladb.Coord{
        results[0].Cell.Coord,
        results[1].Cell.Coord,
        results[2].Cell.Coord,
    }

    pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
        Seeds:     seeds,
        MaxRing:   2,    // expand up to 2 rings from each seed
        MaxTokens: 4096, // token budget
        Assembly: hexxladb.LoadContextBudgetConfig{
            Assemble:         hexxladb.DefaultAssembleCellViewOpts(),
            FilterSuperseded: true, // old preferences auto-replaced by new ones
            IncludeSeams:     true, // surface contradictions for the LLM
        },
    })
    // pack.Cells: ordered context, pack.TotalTokens: fits your budget
})
```

### 6. Track contradictions and preference changes

```go
db.Update(func(tx *hexxladb.Tx) error {
    // User now wants verbose explanations (previously wanted brevity)
    return tx.MarkSupersedes(newPrefCoord, oldPrefCoord, "User changed communication preference")
})

// Or flag an outright contradiction between two facts
db.Update(func(tx *hexxladb.Tx) error {
    return tx.MarkConflict(cellA, cellB, "Conflicting architecture recommendations")
})
```

> **Pipeline:** embed → search → filter → assemble → prompt. All in-process, deterministic, no network calls.

---

## What makes this different

| Capability                         | HexxlaDB | Vector DBs | Graph DBs | General stores |
| ---------------------------------- | :------: | :--------: | :-------: | :------------: |
| Semantic search (HNSW)             |    ✓     |     ✓      |     —     |       —        |
| Structured filters in same query   |    ✓     |  partial   |     ✓     |       ✓        |
| Contradiction tracking             |    ✓     |     —      |     —     |       —        |
| Supersession chains                |    ✓     |     —      |     —     |       —        |
| Token-budgeted context assembly    |    ✓     |     —      |     —     |       —        |
| Spatial locality (ring walks)      |    ✓     |     —      |     —     |       —        |
| Visibility filtering (FOV)         |    ✓     |     —      |     —     |       —        |
| Graph pathfinding (A\*, BFS)       |    ✓     |     —      |     ✓     |       —        |
| MVCC time-travel                   |    ✓     |     —      |     —     |    partial     |
| Reproducible prompt construction   |    ✓     |     —      |     —     |       —        |
| Provenance + confidence per memory |    ✓     |     —      |     —     |       —        |
| Embedded (no network)              |    ✓     |     —      |     —     |       ✓        |
| Encryption at rest                 |    ✓     |   varies   |     —     |       ✓        |

HexxlaDB combines HNSW vector search, spatial indexing, contradiction tracking, and token-budgeted assembly in one embedded engine. Other databases cover subsets of this; none cover the full stack.

---

## Features

### Search & retrieval

- **HNSW vector search** — ANN with flat-scan fallback; vectors stored alongside cells
- **Hybrid queries** — embeddings + tags + confidence + source + temporal + spatial in one call (`QueryCells`)
- **Lexical search** — multi-term ranked matching across content, tags, source IDs; auto-tokenized on whitespace (`SearchCells`, `QueryCells`)

### Spatial algorithms

- **Hex-native keys** — Morton-ordered `(q, r)` coordinates; ring walks scale with ring area, not DB size
- **Field of view** — LOS ray casting skips cells occluded behind empty regions (`LoadContextFOV`)
- **Level of detail** — full resolution nearby, coarsened outer rings (auto-applied by `LoadContext` when `MaxRing >= 10`)
- **Voronoi partitioning** — non-overlapping regions for fair multi-seed budget splits (`LoadContextVoronoi`)
- **Pathfinding** — A\* shortest path, BFS reachability, graph-traversal context loading via `EdgeFilter` (`FindEdgePath`, `WalkEdges`, `LoadContext`)

### Memory management

- **Token-budgeted assembly** — evicts low-confidence outer-ring cells first; auto-dispatches ring walk, LOD, graph BFS, or multi-seed merge (`LoadContext`)
- **Contradiction tracking** — seams surface disagreements in context (`MarkConflict`)
- **Supersession chains** — stale cells auto-replaced by successors (`MarkSupersedes` + `FilterSuperseded`)
- **MVCC time-travel** — pin snapshots, diff between any two points (`ViewAt`, `SnapshotDiff`)

### Storage & operations

- **Encryption at rest** — AES-256-XTS, passphrase or raw key, Argon2id derivation
- **Configurable pages** — 4 KiB to 64 KiB, overflow to 1 MiB, always-on DEFLATE
- **Delete + compact** — tombstones, version pruning, copy-compaction
- **Changefeed** — append-only changelog with op-code filtering
- **Zero runtime deps** — single Go binary, no daemon, no network

---

## API at a glance

Full reference: [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)

| Operation                                | What it does                                                                                        |
| ---------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `PutCell` / `GetCell` / `DeleteCell`     | Store, retrieve, or tombstone a memory                                                              |
| `PutEmbedding` / `SearchByEmbedding`     | Store a vector; HNSW nearest-neighbor search                                                        |
| `QueryCells`                             | Hybrid search: embeddings + tags + confidence + source + temporal + spatial                         |
| `LoadContext`                            | Token-budgeted context assembly — ring walk, LOD, graph BFS, or multi-seed dispatched automatically |
| `LoadContextFOV`                         | Visibility-filtered context — skip cells occluded behind empty regions                              |
| `LoadContextVoronoi`                     | Voronoi-partitioned context loading for non-overlapping multi-region splits                         |
| `FindEdgePath` / `WalkEdges`             | A\* shortest path and BFS reachability over cell edges                                              |
| `MarkConflict` / `MarkSupersedes`        | Record contradictions or preference changes                                                         |
| `FindSeams`                              | Retrieve contradiction/supersession markers                                                         |
| `SearchCells`                            | Multi-term lexical ranked search across content, tags, and source IDs                               |
| `ViewAt` / `ViewAtTime` / `SnapshotDiff` | MVCC time-travel and change detection                                                               |
| `Compact` / `CompactTo`                  | Copy-compaction for file size recovery                                                              |
| `HealthCheck`                            | Structural integrity verification                                                                   |
| `TagCounts` / `TagCooccurrences`         | Tag analytics for memory exploration                                                                |

---

## Performance

```bash
make bench-api
```

| Operation                   | Latency       | Notes                                                                 |
| --------------------------- | ------------- | --------------------------------------------------------------------- |
| **Ring walk (r=3)**         | ~500 µs       | Stable across 512 and 2000 cells — scales with ring area, not DB size |
| **HNSW search**             | sub-ms        | 500 vectors at 32 dimensions; flat-scan fallback for small datasets   |
| **Point read**              | ~28 µs        | O(log n) B+ tree; stable across DB sizes                              |
| **Batch write**             | ~0.34 ms/cell | At batch size 500; vs ~8.3 ms/cell for single writes                  |
| **FindSeams (zero-seam)**   | ~26 µs        | Pre-flight fast path; down from 2.3 ms (-98.9%)                       |
| **MVCC version resolution** | sub-ms        | Up to 100 versions per cell                                           |

Full tables and methodology: [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

_Hardware: Intel Core i9-14900HX, 16 GB, Go 1.26, Linux._

---

## Examples

| Example                                                  | Run                 | What it covers                                                                     |
| -------------------------------------------------------- | ------------------- | ---------------------------------------------------------------------------------- |
| [Conversational Memory](examples/conversational_memory/) | `make demo`         | 14-phase walkthrough: cells, seams, tags, MVCC, queries, context, FOV, pathfinding |
| [LLM Context Engine](examples/llm_context_engine/)       | `make demo-llm`     | Ollama embeddings: semantic search, multi-signal retrieval, supersession, FOV      |
| [Spatial Algorithms](examples/spatial_algorithms/)       | `make demo-spatial` | FOV, LOD, Voronoi, A\* pathfinding, BFS — side-by-side comparison                  |

All targets clean the DB before running. Override paths: `make demo DEMO_DB=/path/to/my.db`. The LLM example requires [Ollama](https://ollama.com/):

```bash
ollama pull all-minilm
make demo-llm
```

---

## Documentation

| Document                                             | What's inside                                                         |
| ---------------------------------------------------- | --------------------------------------------------------------------- |
| [`CONFIGURATION.md`](docs/hexxladb/CONFIGURATION.md) | **Database creation, all Options fields, common configs, encryption** |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) | Complete API reference — every exported symbol                        |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)               | Memory model: hex lattice, seams, validity, supersession              |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)         | Storage layout, key encoding, HNSW keyspace                           |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)       | Production operations, benchmarks, backup, encryption                 |
| [`ROADMAP.md`](docs/ROADMAP.md)                      | What's next and what's out of scope                                   |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack
- **[Mosaic](https://github.com/hexxla/mosaic)** — local MCP server for structured agent memory (hex lattice, hybrid retrieval, policy-governed context) on HexxlaDB

---

## Sponsorship

- **GitHub Sponsors:** [github.com/sponsors/hexxla](https://github.com/sponsors/hexxla)
- **Monero (XMR):** `46shAhAihZ3dmVHGU4V6H2ZZt21ex8xydB7Awkxaheq4U1VZFoK53K92tsqhnL8roV2bV8pQWCryR3yNRJJd5gAeBsZUXPF`

---

## Contributing

See **[CONTRIBUTING.md](CONTRIBUTING.md)**.

```bash
git clone https://github.com/hexxla/hexxladb.git
cd hexxladb
go mod tidy
make test
```

---

## Contact

- [Issues](https://github.com/hexxla/hexxladb/issues) — bugs and feature requests
- [Discussions](https://github.com/hexxla/hexxladb/discussions) — questions and ideas
