# Transactions (`View` / `Update` / `Batch`)

**Audience:** Callers of **`package hexxladb`** using Bolt-style **[`DB.View`](../../db.go)**, **[`DB.Update`](../../db.go)**, and **[`DB.Batch`](../../db.go)**.

## Locking

- **`View`** acquires a **read lock**: many concurrent **`View`** calls can run; they block while an **`Update`** or **`Batch`** holds the **database** lock.
- **`Update`** acquires the **database** lock before beginning the engine write transaction and keeps it through the callback, group-WAL commit wait, changelog projection, and durable-outbox acknowledgement. No concurrent **`View`**, **`Update`**, or **`Batch`** can observe or build on staged engine state. A successful return publishes the complete commit, including its **`CommitSeq`** and changefeed records.
- **`Batch`** is **equivalent** to **`Update`** (same lock and semantics). It exists for alignment with the spec’s `Batch` name and ecosystem expectations. Each successful **`Update`** / **`Batch`** uses one engine **write transaction**; see **[`DURABILITY.md`](./DURABILITY.md)** for barrier ordering.

[`DB.WriteStats`](../../write_stats.go) returns lock-free cumulative timings for accepted public write calls. `LockWait` measures time waiting to acquire the database lock; `Callback`, `Durability`, and `Finalization` measure work while that lock remains held. Read two samples to calculate interval counts and averages without installing an in-library metrics system.

## Snapshot semantics and MVCC

- **Format v1:** A **`View`** sees the **ordered store** (B+ tree) as it was when the read lock was acquired—i.e. **last committed state** at that moment.
- **Format v2 (MVCC):** Open a **new** database with **[`Options.EnableMVCC`](../../options.go)**. **`View`** pins **`read_seq = header.CommitSeq`** at transaction start (last committed snapshot). **`ViewAt(read_seq uint64)`** pins an **older** committed snapshot; **`read_seq`** must not exceed **`CommitSeq`** or **[`ErrReadSeqFuture`](../../errors.go)** is returned. **`ViewAtTime(time.Time)`** maps wall-clock to the most recent commit with `commit_time <= as_of` and pins that snapshot. If no commit exists at/before `as_of`, it resolves to `read_seq=0` (empty snapshot). Each successful **`Update`** / **`Batch`** records an **`__meta/commit-time/`** entry (wall time sampled at **transaction start**, before the callback) and advances **`CommitSeq`** in the same engine transaction as the versioned data and durable changefeed intents.

## `Close`

**[`DB.Close`](../../db.go)** takes the **exclusive** lock and waits for any in-flight **`View`**, **`Update`**, or **`Batch`** to finish before closing files. New **`View`** / **`Update`** / **`Batch`** after a successful close return **[`ErrDatabaseClosed`](../../errors.go)**.

## Nesting and reentrancy

- **`View`** may call **`View`** again (nested read locks are allowed).
- Do **not** call **`Update`** or **`Batch`** from inside **`View`**, or **`View`** from inside **`Update`** / **`Batch`**, on the **same** `DB`—that can **deadlock**.
- **`Update`** and **`Batch`** are **not re-entrant**: do not nest **`Update`**, **`Batch`**, or mix them on the same `DB` (same goroutine will deadlock on the mutex).

## Commit finalization failures

`Update` / `Batch` use an authoritative commit followed by a recoverable projection:

1. For MVCC, stage the commit timeline before invoking the callback.
2. After a successful callback, stage bounded changefeed intents under private `__meta/changelog-outbox/` keys and stage the new header `CommitSeq`.
3. Commit data pages, the outbox, its head, and `CommitSeq` through one engine/WAL boundary.
4. Project those intents to the external changelog. Once its frames are durable, remove the acknowledged outbox keys in an internal cleanup transaction that does not advance `CommitSeq` or emit events.

If the callback or a pre-commit stage fails, the engine transaction is aborted. If the engine reports an error while committing, **[`ErrCommitFinalization`](../../errors.go)** means the outcome is uncertain; close and reopen before inspecting authoritative state or retrying.

If the engine commit is known durable but changelog append, sync, or outbox cleanup fails, the error matches both **`ErrCommitFinalization`** and **[`ErrCommitDurable`](../../errors.go)**. Do **not** retry the mutation. Further writes on that handle are rejected; close and reopen with the same changelog configuration. `Open` projects remaining durable intents before returning a handle. A crash after changelog append but before outbox cleanup may redeliver the same logical mutation, so consumers must remain idempotent.

