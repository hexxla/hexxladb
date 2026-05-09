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

HexxlaDB is an embedded Go library for structured agent memory. Memories live at hex grid coordinates — retrieval expands outward in rings, bounded by your token budget. Every memory carries provenance, confidence, and a validity window. Contradictions are stored as seams, not silently overwritten.

Single binary, zero network dependencies, no daemon.

---

## How it works

Cells sit at `(q, r)` hex coordinates. Related memories sit nearby. `LoadContext` walks outward ring by ring from seed coordinates, filters superseded and low-confidence content, and returns a token-bounded slice ready to inject into a prompt.

| Primitive     | Description                                                                   |
| ------------- | ----------------------------------------------------------------------------- |
| **Cell**      | A memory at `(q, r)` — content, tags, provenance, confidence, validity window |
| **Seam**      | Conflict or supersession marker linking two cells                             |
| **Edge**      | Directed relationship between cells (graph overlay)                           |
| **Facet**     | Summary or annotation cryptographically bound to a cell                       |
| **Embedding** | Vector stored alongside a cell for HNSW similarity search                     |

---

## Quick start

```bash
go get github.com/hexxla/hexxladb
```

Full runnable example: [`examples/llm_context_engine`](examples/llm_context_engine/).

### Open a database

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC: true,
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### Store a memory and its embedding

```go
db.Update(func(tx *hexxladb.Tx) error {
    coord := hexxladb.Coord{Q: 3, R: 1}
    pk, _ := lattice.Pack(coord)

    err := tx.PutCell(ctx, record.CellRecord{
        Key:        pk,
        RawContent: "Use testcontainers-go for integration tests with real Postgres.",
        Tags:       []string{"fact", "testing", "database"},
        Provenance: record.ProvenanceWire{SourceID: "session-2", Confidence: 0.95},
    })
    if err != nil {
        return err
    }
    return tx.PutEmbedding(pk, vectorFromYourModel) // HNSW index maintained automatically
})
```

### Search by meaning

```go
db.View(func(tx *hexxladb.Tx) error {
    results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
        Embedding:     queryVector,
        ExcludeTags:   []string{"preference"},
        MinConfidence: 0.5,
        MaxResults:    8,
        SortBy:        hexxladb.SortByScore,
    })
    // results: ranked cells with score, content, tags, provenance
})
```

### Assemble a token-budgeted context window

```go
db.View(func(tx *hexxladb.Tx) error {
    pack, err := tx.LoadContext(ctx, hexxladb.LoadContextConfig{
        Seeds:   []hexxladb.Coord{results[0].Cell.Coord, results[1].Cell.Coord},
        MaxRing: 2,
        MaxTokens: 4096,
        Assembly: hexxladb.LoadContextBudgetConfig{
            FilterSuperseded: true,
            IncludeSeams:     true, // surface contradictions for the LLM
        },
    })
    // pack.Cells: ordered context within token budget
})
```

### Track contradictions and preference changes

```go
db.Update(func(tx *hexxladb.Tx) error {
    return tx.MarkSupersedes(newPrefCoord, oldPrefCoord, "User changed communication preference")
})
```

> **Pipeline:** embed → search → filter → assemble → prompt. All in-process, no network calls.

---

## Comparison

| Capability                         | HexxlaDB | Vector DBs | Graph DBs | General KV |
| ---------------------------------- | :------: | :--------: | :-------: | :--------: |
| Semantic search (HNSW)             |    ✓     |     ✓      |     —     |     —      |
| Structured filters in same query   |    ✓     |  partial   |     ✓     |     ✓      |
| Contradiction tracking             |    ✓     |     —      |     —     |     —      |
| Supersession chains                |    ✓     |     —      |     —     |     —      |
| Token-budgeted context assembly    |    ✓     |     —      |     —     |     —      |
| Spatial locality (ring walks)      |    ✓     |     —      |     —     |     —      |
| Graph pathfinding (A\*, BFS)       |    ✓     |     —      |     ✓     |     —      |
| MVCC time-travel                   |    ✓     |     —      |     —     |  partial   |
| Provenance + confidence per memory |    ✓     |     —      |     —     |     —      |
| Embedded (no network)              |    ✓     |     —      |     —     |     ✓      |
| Encryption at rest                 |    ✓     |   varies   |     —     |     ✓      |

---

## API

Full reference: [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)

| Operation                                               | What it does                                                            |
| ------------------------------------------------------- | ----------------------------------------------------------------------- |
| `PutCell` / `GetCell` / `DeleteCell`                    | Store, retrieve, or tombstone a memory                                  |
| `PutEmbedding` / `SearchByEmbedding`                    | Store a vector; HNSW nearest-neighbor search                            |
| `QueryCells` / `SearchCells`                            | Hybrid ANN+filter search or multi-term lexical search                   |
| `LoadContext` / `LoadContextFOV` / `LoadContextVoronoi` | Token-budgeted context assembly — ring walk, FOV, or multi-seed Voronoi |
| `FindEdgePath` / `WalkEdges`                            | A\* shortest path and BFS reachability over cell edges                  |
| `MarkConflict` / `MarkSupersedes` / `FindSeams`         | Record and retrieve contradictions and supersessions                    |
| `ViewAt` / `SnapshotDiff`                               | MVCC time-travel and change detection                                   |
| `Compact` / `HealthCheck`                               | Copy-compaction and structural integrity check                          |

---

## Performance

```bash
make bench-api
```

_Intel Core i9-14900HX · 16 GB · Go 1.26 · Linux · `-benchtime=3s -count=1`_

**Reads and ring traversal**

