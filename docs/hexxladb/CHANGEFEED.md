# Logical changefeed (Phase G)

**Audience:** Operators and **automated consumers** (indexers, agents, orchestrators) that need a **durable, ordered** stream of **semantic** mutations—not page-image WAL records.

**Operations context:** [OPERATIONS.md](./OPERATIONS.md) covers backups, retention, and incident response for changefeed consumers.

## North star

The changefeed is **grounded external memory** for what changed in the store: each entry is sufficient to **invalidate caches**, **rebuild derived views**, or **audit** without scanning the full B+ tree. **Cursor-based tailing** supports **resumable** workers; delivery is **at-least-once** (see below).

## What it is not

- **Not** a decode of the redo WAL ([`internal/engine/wal.go`](../../internal/engine/wal.go)); WAL stays page-image for recovery.
- **Not** exactly-once end-to-end (requires idempotent consumers or distributed dedup).

## Files

- Default path: **`<database-path>-changelog`** (same directory as the main DB file, sibling append-only file).
- Format version **1** binary framing (see package [`internal/changelog`](../../internal/changelog)).

## Delivery semantics

- **At-least-once:** a consumer may see the same logical change more than once after crashes or retries. Handlers **must** be **idempotent** (e.g. key on `(log_seq, op, primary_key)` or content hash).
- **Cursor:** monotonic **`uint64` sequence** per record. **`ReadChangelogSince(afterSeq, limit)`** returns records with **`seq > afterSeq`**, up to **`limit`** entries. Use **`0`** to read from the first record.

## Payload policy

- **Small payloads (≤ 4096 bytes)** of the canonical encoded record may be **inlined** alongside **SHA-256** of that encoding.
- **Larger** values: **hash + encoded length** only (keeps the log a **context index**, not a full copy of the database).

## Durability modes ([`Options`](../../options.go))

- **`ChangelogSync` (default when enabled):** append is followed by **`Sync`** on the changelog file (stronger alignment with committed state; slower).
- **`ChangelogLazy`:** buffered append; **shorter fsync** latency; **possible loss of tail records** on crash if the OS has not flushed. Use only when the product accepts that tradeoff.

If changelog append fails **after** the btree write succeeded, the API may still return an error; the database may contain writes **without** a corresponding log entry (documented limitation for this milestone—operators should monitor and re-reconcile if needed).

## Reconciliation runbook (commit succeeded, log append failed)

When an `Update`/`Batch` returns **[`ErrCommitFinalization`](../../errors.go)** with a changelog append cause:

1. Treat the write as **possibly committed** to primary storage.
2. Re-read authoritative state from the DB (not from changelog tail) for affected keyspace.
3. Rebuild downstream derived projections idempotently (upsert/merge by primary key + latest sequence).
4. Resume changelog consumption from the last known good cursor after reconciliation.

This keeps downstream memory/context indexes consistent even when logical log durability lags data durability.

**Retries:** backoff and retry **`ReadChangelogSince`** on transient I/O errors; treat **`ErrChangelogCorrupt`** as terminal for that tail (see **Recovery** below)—do not spin tight loops.

**Lag:** combine **`changelog_records_lag`** (table above) with application-level alerting when consumers fall behind **`CommitSeq`** expectations.

## Recovery

- On open, the implementation scans the log to determine the **next sequence number**, validate frames, and build sparse in-memory sequence-to-offset checkpoints. The checkpoints are not persisted and do not change the file format.
- `ReadChangelogSince` seeks to the nearest checkpoint and scans forward. At most 255 records before the requested cursor are decoded, rather than the entire history.
- Corrupt tail: returns **[`ErrChangelogCorrupt`](../../errors.go)** (or partial repair policy as implemented).

## Operations emitted

One event per successful **mutation** on [`Tx`](../../tx.go) / primitives. Stable op codes are [`ChangelogOp*` constants](../../db_changelog.go): **PutCell** (`OpPutCell`), seam insert/update via **PutSeam** (`OpPutSeam`), **ResolveSeam** (`OpResolveSeam` — same encoded seam payload and MVCC keys as **PutSeam**, distinct op for downstream workflows), **PutFacet** / **UpdateFacet** (`OpPutFacet`), **PutEdge** / **LinkCells** (`OpPutEdge`). **MarkConflict** is recorded as **PutSeam** (`OpPutSeam`) with seam payload distinguishing `mark_conflict`. **Read-only** [`View`](../../db.go) emits nothing.

## Observability (recommended metrics)

There is **no** in-process metrics registry in HexxlaDB itself; exporters should instrument the embedding service.

| Signal | Meaning |
|--------|---------|
| `changelog_records_lag` | Difference between latest applied DB `CommitSeq` (or app-level mutation counter) and last fully processed changelog sequence (your consumer cursor). |
| `changelog_read_errors_total` | Count of [`ErrChangelogCorrupt`](../../errors.go) or I/O failures from [`ReadChangelogSince`](../../db_changelog.go). |
| `commit_finalization_errors_total` | [`ErrCommitFinalization`](../../errors.go) from [`Update`](../../tx.go) (possible **data committed without changelog row**—see reconciliation runbook above). |
| `changelog_append_latency_ms` | Time spent in changelog append path per commit (detect fsync stalls when sync mode is on). |

**Dashboards:** plot lag over time; alert when lag grows unbounded or corrupt-tail errors spike.

## Related

- [TX.md](./TX.md) — transaction boundaries (`Update` / `Batch`).