## Byte keys and lattice primitives

**[`Tx.Get`](../../tx.go)**, **`Put`**, **`AscendRange`** operate on raw **`[]byte`** keys/values backed by the engine B+ tree.

The lattice API adds **`PutCell`**, **`GetCell`**, **`WalkRing`**, **`PutSeam`**, **`FindSeams`**, **`LoadContext`**, and **`ResolveSeam`** on **`Tx`** (see [`primitives.go`](../../primitives.go)). Writes require **`Update`**; reads use **`View`** (or **`Update`**).

- **`PutSeam`** writes **`seam/<ulid>`** plus a **`seam-by-cells/<lo>/<hi>/<ulid>`** secondary (empty value). Changing endpoints for an existing ULID returns **`ErrSeamEndpointMismatch`**. **`FindSeams`** uses the secondary index over cells in the query ball and loads primaries by ULID.

## Spec-named helpers

Mapping to [`HEXXLA_DB.md`](./HEXXLA_DB.md) Native Query Primitives:

- `mark_conflict`: **[`Tx.MarkConflict`](../../primitives.go)** — new ULID seam, canonical endpoints, **`SeamType`** `"mark_conflict"`, then **`PutSeam`**.
- `update_facet` (derivation rule): **[`Tx.UpdateFacet`](../../facets_edges.go)** — requires **`DerivationHash`** = SHA-256 of the cell’s current **`RawContent`**; otherwise **`ErrFacetDerivationMismatch`**. Missing cell: **`ErrCellNotFound`**. Unconstrained writes still use **`PutFacet`**.
- `link_cells`: **[`Tx.LinkCells`](../../facets_edges.go)** — packs coords and **`PutEdge`**.

## Validity filters and facet ring loads

Single-version **read filters** on the current committed cell and seam (not MVCC; for **`ViewAt`** / **`ViewAtTime`** see **Snapshot semantics** and **MVCC temporal semantics** above):

- **[`record.ValidAt`](../../internal/record/validity.go)** — half-open validity window **`[ValidFrom, ValidTo)`** in Unix nanoseconds UTC (`nil` bound = open on that side).
- **[`Tx.WalkRingAt`](../../primitives.go)** — same ring order as **`WalkRing`**, but invokes the callback only for cells whose **`Validity`** contains **`asOf`** (missing or out-of-window cells are skipped).
- **[`Tx.LoadContext`](../../context_load.go)** with **`LoadContextConfig.AsOf`** — assembles only cells whose validity contains **`asOf`**; `MaxCells` remains the upper bound on returned cells.
- **[`Tx.FindSeamsAt`](../../primitives.go)** — like **`FindSeams`**, but only includes seams whose **[`SeamRecord.Validity`](../../internal/record/types.go)** contains **`asOf`**. Seams stored without a validity suffix decode as an open window (always included when **`asOf`** is used).
- **[`Tx.WalkRingFacets`](../../primitives.go)** — for each ring coordinate with an existing cell (and optional **`asOf`** filter on the cell’s validity), loads facet records for **`facet_id`** bits **`0..5`** set in the 6-bit **`facetMask`** (bits outside **`0x3f`** → **`ErrInvalidArgument`**). Typical cost **O(ring_cells × popcount(mask))** btree **`GetFacet`** operations; facets are returned in ascending **`facet_id`** order (missing keys omitted).

## MVCC temporal semantics

For format-v2 databases the authoritative visibility clock is **`CommitSeq`** in the engine header (see [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)). Each successful [`Update`](../../tx.go) / [`Batch`](../../tx.go) advances `CommitSeq` atomically with its engine transaction; a reopened database therefore cannot expose versioned pages under an older reused sequence.

