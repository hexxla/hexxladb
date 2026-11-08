# HexxlaDB architecture

HexxlaDB is an embedded Go library. Consumers import `github.com/hexxla/hexxladb` and own their application policy and transports.

## Package boundaries

| Path | Ownership |
| --- | --- |
| Module root | Stable API and transaction coordination across the engine, logical records, indexes and changefeed |
| `internal/engine` | Pages, B+ tree, locking, WAL and durable publication |
| `internal/index`, `internal/record` | Physical key and record encoding |
| `internal/lattice`, `internal/views`, `internal/hnsw`, `internal/pathfind`, `internal/mvcc` | Private geometry, retrieval and versioning algorithms |
| `internal/inspect` | Typed, bounded operator diagnostics over the public transaction API and private encodings |
| `cmd/hexxladb`, `cmd/tui` | Operator commands, process lifecycle and presentation |
| `examples/ports_adapters` | Reference application with application ports, an outbound adapter and contract tests. Its storage types use the public HexxlaDB types. |

## Dependency direction

An embedding application drives the public API. The public package coordinates the engine and private algorithms. The engine owns physical commit ordering. The operator commands use the public package or the typed diagnostic boundary for storage inspection. Keep physical key scanning and decoding in the diagnostic adapter.

In the reference application, use cases depend on domain-owned ports and the outbound adapter implements them using the public HexxlaDB API. Production transports belong to their embedding applications. The reference app and diagnostic package are outside the library's dependency graph.

Public API compatibility follows [VERSIONING.md](../../VERSIONING.md). Use root aliases deliberately for shared data types. Preserve transaction ordering, changefeed and recovery guarantees when changing a storage boundary.

## Verification

Run the affected package tests, then `task ci`. CI checks dependency boundaries with `depguard` and verifies the public API baseline. Run `task integration` for storage and crash recovery changes. Named crash barriers block only in integration builds.

See [API reference](../hexxladb/API_REFERENCE.md), [transaction model](../hexxladb/TX.md), and [durability](../hexxladb/DURABILITY.md).
