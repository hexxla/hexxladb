# HexxlaDB storage model and key layout

This document defines the logical records and key families implemented by HexxlaDB. Page and WAL bytes are specified separately in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md); product memory concepts are defined in [`HEXXLA.md`](./HEXXLA.md).

HexxlaDB is an embedded, hex-native database implemented by this module. The stable public package is `github.com/hexxla/hexxladb`; private storage components live under `internal/`.

## Storage design

The engine uses one B+ tree over byte-ordered key families, backed by fixed-size pages and a redo WAL. Coordinates are Morton-packed so spatial addresses have a stable ordered representation. Lattice operations, records, and secondary indexes are part of the database implementation rather than adapters over a third-party SQL or key-value engine.

The principal private packages are:

| Package            | Owns                                                      |
| ------------------ | --------------------------------------------------------- |
| `internal/engine`  | File header, pages, B+ tree, WAL, locking, and durability |
| `internal/lattice` | Coordinates, packing, rings, and pure geometry            |
| `internal/record`  | Cell, facet, edge, and seam encodings                     |
| `internal/index`   | Key-family encodings and MVCC suffixes                    |
| `internal/hnsw`    | Persisted approximate-nearest-neighbor graph mechanics    |

Applications must use the public root API rather than importing these packages.

## Logical records

### Cell

A cell is addressed by `PackedCoord` and stores raw content, provenance, validity, tags, and an optional cluster hint. `PutCell` maintains the applicable source, time, tag, embedding, and MVCC data associated with the logical cell.

### Facet

A facet uses `(PackedCoord, facetID)` where `facetID` is `0..5`. It stores derived content, rotation time, and a derivation hash. `UpdateFacet` verifies that the derivation hash still matches the cell's raw content.

### Edge

An edge uses `(from, to, relationType)` and stores a directed relation, weight, and provenance. Edges support adjacency walks and weighted pathfinding.

### Seam

A seam is identified by a ULID and stores two cell endpoints, type, reason, confidence delta, detection time, resolution state, validity, and provenance. Endpoints are immutable for a given ULID.

The `seam-by-cells` secondary uses canonically ordered endpoints so the pair has one normalized representation. Seams are separate from ordinary edges because they carry contradiction and resolution lifecycle data.

## Coordinate packing

HexxlaDB converts axial `(q, r)` coordinates to cube coordinates, zigzag-encodes signed components, and interleaves the bits into a Morton `PackedCoord`.

The encoding provides a stable sortable key and useful spatial locality, but it does not make an exact hex ring one contiguous byte range. Ring enumeration remains a lattice operation. Bounds and byte layout are specified in [`internal/lattice/PACKED_COORD.md`](../../internal/lattice/PACKED_COORD.md).

## Logical key families

The notation below describes logical identities. Concrete encodings use binary separators, length prefixes, and fixed-width fields defined in `internal/index` and `internal/record`.

| Family             | Logical key and value                                                |
| ------------------ | -------------------------------------------------------------------- |
| Cell               | `cell/<packed_coord>` → cell record                                  |
| Facet              | `facet/<packed_coord>/<facet_id>` → facet record                     |
| Edge               | `edge/<packed_from>/<packed_to>/<type>` → edge record                |
| Seam               | `seam/<ulid>` → seam record                                          |
| Seam endpoints     | `seam-by-cells/<packed_a>/<packed_b>/<ulid>` → empty secondary value |
| Cell source        | `source/<source_id>/<packed_coord>` → empty secondary value          |
| Cell validity time | `time/<week_bucket>/<packed_coord>` → empty secondary value          |
| Cell tag           | `tag/<tag>/<packed_coord>` → empty secondary value                   |
| Seam source        | `seam-source/<source_id>/<ulid>` → empty secondary value             |
| Seam validity time | `seam-time/<week_bucket>/<ulid>` → empty secondary value             |
| Embedding          | `embed/<packed_coord>` → fixed-dimension float32 vector              |
| HNSW metadata      | `hnsw/meta`, `hnsw/entry` → graph configuration and entry point      |
| HNSW node          | `hnsw/node/<packed_coord>` → graph layers and neighbor lists         |
| Changefeed head    | `__meta/changelog-head` → latest durable outbox commit identifier    |
| Changefeed outbox  | `__meta/changelog-outbox/<commit>/<ordinal>/<key>` → bounded intent  |