- **[`DB.ViewAt(readSeq)`](../../tx.go)** pins `read_seq` for the callback. Cell, facet, and seam point reads use the version suffix's byte ordering to seek directly to the largest stored `commit_seq <= read_seq`, then stop after one valid version. [`ErrReadSeqFuture`](../../errors.go) is returned if `read_seq` exceeds the header's `CommitSeq`.
- **[`DB.ViewAtTime(asOf)`](../../tx.go)** maps UTC wall time to a `read_seq` using the commit timeline: during each MVCC `Update`, an `__meta/commit-time/` btree key records `(wall_unix_nano, writeSeq)` (wall timestamp sampled at transaction start). The resolver performs a reverse bounded seek and picks the maximum `commit_seq` at or before `asOf`. Determinism requires stable UTC clock usage; the same `asOf` yields the same snapshot for a given database history.
- **Secondary indexes (`source/`, `time/`, seam secondaries):** version-suffixed secondary keys coexist with MVCC primaries; [`AscendCellsBySource`](../../cell_secondary.go) (and seam variants) dedupe by logical ID and evaluate visibility via [`GetCell`](../../mvcc.go) / seam readers at the transaction `read_seq`.
- **Validity vs snapshot:** [`WalkRingAt`](../../primitives.go), [`LoadContextConfig.AsOf`](../../context_load.go), and [`record.ValidAt`](../../internal/record/validity.go) filter by record validity windows — orthogonal to `read_seq` (snapshot time vs. domain validity interval).

## Secondary indexes — `source/` and `time/`

Per [HEXXLA_DB.md](./HEXXLA_DB.md) Storage Layout, **`PutCell`** dual-writes secondary keys (empty values, like seam secondaries):

- **`source/<u16be len><source_id bytes>/<packed_coord>`** — when **`Provenance.SourceID`** is non-empty after trim. **`SourceID`** length is capped ([`index.MaxSourceIDBytes`](../../internal/index/source_key.go)); oversize returns **`ErrInvalidArgument`**.
- **`time/<int64be week_bucket>/<packed_coord>`** — when **`Validity.ValidFrom`** is set; **`week_bucket`** = **`ValidFrom` / (7×24h in nanoseconds)** ([`index.WeekBucketFromValidity`](../../internal/index/time_key.go)). No **`time/`** entry when **`ValidFrom`** is nil.

**Seams** use separate prefixes so keys do not collide with cell keys:

- **`seam-source/<u16be len><source_id>/<ulid>`** — when **[`SeamRecord.Provenance.SourceID`](../../internal/record/types.go)** is non-empty after trim ([`index.SeamSourceKey`](../../internal/index/seam_secondary_keys.go)).
- **`seam-time/<int64be week_bucket>/<ulid>`** — when **`SeamRecord.Validity.ValidFrom`** is set (same week bucket scheme as cells).

**`PutSeam`** removes stale seam secondaries only for format v1 overwrite semantics. Under MVCC v2, seam primaries and seam source/time secondaries are versioned by `commit_seq`, and read paths select the visible version for the transaction snapshot.

On v1 overwrite, stale secondaries are removed via **[`engine.BTree.Delete`](../../internal/engine/btree_delete.go)** before attaching new index keys.

Read paths for **cells**: **[`Tx.AscendCellsBySource`](../../cell_secondary.go)** (prefix scan by **`source_id`**), **[`Tx.AscendCellsInTimeBucket`](../../cell_secondary.go)** (one UTC week bucket), **[`Tx.AscendCellsByTag`](../../cell_secondary.go)** (prefix scan by **`tag`**; secondaries maintained by **`PutCell`**), **[`Tx.AscendDistinctTags`](../../cell_secondary.go)** / **[`Tx.ListExistingTopics`](../../cell_secondary.go)** (distinct **`tag`** values visible at this snapshot).

## Logical changefeed

Optional **provenance log** (append-only file next to the database, not page-image WAL tail):

- Enable with **[`Options.ChangelogEnabled`](../../options.go)** (see **[`Options.ChangelogPath`](../../options.go)**, **[`Options.ChangelogLazy`](../../options.go)** for path override and fsync tradeoffs).
- Read bounded batches with **[`DB.ReadChangelogSince`](../../db_changelog.go)** after commits from **`Update`** / **`Batch`**.

Semantics, cursors, at-least-once delivery, and durability modes are documented in **[`CHANGEFEED.md`](./CHANGEFEED.md)**.

## Encryption

Optional **AES-256-XTS** at the engine page boundary is configured via **[`Options`](../../options.go)** on **[`Open`](../../db.go)** (`EncryptionKey` and/or `Passphrase`). Transactions see **plaintext**; ciphertext applies to data pages on disk and in the WAL. Threat model, WAL behavior, and limitations are documented in **[`ENCRYPTION.md`](./ENCRYPTION.md)**.