| Operation                             | Latency | Notes                                    |
| ------------------------------------- | ------- | ---------------------------------------- |
| `GetCell` (2k cells)                  | ~20 µs  | O(log n) B+ tree                         |
| `GetCell` encrypted (2k cells)        | ~21 µs  | AES-256-XTS; ~1 µs overhead vs plaintext |
| `GetCell` MVCC + encrypted (2k cells) | ~26 µs  | Combined MVCC version scan + decryption  |
| `WalkRing` r=2 (2k cells)             | ~162 µs | Scales with ring area, not DB size       |
| `QueryCells` tag-only (2k cells)      | ~15 µs  | Index-only; no page reads                |
| `QueryCells` spatial r=5 (2k cells)   | ~634 µs | Ring walk + filter                       |
| `QueryCells` combined (2k cells)      | ~62 ms  | source + spatial + confidence + sort     |
| `FindSeams` zero-seam fast-path       | ~1.3 µs | Pre-flight check; effectively free       |
| `FindSeams` 100 seams                 | ~995 µs | Seam index scan                          |

**Context assembly**

| Operation                         | Latency  | Notes                                           |
| --------------------------------- | -------- | ----------------------------------------------- |
| `LoadContext` r=3 (2k cells)      | ~753 µs  | Token-budgeted ring walk                        |
| `LoadContext` r=5 (2k cells)      | ~1.67 ms | Ring area grows as 3r²+3r+1                     |
| `LoadContextFOV` r=3 (2k cells)   | ~526 µs  | FOV skips occluded cells; faster than plain r=3 |
| `LoadContextFOV` r=5 (2k cells)   | ~1.30 ms | Open-field (no occlusion) — worst case for FOV  |
| `LoadContextVoronoi` 2 seeds (2k) | ~2.1 ms  | Non-overlapping region partition                |
| `LoadContextVoronoi` 4 seeds (2k) | ~4.4 ms  | Scales linearly with seed count                 |

**Writes**

| Operation                           | Latency       | Notes                                       |
| ----------------------------------- | ------------- | ------------------------------------------- |
| `PutCell` single write              | ~5.0 ms/op    | Single-writer B+ tree; fsync per commit     |
| `PutCell` MVCC                      | ~5.7 ms/op    | Version row + changelog overhead            |
| `BatchPutCells` batch=500           | ~0.09 ms/cell | vs ~5 ms/cell single-write (55× faster)     |
| `PutEmbedding` dim=32 (HNSW insert) | ~53 ms/op     | Full HNSW graph maintenance per write       |
| `PutEmbedding` dim=384              | ~74 ms/op     | Encode + graph insert scales with dimension |

**Semantic and lexical search**

| Operation                         | Latency   | Notes                                            |
| --------------------------------- | --------- | ------------------------------------------------ |
| `QueryCells` embedding (500×32d)  | ~13 ms    | Full HNSW ANN + post-filter pipeline             |
| `QueryCells` embedding (500×128d) | ~11 ms    | Higher dim; fewer graph candidates needed        |
| `SearchCells` lexical (2k cells)  | ~28–41 ms | Full-scan scorer; use tags/source for pre-filter |

**MVCC and maintenance**

| Operation                              | Latency | Notes                                         |
| -------------------------------------- | ------- | --------------------------------------------- |
| MVCC version resolution (100 versions) | ~1.1 ms | O(n) version scan per `GetCell`               |
| MVCC version resolution (500 versions) | ~5.5 ms | Prune old versions to keep this fast          |
| `SnapshotDiff` (10 writes)             | ~159 µs | Incremental CDC; scales linearly with range   |
| `SnapshotDiff` (500 writes)            | ~6.9 ms |                                               |
| `Compact` (512 cells)                  | ~67 ms  | Copy-compaction; run after heavy delete/prune |
| `Compact` (2k cells)                   | ~236 ms | One-time cost; DB is read-only during copy    |

---

## Examples

| Example                                                  | Run                 | What it covers                                               |
| -------------------------------------------------------- | ------------------- | ------------------------------------------------------------ |
| [Conversational Memory](examples/conversational_memory/) | `make demo`         | Cells, seams, tags, MVCC, queries, context, FOV, pathfinding |
| [LLM Context Engine](examples/llm_context_engine/)       | `make demo-llm`     | Ollama embeddings, semantic search, supersession, FOV        |
| [Spatial Algorithms](examples/spatial_algorithms/)       | `make demo-spatial` | FOV, LOD, Voronoi, A\*, BFS — side-by-side                   |

The LLM example requires [Ollama](https://ollama.com/): `ollama pull all-minilm && make demo-llm`

---

## Caveats

- **Write throughput** — B+ tree with single writer; not suited for high-volume random write workloads. Use batch writes (`db.Update` with many operations) where possible.
- **In-process only** — no network server; one process owns the file at a time.
- **HNSW at scale** — the graph is persisted in the B+ tree; very large vector sets (>10K) may hit page-split pressure. Flat-scan fallback is always available.
- **Coordinates are sparse** — hex grid is a logical namespace, not a dense array. No compaction of coordinate space happens automatically.
- **Pre-v1** — API may change between minor versions during the v0.y.z phase.

---

## Documentation

| Document                                             | What's inside                                            |
| ---------------------------------------------------- | -------------------------------------------------------- |
| [`CONFIGURATION.md`](docs/hexxladb/CONFIGURATION.md) | All `Options` fields, common configs, encryption setup   |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) | Complete API reference — every exported symbol           |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)               | Memory model: hex lattice, seams, validity, supersession |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)         | Storage layout, key encoding, HNSW keyspace              |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)       | Production ops, benchmarks, backup, encryption           |
| [`ROADMAP.md`](docs/ROADMAP.md)                      | What's next and what's out of scope                      |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack
- **[Mosaic](https://github.com/hexxla/mosaic)** — local MCP server for structured agent memory on HexxlaDB

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
