# HexxlaDB storage model and key layout

This document defines the logical records and key families implemented by HexxlaDB. Page and WAL bytes are specified separately in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md); product memory concepts are defined in [`HEXXLA.md`](./HEXXLA.md).

HexxlaDB is an embedded, hex-native database implemented by this module. The stable public package is `github.com/hexxla/hexxladb`; private storage components live under `internal/`.

## Storage design

The engine uses one B+ tree over byte-ordered key families, backed by fixed-size pages and a redo WAL. Coordinates are Morton-packed so spatial addresses have a stable ordered representation. Lattice operations, records, and secondary indexes are part of the database implementation rather than adapters over a third-party SQL or key-value engine.

`PageSize` is the logical B+ tree page capacity. Plaintext v2 files use the logical page size as their physical stride. Authenticated encrypted v3 adds a
48-byte generation/nonce/tag envelope to every data page while leaving page 0
at one logical page. The exact byte layout belongs to
[`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).

The [architecture guide](../architecture/HEXAGONAL_ARCHITECTURE.md) owns
package boundaries and dependency direction.

## Logical records

### Cell

A cell is addressed by `PackedCoord` and stores raw content, provenance, validity, tags, and an optional cluster hint. `PutCell` maintains the applicable source, time, tag, and MVCC data associated with the logical cell. Embeddings are separate records: callers that change content must call `PutEmbedding` explicitly, preferably in the same transaction, or deliberately retain/delete the existing vector.

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
| HNSW lifecycle     | `hnsw/state` → revision and reachability-validated graph state        |
| Changefeed head    | `__meta/changelog-head` → latest durable outbox commit identifier    |
| Changefeed outbox  | `__meta/changelog-outbox/<commit>/<ordinal>/<key>` → bounded intent  |

Source identifiers and tags are length-prefixed byte strings. The cell source
index caps its identifier at [220 bytes](../../internal/index/source_key.go).
Time indexes derive the UTC week bucket from `Validity.ValidFrom` using
[division by seven days rounded down](../../internal/index/time_key.go). Missing
source, tag, or `ValidFrom` values leave the corresponding secondary absent.

Cell and seam source/time/tag secondaries are maintained by their typed write methods. Raw `Tx.Put` does not maintain them.

Typed record writes validate the physical identity before encoding: packed cell,
facet, edge, seam, embedding, and cluster-hint coordinates must be valid packing
outputs, and stored floating-point metadata must be finite. This prevents keys
that cannot be enumerated spatially and values that would make ranking or graph
ordering non-total. Raw application key/value rows remain application-owned.

## MVCC physical keys

Plaintext v2 and authenticated encrypted v3 use the same MVCC physical-key rules. Multiple committed
versions then coexist. Cell, facet, and seam point reads seek backward from the
transaction's `read_seq` bound and stop at the newest visible version; they do
not scan the logical key's full history.

Cells use a zero-length latest value as a tombstone. Deletion therefore adds a version rather than immediately removing physical history. Pruning can remove eligible non-latest versions; compaction rewrites the remaining physical keys into a new file.

Authenticated format v3 persists reusable B+ tree and overflow-page ids in the
authenticated transaction boundary and consumes them before extending the
primary. [`DB.ReclaimTail`](../../reclaim.go) can truncate only a contiguous
allocator-owned suffix. Plaintext v2 remains extend-only.
[`DB.StorageStats`](../../storage_stats.go) walks the current B+ tree, overflow
chains, and allocator metadata to report page-rounded live, reusable, and whole
unreachable bytes. It does not treat unused capacity inside a reachable page as
dead, so compacted output may be smaller than
`PrimaryBytes - ReclaimableBytes` after repacking low-fill pages.

Timeline keys map wall time to commit snapshots:

```text
__meta/commit-time/<wall_nanos>/<commit_seq>
```

Named snapshot metadata is stored under private `__meta/` keys. The
`__meta/mvcc-snapshot-floor` key stores the earliest guaranteed MVCC
sequence as a big-endian uint64. Prune commits publish it with the affected
primary and secondary rows. Copy maintenance preserves the metadata.
Snapshot and validity semantics are documented in [`TX.md`](./TX.md); retention and compaction procedures are in [`OPERATIONS.md`](./OPERATIONS.md).

## Embedding and HNSW storage

Embeddings are optional. `Options.EmbeddingDimension` and `Options.DistanceMetric` become immutable database settings once established. Each cell may have one float32 vector.

`PutEmbedding` maintains a current HNSW graph. Node levels are derived
deterministically from the packed coordinate. `SearchByEmbedding` falls back
to an exact flat scan when the graph is absent, stale, or returns fewer
vectors than requested and available. Malformed graph or vector data
encountered during the search returns `ErrCorruptDatabase`.
`SearchByEmbeddingWithStats` exposes the selected path and effective query
breadth. `DeleteCell` removes the authoritative vector and repairs the graph
when it is current. Detected graph disconnection marks the graph stale.

`PutEmbeddingWithOptions` can defer graph maintenance during bounded bulk
ingestion. The vector under `embed/` remains authoritative and the lifecycle
state becomes dirty, so queries use exact flat search rather than a stale
graph. `RebuildEmbeddingIndex` advances the persisted revision, snapshots the
authoritative vectors, builds a bounded in-memory graph, checks layer-zero
reachability from its entry point, then
replaces the graph keys and marks that same revision current in one database
commit. Every embedding mutation during a dirty interval advances the revision;
an intervening write therefore prevents publication. Cancellation and failed
publication leave the prior graph records intact but inactive, with flat search
available until a successful retry. This is a logical-key addition, not an
engine-format or page-layout change, and backup, compaction, and migration
preserve it as ordinary primary data.

The measured deferred-rebuild envelope uses 4 KiB B+ tree pages for 20,000 vectors at 32 dimensions and 10,000 at 384 dimensions. HNSW graph maintenance performs many small random reads and rewrites, so increasing the page size can amplify copying, dirty-page retention, file growth, and transaction memory even though each graph record is small. Rebuild preflight bounds vectors, estimated memory/transient WAL, and available filesystem capacity. The vector-scale evidence runner and reference measurements are documented in [`PERFORMANCE_EVIDENCE.md`](./PERFORMANCE_EVIDENCE.md).

The query and content-search APIs can use embedding similarity for seed selection, but spatial expansion after seed selection remains deterministic and does not require vectors.

Retired single-version and AES-XTS files require [offline conversion](./OPERATIONS.md#offline-conversion-of-retired-files).

## Logical changefeed

The consumer-facing changefeed is an append-only sidecar file, `{primary}-changelog`. When enabled, each database commit first stores bounded private intents under `__meta/changelog-outbox/`; those records are the recoverable source for projecting semantic mutation records to the sidecar. A head key supplies commit identifiers independently of sidecar delivery sequence. Acknowledged outbox entries are deleted without advancing MVCC `CommitSeq`.

The redo WAL and logical changefeed have separate purposes:

- the WAL contains page images required for crash recovery;
- the primary outbox contains unacknowledged logical intent needed for recovery;
- the changefeed sidecar contains ordered logical deliveries used by external consumers and rebuildable projections.

See [`CHANGEFEED.md`](./CHANGEFEED.md) for framing, cursors, reconciliation, and recovery.

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
