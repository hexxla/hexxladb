# Logical changefeed

**Audience:** Operators and **automated consumers** (indexers, agents, orchestrators) that need a **durable, ordered** stream of **semantic** mutations—not page-image WAL records.

**Operations context:** [OPERATIONS.md](./OPERATIONS.md) covers backups, retention, and incident response for changefeed consumers.

## North star

The changefeed is **grounded external memory** for what changed in the store: each entry is sufficient to **invalidate caches**, **rebuild derived views**, or **audit** without scanning the full B+ tree. **Cursor-based tailing** supports **resumable** workers; delivery is **at-least-once** (see below).

## What it is not

- **Not** a decode of the redo WAL ([`internal/engine/wal.go`](../../internal/engine/wal.go)); WAL stays page-image for recovery.
- **Not** exactly-once end-to-end (requires idempotent consumers or distributed dedup).

## Files

- Default path: **`<database-path>-changelog`** (same directory as the main DB file, sibling append-only file).
- **Format v1:** plaintext binary framing for a plaintext database.
- **Format v2:** automatically used with [`Options.EncryptionKey`](../../options.go) or [`Options.Passphrase`](../../options.go). Logical frame contents use XChaCha20-Poly1305 with database- and log-specific derived keys; frame sequence and length remain visible.

Formats are never mixed in one file. An encrypted database rejects a legacy format-v1 changelog with [`ErrChangelogPlaintext`](../../errors.go). Follow the reconciliation/archive procedure in [`ENCRYPTION.md`](./ENCRYPTION.md#changelog-policy), or use offline encryption rotation to convert a plaintext database and changelog together.

## Delivery semantics

- **Recoverable projection:** before the engine commit, each logical mutation is stored as a bounded intent in the primary database under `__meta/changelog-outbox/`. `Open` projects any remaining intents before returning a changelog-enabled handle, so a committed mutation cannot become a permanent sidecar gap.
- **At-least-once:** a crash after the sidecar append but before primary outbox acknowledgement can append the same operation again during recovery. Handlers **must** be **idempotent**: apply puts as upserts, deletes as idempotent deletes, and use the primary key plus content hash/authoritative state when suppressing equivalent work. The sidecar `Seq` identifies delivery order, not a globally exactly-once mutation identity.
- **Cursor:** monotonic **`uint64` sequence** per record. **`ReadChangelogSince(afterSeq, limit)`** returns records with **`seq > afterSeq`**, up to **`limit`** entries. Use **`0`** to read from the first record.

## Payload policy

- **Small payloads (≤ 4096 bytes)** of the canonical encoded record may be **inlined** alongside **SHA-256** of that encoding.
- **Larger** values: **hash + encoded length** only (keeps the log a **context index**, not a full copy of the database).

## Durability modes ([`Options`](../../options.go))

- **`ChangelogSync` (default when enabled):** append is followed by **`Sync`** on the changelog file, then the acknowledged primary outbox entries are removed.
- **`ChangelogLazy`:** append does not fsync each commit. Primary outbox intents remain durable until the sidecar is synced and acknowledged after 256 pending intents or during a clean `Close`. A crash may therefore cause duplicate delivery after reopen, but the primary intents prevent permanent tail loss.

The default path therefore performs a second small engine transaction to remove acknowledged outbox keys. This cleanup does not advance MVCC `CommitSeq` or emit another event. [`DB.WriteStats`](../../write_stats.go) **`Finalization`** includes sidecar projection and acknowledged-outbox cleanup time, while **`Durability`** covers the authoritative primary commit.

## Finalization recovery runbook

When an `Update`/`Batch` returns **[`ErrCommitFinalization`](../../errors.go)**:

1. Stop issuing writes on that handle and retain the original error cause.
2. If the error also matches **[`ErrCommitDurable`](../../errors.go)**, the primary commit is known durable: do **not** retry the mutation.
3. Otherwise the engine outcome is uncertain: close, reopen, then inspect authoritative state before deciding whether an application-level idempotent retry is needed.
4. Reopen with the same changelog path and encryption credentials. `Open` projects durable outbox intents and removes them only after the sidecar is synced.
5. Resume the consumer from its last durable cursor. Accept equivalent redelivery and apply it idempotently.

If reopen returns `ErrCommitFinalization`, primary intent remains preserved but the sidecar could not be repaired or written. Resolve the reported filesystem error and retry `Open`; do not delete the primary outbox keys manually.

**Retries:** backoff and retry **`ReadChangelogSince`** on transient I/O errors; treat **`ErrChangelogCorrupt`** as terminal for that tail (see **Recovery** below)—do not spin tight loops.

**Lag:** combine **`changelog_records_lag`** (table above) with application-level alerting when consumers fall behind **`CommitSeq`** expectations.

## Recovery

- On open, the implementation scans the log to determine the **next sequence number**, validate frames, and build sparse in-memory sequence-to-offset checkpoints. The checkpoints are not persisted and do not change the file format.
- When the primary contains durable outbox intents, open may truncate an **incomplete final frame** to the last validated boundary and reproject the intents. Without authoritative pending intent, the same truncation remains a terminal corruption error.
- Full frames with invalid CRC/authentication, non-contiguous sequence numbers, or invalid primary outbox records fail closed. Recovery never silently discards an authenticated or structurally complete frame.
- `ReadChangelogSince` seeks to the nearest checkpoint and scans forward. At most 255 records before the requested cursor are decoded, rather than the entire history.
- Without matching durable primary intent, a corrupt/truncated format-v1 tail or authentication failure in a format-v2 frame returns **[`ErrChangelogCorrupt`](../../errors.go)**. Wrong database/key binding fails during open with [`ErrChangelogEncryptionKeyMismatch`](../../errors.go).

## Operations emitted

One event per successful **mutation** on [`Tx`](../../tx.go) / primitives. Stable op codes are [`ChangelogOp*` constants](../../db_changelog.go): **PutCell** (`OpPutCell`), seam insert/update via **PutSeam** (`OpPutSeam`), **ResolveSeam** (`OpResolveSeam` — same encoded seam payload and MVCC keys as **PutSeam**, distinct op for downstream workflows), **PutFacet** / **UpdateFacet** (`OpPutFacet`), **PutEdge** / **LinkCells** (`OpPutEdge`). **MarkConflict** is recorded as **PutSeam** (`OpPutSeam`) with seam payload distinguishing `mark_conflict`. **Read-only** [`View`](../../db.go) emits nothing.

## Observability (recommended metrics)

There is **no** in-process metrics registry in HexxlaDB itself; exporters should instrument the embedding service.

| Signal                             | Meaning                                                                                                                                                                                        |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `changelog_records_lag`            | Difference between latest applied DB `CommitSeq` (or app-level mutation counter) and last fully processed changelog sequence (your consumer cursor).                                           |
| `changelog_read_errors_total`      | Count of [`ErrChangelogCorrupt`](../../errors.go) or I/O failures from [`ReadChangelogSince`](../../db_changelog.go).                                                                          |
| `commit_finalization_errors_total` | [`ErrCommitFinalization`](../../errors.go) from [`Update`](../../tx.go); split known-durable errors with [`ErrCommitDurable`](../../errors.go) and alert until close/reopen recovery succeeds. |
| `changelog_append_latency_ms`      | Time spent in changelog append path per commit (detect fsync stalls when sync mode is on).                                                                                                     |

**Dashboards:** plot lag over time; alert when lag grows unbounded or corrupt-tail errors spike.

## Related

- [TX.md](./TX.md) — transaction boundaries (`Update` / `Batch`).