Source identifiers and tags are length-prefixed UTF-8 byte strings. Time indexes use UTC week buckets derived from `Validity.ValidFrom`. Missing source, tag, or `ValidFrom` values do not create the corresponding secondary.

Cell and seam source/time/tag secondaries are maintained by their typed write methods. Raw `Tx.Put` does not maintain them.

## MVCC physical keys

Format v1 stores one physical value for each logical key and overwrites it in place.

When a new database is created with `Options.EnableMVCC`, format v2 appends a commit-sequence suffix to versioned cell, facet, edge, seam, and secondary keys. Multiple committed versions then coexist. Cell, facet, and seam point reads seek backward from the transaction's `read_seq` bound and stop at the newest visible version; they do not scan the logical key's full history.

Cells use a zero-length latest value as a tombstone. Deletion therefore adds a version rather than immediately removing physical history. Pruning can remove eligible non-latest versions; compaction rewrites the remaining physical keys into a new file.

The page allocator is extend-only. [`DB.StorageStats`](../../storage_stats.go) walks the current B+ tree and overflow chains to report page-rounded live bytes and whole unreachable pages that compaction can reclaim. It does not treat unused capacity inside a reachable page as dead, so compacted output may be smaller than `PrimaryBytes - ReclaimableBytes` after repacking low-fill pages.

Timeline keys map wall time to commit snapshots:

```text
__meta/commit-time/<wall_nanos>/<commit_seq>
```

Named snapshot metadata is stored under private `__meta/` keys. Snapshot and validity semantics are documented in [`TX.md`](./TX.md); retention and compaction procedures are in [`OPERATIONS.md`](./OPERATIONS.md).

## Embedding and HNSW storage

Embeddings are optional. `Options.EmbeddingDimension` and `Options.DistanceMetric` become immutable database settings once established. Each cell may have one float32 vector.

`PutEmbedding` maintains the persisted HNSW graph. `SearchByEmbedding` uses HNSW when available and falls back to a flat scan when required. `DeleteCell` also removes the cell's embedding and HNSW node.

The query and content-search APIs can use embedding similarity for seed selection, but spatial expansion after seed selection remains deterministic and does not require vectors.

## Logical changefeed

The consumer-facing changefeed is an append-only sidecar file, `{primary}-changelog`. When enabled, each database commit first stores bounded private intents under `__meta/changelog-outbox/`; those records are the recoverable source for projecting semantic mutation records to the sidecar. A head key supplies commit identifiers independently of sidecar delivery sequence. Acknowledged outbox entries are deleted without advancing MVCC `CommitSeq`.

The redo WAL and logical changefeed have separate purposes:

- the WAL contains page images required for crash recovery;
- the primary outbox contains unacknowledged logical intent needed for recovery;
- the changefeed sidecar contains ordered logical deliveries used by external consumers and rebuildable projections.

See [`CHANGEFEED.md`](./CHANGEFEED.md) for framing, cursors, reconciliation, and recovery.

## Supported query paths

Typed public operations provide the following storage paths:

- point reads and writes for cells, facets, edges, seams, and embeddings;
- ring and bounded-neighborhood enumeration;
- source, time, and tag secondary scans;
- seam lookup by cell participation, source, or time;
- MVCC snapshots by commit sequence, wall time, or named tag;
- lexical, structured, and embedding-assisted seed selection;
- context assembly, visibility filtering, graph traversal, and Voronoi partitioning;
- logical changefeed reads, health checks, snapshot diffs, pruning, and compaction.

The task-oriented public surface is described in [`API_REFERENCE.md`](./API_REFERENCE.md).

## Derived occupancy summaries

`SuperHexSummaryIndex` is a process-local projection, not a stored key family. It rebuilds from a consistent database snapshot and follows the logical changefeed to maintain aperture-7 occupancy counts. Replacing or compacting the source database requires rebuilding the index.

Persistent or content-bearing regional summaries are not part of the current storage contract. They remain an evidence-gated roadmap candidate.

## Storage boundaries

HexxlaDB intentionally does not provide:

- distributed replication or cross-node consistency;
- online key rotation or online compaction;
- automatic primary-file shrinking;
- a pluggable third-party storage backend;
- general-purpose graph or vector database semantics.

These boundaries keep the on-disk and public contracts focused on an embedded hex-native store. Deferred candidates are tracked only in [`ROADMAP.md`](../ROADMAP.md).
