# Logical changefeed (Phase G)

**Audience:** Operators and **automated consumers** (indexers, agents, orchestrators) that need a **durable, ordered** stream of **semantic** mutations—not page-image WAL records.

**Readiness context:** [HEXXLA_READINESS_ROADMAP.md](./HEXXLA_READINESS_ROADMAP.md) (Remaining gaps and roadmap).

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

## Recovery

- On open, the implementation scans the log to determine the **next sequence number** and validate frames.
- Corrupt tail: returns **[`ErrChangelogCorrupt`](../../errors.go)** (or partial repair policy as implemented).

## Operations emitted

One event per successful **mutation** on [`Tx`](../../tx.go) / primitives: **PutCell**, **PutSeam**, **ResolveSeam**, **PutFacet**, **UpdateFacet**, **PutEdge**, **LinkCells**. **MarkConflict** is recorded as **PutSeam** (same encoded seam path) with seam payload distinguishing `mark_conflict`. **Read-only** [`View`](../../db.go) emits nothing.

## Related

- [TX.md](./TX.md) — transaction boundaries (`Update` / `Batch`).
- [MODERN_GO.md](../context/MODERN_GO.md) — Go version and stdlib reference.
