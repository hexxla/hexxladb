# HexxlaDB architecture

HexxlaDB is an embedded Go library. Consumers import `github.com/hexxla/hexxladb` and own their application policy and transports.

## Package boundaries

| Path | Ownership |
| --- | --- |
| Module root | Stable API and transaction coordination across the engine, logical records, indexes and changefeed |
| `internal/engine` | Locking, durable publication, [page and WAL formats](../../internal/engine/ENGINE_FORMAT.md), and [B+ tree layout](../../internal/engine/ORDERED_STORE.md) |
| `internal/index`, `internal/record` | [Physical key families](../hexxladb/HEXXLA_DB.md#logical-key-families) and [binary record encoding](../../internal/record/FORMAT.md) |
| `internal/lattice`, `internal/views`, `internal/hnsw`, `internal/pathfind` | Private geometry, view assembly and retrieval algorithms |
| `internal/inspect` | Typed operator diagnostics over the public transaction API and private diagnostic encodings |
| `cmd/hexxladb`, `cmd/tui` | Operator commands, process lifecycle and presentation |
| `internal/cmdconfig` | Credential handling shared by operator commands |
| `tools/evidence`, `tools/seed_tui` | Opt-in workload runners and synthetic explorer fixture generation |
| `examples/ports_adapters` | Runnable contradiction workflow with application-owned values, a two-operation port and a public-API-backed adapter |

## Dependency direction

An embedding application drives the public API. The root package owns logical
snapshot semantics, including the seam scan shared by health checks and
operator views. `internal/index` owns physical key encoding, including MVCC
version keys. The engine owns physical commit ordering. HNSW receives its
storage port and scorer from its caller. Operator commands use the public API
and typed diagnostics, with private key decoding confined to the diagnostic
adapter where it is still needed.

In the reference application, the service validates the contradiction, the
domain port describes one atomic record operation, and the outbound adapter
writes both observations and the seam in one database transaction. The example
entry point owns wiring and database lifetime. Production transports belong
to their embedding applications. The reference app and diagnostic package are
outside the library's dependency graph.

Public API compatibility follows [VERSIONING.md](../../VERSIONING.md). Use root aliases deliberately for shared data types. Preserve transaction ordering, changefeed and recovery guarantees when changing a storage boundary.

## Root file map

Start with these files when tracing a public operation. Related files share the root transaction boundary.

| Workflow | Entry points and coordination |
| --- | --- |
| Database and transactions | [`db.go`](../../db.go) owns the database handle and close path. `db_open.go` opens and recovers the file set. [`tx.go`](../../tx.go) runs reads and writes, [`batch.go`](../../batch.go) batches writes, and [`changelog_outbox.go`](../../changelog_outbox.go) coordinates changefeed delivery. |
| Records and secondary indexes | [`cells.go`](../../cells.go), [`delete_cell.go`](../../delete_cell.go), and [`seams.go`](../../seams.go) handle record lifecycles. [`cell_secondary.go`](../../cell_secondary.go) and [`seam_secondary.go`](../../seam_secondary.go) maintain lookup keys. |
| Snapshots and retention | [`mvcc.go`](../../mvcc.go) resolves versioned records. [`mvcc_lifecycle.go`](../../mvcc_lifecycle.go) prunes history, [`mvcc_snapshot_floor.go`](../../mvcc_snapshot_floor.go) tracks retention, and [`snapshot_tags.go`](../../snapshot_tags.go) names snapshots. |
| Retrieval and traversal | [`query.go`](../../query.go) defines queries, [`query_exec.go`](../../query_exec.go) selects and runs scans, and [`query_rank.go`](../../query_rank.go) retains and ranks results. [`context_load.go`](../../context_load.go), [`spatial_reads.go`](../../spatial_reads.go), and [`pathfind_api.go`](../../pathfind_api.go) drive context, spatial scans, and pathfinding. |
| Embeddings and HNSW | [`tx_embedding.go`](../../tx_embedding.go) writes vectors and [`embedding_search.go`](../../embedding_search.go) handles lookup. [`embedding_index_rebuild.go`](../../embedding_index_rebuild.go) takes the vector snapshot and publishes the graph built by [`internal/hnsw`](../../internal/hnsw/build.go). |
| Maintenance | [`backup.go`](../../backup.go), [`compact.go`](../../compact.go), and [`rotation.go`](../../rotation.go) coordinate durable copies. [`health.go`](../../health.go) checks logical records and indexes. |

## Verification

Run the affected package tests, then `task ci`. CI checks dependency boundaries with `depguard` and verifies the public API baseline. Run `task integration` for storage and crash recovery changes. Named crash barriers block only in integration builds.

See [API reference](../hexxladb/API_REFERENCE.md), [transaction model](../hexxladb/TX.md), and [durability](../hexxladb/DURABILITY.md).
