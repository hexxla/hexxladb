# HexxlaDB architecture

HexxlaDB is an embedded Go library. Consumers import `github.com/hexxla/hexxladb` and own their application policy and transports.

## Package boundaries

| Path | Ownership |
| --- | --- |
| Module root | Stable API and transaction coordination across the engine, logical records, indexes and changefeed |
| `internal/engine` | Pages, B+ tree, locking, WAL and durable publication |
| `internal/index`, `internal/record` | Physical key and record encoding |
| `internal/lattice`, `internal/views`, `internal/hnsw`, `internal/pathfind` | Private geometry, view assembly and retrieval algorithms |
| `internal/inspect` | Typed operator diagnostics over the public transaction API and private diagnostic encodings |
| `cmd/hexxladb`, `cmd/tui` | Operator commands, process lifecycle and presentation |
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

Root files group cell writes, seam lifecycle, spatial reads, and migration
stages by responsibility. They share the existing transaction boundary.

Public API compatibility follows [VERSIONING.md](../../VERSIONING.md). Use root aliases deliberately for shared data types. Preserve transaction ordering, changefeed and recovery guarantees when changing a storage boundary.

## Verification

Run the affected package tests, then `task ci`. CI checks dependency boundaries with `depguard` and verifies the public API baseline. Run `task integration` for storage and crash recovery changes. Named crash barriers block only in integration builds.

See [API reference](../hexxladb/API_REFERENCE.md), [transaction model](../hexxladb/TX.md), and [durability](../hexxladb/DURABILITY.md).
