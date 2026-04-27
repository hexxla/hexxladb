<div align="center">

<img src="assets/images/hexxladb_logo.png" alt="HexxlaDB" width="300">

# HexxlaDB

**Persistent, structured, contradiction-aware memory for LLMs and agents.**

[![CI](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml)
[![Integration](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexxla/hexxladb.svg)](https://pkg.go.dev/github.com/hexxla/hexxladb)
[![Go Report Card](https://goreportcard.com/badge/github.com/hexxla/hexxladb)](https://goreportcard.com/report/github.com/hexxla/hexxladb)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](https://go.dev/doc/go1.26)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

---

Every LLM call today is stateless. Context windows are packed with RAG snippets that were retrieved by similarity alone — no provenance, no contradiction awareness, no memory of what the user said three sessions ago. When the model gives the wrong answer, there is no way to ask _"what did you actually know when you said that?"_

HexxlaDB is an embedded database built from scratch for this problem. It stores memories on a hexagonal coordinate grid where **spatial locality is a physical property of the on-disk format**, not a query-time approximation. Retrieval expands outward in deterministic rings — predictable, reproducible, and bounded by token budgets. Every memory carries provenance, confidence, and a validity window. When two memories contradict each other, the database doesn't silently overwrite one — it stores a **seam** that surfaces the conflict so the LLM can reason about it.

HexxlaDB also has a built-in **HNSW vector index** for embedding-based semantic search, so you can combine "find memories similar to this question" with "only include high-confidence facts from session 3 that are tagged as architecture decisions" — in a single query. The results feed directly into a token-budgeted context assembler that knows how to evict low-value memories and respect supersession chains.

The result: **an LLM that remembers across sessions, tracks preference changes, surfaces contradictions, and builds reproducible prompts — all from a single embedded Go library with zero network dependencies.**

---

## The problem

| What you get today                                  | What you actually need                                                                   |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Stateless API calls — context lost between sessions | Persistent memory that survives restarts and spans sessions                              |
| RAG retrieves by similarity alone                   | Retrieval that combines semantic similarity _with_ tags, confidence, source, and recency |
| User preferences silently overwritten               | Supersession chains that track how preferences evolve over time                          |
| Contradictions invisible to the model               | Explicit conflict markers the LLM can see and reason about                               |
| Token budget enforced by truncation                 | Intelligent eviction that drops low-confidence outer context first                       |
| No audit trail                                      | MVCC snapshots: "what did the model know at 3pm Tuesday?"                                |

---

## How it works

Every memory lives at a coordinate on a honeycomb grid. Related memories are placed near each other. When you need context for a prompt, HexxlaDB walks outward ring by ring from a seed coordinate — picking up the most relevant memories first, staying within your token budget, and automatically filtering out superseded or low-confidence content.

**Core primitives:**

| Primitive     | What it is                                                                                                                                                 |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Cell**      | A memory — a fact, message, preference, or document chunk — at a hex coordinate `(q, r)` with content, tags, provenance, confidence, and a validity window |
| **Seam**      | A visible marker linking two cells that contradict each other, with a reason, confidence delta, and resolution status                                      |
| **Edge**      | A directed relationship between cells ("see also", "follow-up", "derived from")                                                                            |
| **Facet**     | A summary or annotation cryptographically bound to a cell                                                                                                  |
| **Embedding** | A vector stored alongside a cell for semantic similarity search (HNSW-indexed)                                                                             |

---

## Quick start

```bash
go get github.com/hexxla/hexxladb
```

The following walkthrough shows the real production workflow — the same pipeline used by the [`llm_context_engine`](examples/llm_context_engine/) example. Every code block is copy-pasteable.

### 1. Open a database

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC:         true,  // snapshot isolation + time-travel
    EmbeddingDimension: 384,   // vector size (e.g. all-MiniLM-L6-v2)
    DistanceMetric:     hexxladb.DistanceCosine,
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### 2. Store a conversation turn with its embedding

Every message gets a cell with content, tags, provenance, and a vector embedding from your model of choice.

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

When a new user message arrives, embed it and search. HexxlaDB uses the HNSW graph for fast approximate nearest-neighbor lookup, then applies your filters as post-predicates.

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

### 4. Retrieve user preferences (for the system prompt)

Preferences are just cells with a `"preference"` tag. Query them separately so they always appear in the system prompt, regardless of what the user is asking about.

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

Take the top search results as seed coordinates and expand outward. The assembler walks concentric rings, fills your token budget, and automatically replaces superseded cells with their successors.

```go
db.View(func(tx *hexxladb.Tx) error {
    // Use the top-3 search results as seeds
    seeds := []hexxladb.Coord{results[0].Cell.Coord, results[1].Cell.Coord, results[2].Cell.Coord}

    pack, err := tx.LoadContextPackFrom(ctx,
        2,     // max ring radius
        4096,  // token budget
        hexxladb.ByteLenBudgeter{},
        hexxladb.LoadContextBudgetConfig{
            FilterSuperseded: true,  // old preferences auto-replaced by new ones
            IncludeSeams:     true,  // surface contradictions for the LLM
        },
        seeds...,
    )
    // pack.Cells: ordered context, pack.TotalTokens: fits your budget
})
```

### 6. Track contradictions and preference changes

When a user changes their mind, HexxlaDB doesn't silently overwrite — it records the relationship so context assembly can handle it automatically.

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

> **That's the full pipeline.** Embed → search → filter → assemble → prompt. Every step runs in-process, deterministically, with no network calls to the database layer. See [`examples/llm_context_engine`](examples/llm_context_engine/) for a complete runnable version.

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
| MVCC time-travel                   |    ✓     |     —      |     —     |    partial     |
| Reproducible prompt construction   |    ✓     |     —      |     —     |       —        |
| Provenance + confidence per memory |    ✓     |     —      |     —     |       —        |
| Embedded (no network)              |    ✓     |     —      |     —     |       ✓        |
| Encryption at rest                 |    ✓     |   varies   |     —     |       ✓        |

**Vector DBs** (Pinecone, Weaviate, Chroma) excel at similarity search but have no concept of contradiction, supersession, or token-budgeted assembly. **Graph DBs** (Neo4j) model relationships well but aren't embeddable and lack spatial coherence. **Temporal DBs** (Datomic) offer immutable history but no spatial indexing or LLM-aware retrieval. **General stores** (Postgres, SQLite) are reliable foundations, but hex coordinates, seam semantics, and context budgeting become application-level afterthoughts.

HexxlaDB is purpose-built: HNSW vector search, Morton-ordered spatial keys, contradiction-aware seams, MVCC snapshots, and token-budgeted context assembly — in a single embedded engine.

---

## Features

- **HNSW embedding search** — store vectors alongside cells; approximate nearest-neighbor retrieval with flat-scan fallback for small datasets
- **Hybrid queries** — combine embedding similarity with tag filters, confidence thresholds, source IDs, temporal ranges, and spatial predicates in one call
- **Hex-native spatial keys** — Morton-ordered `(q, r)` coordinates; ring walks are prefix scans that scale with ring area, not database size
- **Token-budgeted context assembly** — `LoadContextPackFrom` evicts low-confidence outer-ring cells first; spatial locality preserves semantic coherence
- **Contradiction tracking** — `MarkConflict` stores seams that surface disagreements; `IncludeSeams` injects them into context so models can reason about conflicts
- **Supersession chains** — `MarkSupersedes` records preference evolution; `FilterSuperseded` automatically replaces stale cells with their successors
- **MVCC time-travel** — `ViewAt` / `ViewAtTime` pin read snapshots; `SnapshotDiff` computes changes between any two points in time
- **Logical changefeed** — append-only changelog with op-code filtering for audit trails, CDC, and replication pipelines
- **AES-256-XTS encryption at rest** — passphrase or raw key; per-page encryption with HKDF-SHA256 / Argon2id key derivation
- **Configurable storage** — page sizes from 4 KiB to 64 KiB, overflow pages for values up to 1 MiB, always-on DEFLATE compression
- **Delete + compact** — MVCC tombstones, version pruning, copy-compaction for file size recovery
- **Zero dependencies at runtime** — single Go binary, no daemon, no network, no serialization overhead

---

## API at a glance

Full reference: [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md)

| Operation                                 | What it does                                                                |
| ----------------------------------------- | --------------------------------------------------------------------------- |
| `PutCell` / `GetCell` / `DeleteCell`      | Store, retrieve, or tombstone a memory                                      |
| `PutEmbedding` / `SearchByEmbedding`      | Store a vector; HNSW nearest-neighbor search                                |
| `QueryCells`                              | Hybrid search: embeddings + tags + confidence + source + temporal + spatial |
| `LoadContextPack` / `LoadContextPackFrom` | Token-budgeted context assembly with supersession filtering                 |
| `MarkConflict` / `MarkSupersedes`         | Record contradictions or preference changes                                 |
| `FindSeams`                               | Retrieve contradiction/supersession markers                                 |
| `SearchCells`                             | Lexical ranked search across content, tags, and source IDs                  |
| `ViewAt` / `ViewAtTime` / `SnapshotDiff`  | MVCC time-travel and change detection                                       |
| `Compact` / `CompactTo`                   | Copy-compaction for file size recovery                                      |
| `HealthCheck`                             | Structural integrity verification                                           |
| `TagCounts` / `TagCooccurrences`          | Tag analytics for memory exploration                                        |

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

| Example                                                  | Run                                       | What it demonstrates                                                                                                                |
| -------------------------------------------------------- | ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| [Conversational Memory](examples/conversational_memory/) | `go run ./examples/conversational_memory` | 12-phase production walkthrough: cells, seams, tags, MVCC time-travel, queries, context assembly, delete, compact                   |
| [LLM Context Engine](examples/llm_context_engine/)       | `go run ./examples/llm_context_engine`    | Full LLM memory pipeline with Ollama embeddings: ingest → semantic search → multi-signal retrieval → supersession → prompt assembly |

The LLM Context Engine example requires [Ollama](https://ollama.com/) with the `all-minilm` model:

```bash
ollama pull all-minilm
go run ./examples/llm_context_engine
```

---

## Documentation

| Document                                             | What's inside                                            |
| ---------------------------------------------------- | -------------------------------------------------------- |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) | Complete API reference — every exported symbol           |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)               | Memory model: hex lattice, seams, validity, supersession |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)         | Storage layout, key encoding, HNSW keyspace              |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)       | Production operations, benchmarks, backup, encryption    |
| [`ROADMAP.md`](docs/ROADMAP.md)                      | What's next and what's out of scope                      |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack

---

## Sponsorship

HexxlaDB is open source and under active development. If it's useful to your work — or you want to accelerate the roadmap (distributed replication, materialized views, richer seam semantics) — sponsorship is the most direct way to help.

- **GitHub Sponsors:** [github.com/sponsors/hexxla](https://github.com/sponsors/hexxla)
- **Monero (XMR):** `46shAhAihZ3dmVHGU4V6H2ZZt21ex8xydB7Awkxaheq4U1VZFoK53K92tsqhnL8roV2bV8pQWCryR3yNRJJd5gAeBsZUXPF`
- **Open Collective:** _coming soon_

Sponsors get early access to roadmap discussions, priority issue triage, and attribution in release notes.

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
