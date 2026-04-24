# Roadmap

## Quick Wins

Low effort, high value. No design required.

- Add `testing.B.Loop()` benchmarks for context packing time
- Expose MVCC memory pressure metric (`DB.StatsMVCC` enhancement)

## Near-term

Requires design + benchmarks before implementation.

- Batch MVCC prune (`PruneCellVersions`) — coalesce deletes under single engine write txn to reduce WAL pressure ([`DURABILITY.md`](./hexxladb/DURABILITY.md))

## Future

Spec exists; implementation deferred.

- `embed/` keyspace for ANN/hybrid retrieval ([`HEXXLA_DB.md`](./hexxladb/HEXXLA_DB.md))
- Materialized views / super-hex aggregation as engine algorithms
- Materialized changefeed consumers with automated prune policy

## Out of Scope

Intentional boundaries for embedded library v1.

- Distributed replication / HA — product-tier orchestration
- Freelist / primary file shrink — extend-only allocator by design ([`OPERATIONS.md`](./hexxladb/OPERATIONS.md))
- Online re-encryption — offline rotation only ([`VERSIONING.md`](../VERSIONING.md))
- Third-party KV backends (SQLite, etc.) — Hex-native engine is the direction

---

## Audit Log

| Date       | Scope                                         |
| ---------- | --------------------------------------------- |
| 2026-04-24 | v0.1.0 initial release                        |
| 2026-04-24 | Roadmap consolidated to priority-based format |
