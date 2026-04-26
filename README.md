<div align="center">

<img src="assets/images/hexxla_logo.png" alt="HexxlaDB" width="300">

# HexxlaDB

**The storage engine that makes associative memory a first-class physical property.**

[![CI](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/ci.yml)
[![Integration](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml/badge.svg)](https://github.com/hexxla/hexxladb/actions/workflows/integration.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/hexxla/hexxladb.svg)](https://pkg.go.dev/github.com/hexxla/hexxladb)
[![Go Report Card](https://goreportcard.com/badge/github.com/hexxla/hexxladb)](https://goreportcard.com/report/github.com/hexxla/hexxladb)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](https://go.dev/doc/go1.26)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

LLM memory is a patchwork: RAG dumps, graph plugins, and "just stuff more tokens in the context" hacks. HexxlaDB is a different bet — it makes the entire storage engine a faithful implementation of a **structured, contradiction-aware, temporally-valid hexagonal memory model**, where spatial locality is not a query feature but a physical property of the on-disk format.

Every memory lives at a coordinate on a honeycomb grid. Retrieval expands outward in predictable rings — mirroring how associative human memory actually works. That means **repeated bounded-radius traversals with no random I/O tax**, deterministic context assembly that fits token budgets, and a database that can genuinely give agents persistent, inspectable, updatable, contradiction-resolving long-term memory at scale.

This is not "yet another vector DB with graph plugins." Spatial locality is baked into the primary key encoding. Contradictions are first-class citizens, not silent overwrites. **When two memories disagree, HexxlaDB surfaces the conflict so the LLM can reason about it.**

---

## Core concepts

| Primitive | What it is                                                                                                                       |
| --------- | -------------------------------------------------------------------------------------------------------------------------------- |
| **Cell**  | A memory — fact, message, or document chunk — at a hex coordinate `(q, r)` with content, tags, provenance, and a validity window |
| **Seam**  | A visible contradiction marker between two conflicting cells, with type, reason, confidence delta, and resolution status         |
| **Edge**  | A directed "see also" relationship between cells                                                                                 |
| **Facet** | A cryptographically-bound summary attached to a cell                                                                             |

---

## Quick start

```bash
go get github.com/hexxla/hexxladb
```

### Store a memory

```go
db, _ := hexxladb.Open("memories.db", nil)
defer db.Close()

center := hexxladb.Coord{Q: 0, R: 0}
db.Update(func(tx *hexxladb.Tx) error {
    return tx.PutCell(ctx, record.CellRecord{
        RawContent: "User prefers concise responses.",
        Tags:       []string{"preferences"},
        Provenance: record.ProvenanceWire{SourceID: "session-001", Confidence: 0.95},
    })
})
```

### Assemble context for an LLM prompt

```go
// Token-budgeted load: expands outward ring by ring, evicts outer rings first
pack, _ := tx.LoadContextPack(ctx, center, radius, maxTokens,
    hexxladb.ByteLenBudgeter{}, cfg)
// Same seed → same order → reproducible prompts every time
```

### Track a contradiction

```go
db.Update(func(tx *hexxladb.Tx) error {
    return tx.MarkConflict(cellA, cellB, "User reversed their stated preference")
})

// Seams surface automatically in LoadContextPack with IncludeSeams: true
seams, _ := tx.FindSeams(ctx, center, 2, true) // unresolved only
```

---

## Why HexxlaDB

| Need                       | How HexxlaDB delivers                                                                                                         |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| **Spatial locality**       | Morton-ordered hex keys — ring walks are prefix scans, not graph traversals; cost scales with ring area, not DB size          |
| **Token budgets**          | `LoadContextPack` evicts outer rings first; spatial locality preserves semantic coherence under budget pressure               |
| **Contradiction handling** | Seams are stored, queryable, and injected into context — the LLM sees the conflict rather than a silently overwritten fact    |
| **Reproducibility**        | Same coordinate + same radius + same seed → same cell order → identical prompt construction                                   |
| **Provenance & audit**     | Every cell carries source ID, confidence, and a validity window; MVCC snapshots let you ask "what did the DB know at time T?" |
| **Embedded, no network**   | In-process, no daemon, no serialization round-trips                                                                           |

### vs. existing approaches

- **Vector DBs** (Pinecone, Weaviate, Chroma) — great at similarity search; no contradiction model, no spatial coherence, no provenance
- **Graph DBs** (Neo4j) — rich relationships; no hex-native addressing, no seam semantics, not embeddable
- **Temporal DBs** (Datomic) — immutable history; no spatial indexing, no token-budget-aware retrieval
- **General stores** (Postgres, SQLite) — reliable; hex coordinates and seam semantics are application-level afterthoughts

HexxlaDB combines Morton-ordered spatial keys, seams as first-class storage primitives, MVCC time-travel, and token-budgeted context assembly in a single embedded engine. Nothing else does all four.

---

## API overview

See [`docs/hexxladb/API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) for the full reference.

**Core operations:**

- `PutCell` / `GetCell` — store and retrieve memories
- `LoadContext` / `LoadContextPack` — spatial context assembly with optional token budget
- `MarkConflict` / `FindSeams` — contradiction tracking
- `QueryCells` — tag, source, and spatial predicate queries
- `SearchCells` — lexical ranked search across cell content
- `ViewAt` / `ViewAtTime` — MVCC snapshot reads

---

## Benchmarks

```bash
make bench-api   # DB files land in .tmp/
```

Key characteristics confirmed by benchmarks:

- **Ring walks scale with ring area, not DB size** — `LoadContext` at r=3 costs ~500 µs on 512 or 2000 cells
- **`FindSeams` zero-seam fast path: ~26 µs** (down from ~2.3 ms; -98.9%)
- **Batch writes: ~0.34 ms/cell at batch=500** vs. ~8.3 ms/cell single `PutCell`
- **Point reads: ~28 µs** plain, stable across DB sizes — O(log n) B+ tree confirmed
- **MVCC version resolution: sub-millisecond up to 100 versions**

Full tables: [`docs/hexxladb/OPERATIONS.md`](docs/hexxladb/OPERATIONS.md).

_Hardware: Intel Core i9-14900HX, 16 GB RAM, Go 1.26, Linux. Results vary by storage speed and data shape._

---

## Documentation

| Document                                             | What's inside                        |
| ---------------------------------------------------- | ------------------------------------ |
| [`API_REFERENCE.md`](docs/hexxladb/API_REFERENCE.md) | Complete API reference               |
| [`HEXXLA.md`](docs/hexxladb/HEXXLA.md)               | Memory model and concepts            |
| [`HEXXLA_DB.md`](docs/hexxladb/HEXXLA_DB.md)         | Storage layout and key encoding      |
| [`OPERATIONS.md`](docs/hexxladb/OPERATIONS.md)       | Production operations and benchmarks |

---

## Projects using HexxlaDB

- **[Hexxla](https://github.com/hexxla/hexxla)** — spatial LLM memory and reasoning stack

---

## Sponsorship

HexxlaDB is open source and actively developed. If it's useful to your work — or you want to see the roadmap (vector hybrid retrieval, distributed replication, richer seam semantics) move faster — sponsorship is the most direct way to help.

- **GitHub Sponsors:** [github.com/sponsors/hexxla](https://github.com/sponsors/hexxla)
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
