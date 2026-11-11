# Transactions (`View` / `Update`)

**Audience:** Callers of **`package hexxladb`** using Bolt-style **[`DB.View`](../../tx.go)** and **[`DB.Update`](../../tx.go)**.

## Locking

- **`View`** acquires a **read lock**: many concurrent **`View`** calls can run; they block while an **`Update`** holds the **database** lock.
- **`Update`** acquires the **database** lock before beginning the engine write transaction and keeps it through the callback, synchronous engine commit, changelog projection, and durable-outbox acknowledgement. No concurrent **`View`** or **`Update`** can observe or build on staged engine state. A successful return publishes the complete commit, including its **`CommitSeq`** and changefeed records.

Each successful **`Update`** uses one engine **write transaction**; see **[`DURABILITY.md`](./DURABILITY.md)** for barrier ordering.

[`DB.WriteStats`](../../write_stats.go) returns lock-free cumulative timings for accepted public write calls. `LockWait` measures time waiting to acquire the database lock; `Callback`, `Durability`, and `Finalization` measure work while that lock remains held. Read two samples to calculate interval counts and averages without installing an in-library metrics system.

## Snapshot semantics and MVCC

`View` pins the latest committed sequence for the callback. Historical views
select an explicit sequence, timestamp, or snapshot tag. See
[MVCC temporal semantics](#mvcc-temporal-semantics) for visibility and timeline
rules.

### Retained snapshots

[`OldestSnapshotSeq`](../../mvcc_snapshot_floor.go) is the earliest guaranteed
MVCC sequence. `ViewAt`, `ViewAtTime`, and `ViewAtTag` return
`ErrSnapshotExpired` below it before calling the callback. Pruning preserves
the cell version or tombstone visible at the cutoff. The first deletion batch
records the new boundary in the same engine transaction as its deletions.
If that transaction needs recovery, historical reads return
`ErrCommitFinalization` until the database is reopened.
A snapshot tag names a sequence and expires when the retention boundary
passes it.

An existing MVCC database without retention metadata uses its current head
as the baseline at upgrade. The first versioned write or effective prune
persists the boundary. Older binaries cannot maintain it. Use a pre-upgrade
backup for a downgrade that needs to write or prune.

## `Close`

**[`DB.Close`](../../db.go)** takes the **exclusive** lock and waits for any in-flight **`View`** or **`Update`** to finish before closing files. New **`View`** / **`Update`** after a successful close return **[`ErrDatabaseClosed`](../../errors.go)**.

## Nesting and reentrancy

Pass the callback's `Tx` to helper functions. Opening another transaction on
the same `DB` from inside a callback can deadlock, including a nested `View`
when a writer is waiting.

## Commit finalization failures

`Update` uses an authoritative commit followed by a recoverable projection:

1. Run the callback. Sample the MVCC timeline timestamp at transaction start.
2. After a successful callback, stage the MVCC timeline row, bounded changefeed intents under private `__meta/changelog-outbox/` keys, and the new header `CommitSeq`.
3. Commit data pages, the outbox, its head, and `CommitSeq` through one engine/WAL boundary. On authenticated v3, page releases and freelist metadata are part of this same boundary; an abort cannot publish reusable ids from an uncommitted tree mutation.
4. Project those intents to the external changelog. Once its frames are durable, remove the acknowledged outbox keys in an internal cleanup transaction that does not advance `CommitSeq` or emit events.

If the callback or a pre-commit stage fails, the engine transaction aborts.
An embedding index maintenance error after staging a vector also aborts the
transaction, even when the callback ignores the returned error.
A transaction that exceeds the WAL size limit returns `ErrInvalidArgument`
before writing the WAL. After an engine commit failure, storage reads also reject the
handle with `ErrCommitFinalization`, preventing exposure of partially
published primary pages.

If the engine commit is known durable but changefeed projection or cleanup fails,
the error matches both `ErrCommitFinalization` and [`ErrCommitDurable`](../../errors.go).
Further writes on that handle are rejected. Follow the
[finalization recovery runbook](./CHANGEFEED.md#finalization-recovery-runbook)
to distinguish known-durable and uncertain outcomes and recover the handle.

## Byte keys

Raw reads expose physical versions. Raw writes are available for
application-owned keys such as `app/`. See [advanced byte-key access](./API_REFERENCE.md#advanced-byte-key-access)
and [storage key families](./HEXXLA_DB.md#logical-key-families).

## Validity filters and facet ring loads

These read filters apply record validity to cells and seams visible in the transaction snapshot. The caller can choose the snapshot and the validity time independently.

- **[`ValidityWire`](../../export.go)** — half-open validity window **`[ValidFrom, ValidTo)`** in Unix nanoseconds UTC (`nil` bound = open on that side), applied by validity-aware typed reads.
- **[`Tx.WalkRingAt`](../../spatial_reads.go)** — same ring order as **`WalkRing`**, but invokes the callback only for cells whose **`Validity`** contains **`asOf`** (missing or out-of-window cells are skipped).
- **[`Tx.LoadContext`](../../context_load.go)** with **`LoadContextConfig.AsOf`** — assembles only cells whose validity contains **`asOf`**; `MaxCells` remains the upper bound on returned cells.
- **[`Tx.FindSeamsAt`](../../seams.go)** — like **`FindSeams`**, but only includes seams whose **[`SeamRecord.Validity`](../../export.go)** contains **`asOf`**. Seams stored without a validity suffix decode as an open window (always included when **`asOf`** is used).
- **[`Tx.WalkRingFacets`](../../spatial_reads.go)** — for each ring coordinate with an existing cell (and optional **`asOf`** filter on the cell’s validity), loads facet records for **`facet_id`** bits **`0..5`** set in the 6-bit **`facetMask`** (bits outside **`0x3f`** → **`ErrInvalidArgument`**). Typical cost **O(ring_cells × popcount(mask))** btree **`GetFacet`** operations; facets are returned in ascending **`facet_id`** order (missing keys omitted).

## MVCC temporal semantics

For MVCC formats v2 and v3 the authoritative visibility clock is **`CommitSeq`** in the engine header (see [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)). Each successful [`Update`](../../tx.go) advances `CommitSeq` atomically with its engine transaction; a reopened database therefore cannot expose versioned pages under an older reused sequence.

- **[`DB.ViewAt(readSeq)`](../../tx.go)** pins `read_seq` for the callback. Cell, facet, and seam point reads use the version suffix's byte ordering to seek directly to the largest stored `commit_seq <= read_seq`, then stop after one valid version. [`ErrReadSeqFuture`](../../errors.go) is returned if `read_seq` exceeds the header's `CommitSeq`.
- **[`DB.ViewAtTime(asOf)`](../../tx.go)** maps UTC wall time to a `read_seq` using the commit timeline: during each MVCC `Update`, an `__meta/commit-time/` btree key records `(wall_unix_nano, writeSeq)` (wall timestamp sampled at transaction start). The resolver seeks to the latest signed timestamp at or before `asOf`, breaking ties by commit sequence. If no commit qualifies, it resolves to sequence zero subject to the retention boundary. Determinism requires stable UTC clock usage; the same `asOf` yields the same snapshot for a given database history.
- **Secondary indexes (`tag/`, `source/`, `time/`, seam secondaries):** version-suffixed entries are retained alongside committed primary versions. Readers deduplicate by logical ID and check that the snapshot-visible record matches the requested tag, source, or week. Deleting a cell preserves the previous committed index entries until its old version is pruned. For databases written by an affected release, see [repairing MVCC secondary indexes](./OPERATIONS.md#repairing-mvcc-secondary-indexes).
- **Current-only families:** Edges and embeddings are stored at head. Typed
  graph and vector operations return `ErrHistoricalDataUnavailable` when the
  requested sequence precedes head, including embedding queries and context
  graph expansion or edge enrichment. Use a separate `DB.View` when you
  explicitly want current graph or vector data. Separate views can observe
  different commits.
- **Validity vs snapshot:** [`WalkRingAt`](../../spatial_reads.go), [`FindSeamsAt`](../../seams.go), and [`LoadContextConfig.AsOf`](../../context_load.go) filter by record validity windows — orthogonal to `read_seq` (snapshot time vs. domain validity interval).

The [storage model](./HEXXLA_DB.md#mvcc-physical-keys) owns the physical
layout. Replacing a staged version removes that version's old secondary
entries. Prior committed versions keep their entries until pruning.

## Logical changefeed

See the [changefeed contract](./CHANGEFEED.md) for opt-in configuration,
durable cursors, and consumer recovery.

## Encryption

Official at-rest encryption is configured via **[`Options`](../../options.go)** on **[`Open`](../../db.go)** (`EncryptionKey` or `Passphrase`). Transactions see plaintext; new encrypted databases use authenticated engine format v3. Threat model, WAL publication, migration, and residual rollback limits are documented in **[`ENCRYPTION.md`](./ENCRYPTION.md)**.
