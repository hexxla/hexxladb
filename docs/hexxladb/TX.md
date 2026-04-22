# Transactions (`View` / `Update` / `Batch`)

**Audience:** Callers of **`package hexxladb`** using Bolt-style **[`DB.View`](../../db.go)**, **[`DB.Update`](../../db.go)**, and **[`DB.Batch`](../../db.go)**.

## Locking

- **`View`** acquires a **read lock**: many concurrent **`View`** calls can run; they block only while an **`Update`** or **`Batch`** holds the exclusive lock.
- **`Update`** acquires a **write lock**: exclusive access—no concurrent **`View`**, **`Update`**, or **`Batch`** until the callback returns.
- **`Batch`** is **equivalent** to **`Update`** (same lock and semantics). It exists for alignment with the spec’s `Batch` name and ecosystem expectations; there is no separate internal batching or WAL coalescing in v1.

This matches **single writer, multiple readers** — see **Locking** above.

## Snapshot semantics (M5) and MVCC (E2+)

- **Format v1:** A **`View`** sees the **ordered store** (B+ tree) as it was when the read lock was acquired—i.e. **last committed state** at that moment.
- **Format v2 (MVCC):** Open a **new** database with **[`Options.EnableMVCC`](../../options.go)**. **`View`** pins **`read_seq = header.CommitSeq`** at transaction start (last committed snapshot). **`ViewAt(read_seq uint64)`** pins an **older** committed snapshot; **`read_seq`** must not exceed **`CommitSeq`** or **[`ErrReadSeqFuture`](../../errors.go)** is returned. **`ViewAtTime(time.Time)`** maps wall-clock to the most recent commit with `commit_time <= as_of` and pins that snapshot. If no commit exists at/before `as_of`, it resolves to `read_seq=0` (empty snapshot). Each successful **`Update`** / **`Batch`** records an **`__meta/commit-time/`** entry (wall time sampled at **transaction start**, before the callback) and, after the callback, advances **`CommitSeq`** in the header for deterministic `as_of` resolution.

## `Close`

**[`DB.Close`](../../db.go)** takes the **exclusive** lock and waits for any in-flight **`View`**, **`Update`**, or **`Batch`** to finish before closing files. New **`View`** / **`Update`** / **`Batch`** after a successful close return **[`ErrDatabaseClosed`](../../errors.go)**.

## Nesting and reentrancy

- **`View`** may call **`View`** again (nested read locks are allowed).
- Do **not** call **`Update`** or **`Batch`** from inside **`View`**, or **`View`** from inside **`Update`** / **`Batch`**, on the **same** `DB`—that can **deadlock**.
- **`Update`** and **`Batch`** are **not re-entrant**: do not nest **`Update`**, **`Batch`**, or mix them on the same `DB` (same goroutine will deadlock on the mutex).

## Commit finalization failures

`Update` / `Batch` run in two stages: for MVCC, a start-of-transaction btree write (commit timeline) and then the callback (where `PutCell` and other logical writes happen), then post-callback finalization (changelog append and header `CommitSeq` update). If the callback returns an error, the timeline entry for that transaction is rolled back from the btree.

If finalization fails, the API returns **[`ErrCommitFinalization`](../../errors.go)** (wrapped with cause). Callers should treat this as a **commit outcome uncertainty**: callback writes may already be persisted even though the transaction returned an error.

## Byte keys (M5) and primitives (M6)

**[`Tx.Get`](../../tx.go)**, **`Put`**, **`AscendRange`** operate on raw **`[]byte`** keys/values backed by the engine B+ tree.

M6+ adds **`PutCell`**, **`GetCell`**, **`WalkRing`**, **`PutSeam`**, **`FindSeams`**, **`LoadContext`**, and **`ResolveSeam`** on **`Tx`** (see [`primitives.go`](../../primitives.go)). Writes require **`Update`**; reads use **`View`** (or **`Update`**).

- **M7:** **`PutSeam`** writes **`seam/<ulid>`** plus a **`seam-by-cells/<lo>/<hi>/<ulid>`** secondary (empty value). Changing endpoints for an existing ULID returns **`ErrSeamEndpointMismatch`**. **`FindSeams`** uses the secondary index over cells in the query ball and loads primaries by ULID.

## Spec-named helpers (Phase B)

Mapping to [`HEXXLA_DB.md`](./HEXXLA_DB.md) Native Query Primitives:

- `mark_conflict`: **[`Tx.MarkConflict`](../../primitives.go)** — new ULID seam, canonical endpoints, **`SeamType`** `"mark_conflict"`, then **`PutSeam`**.
- `update_facet` (derivation rule): **[`Tx.UpdateFacet`](../../facets_edges.go)** — requires **`DerivationHash`** = SHA-256 of the cell’s current **`RawContent`**; otherwise **`ErrFacetDerivationMismatch`**. Missing cell: **`ErrCellNotFound`**. Unconstrained writes still use **`PutFacet`**.
- `link_cells`: **[`Tx.LinkCells`](../../facets_edges.go)** — packs coords and **`PutEdge`**.

## Validity filters and facet ring loads (Phase C)

Single-version **read filters** on the current committed cell and seam (not MVCC; for **`ViewAt`** / **`ViewAtTime`** see **Snapshot semantics** above and [`MVCC_TEMPORAL.md`](./MVCC_TEMPORAL.md)):

- **[`record.ValidAt`](../../internal/record/validity.go)** — half-open validity window **`[ValidFrom, ValidTo)`** in Unix nanoseconds UTC (`nil` bound = open on that side).
- **[`Tx.WalkRingAt`](../../primitives.go)** — same ring order as **`WalkRing`**, but invokes the callback only for cells whose **`Validity`** contains **`asOf`** (missing or out-of-window cells are skipped).
- **[`Tx.LoadContextAt`](../../primitives.go)** — same concentric order as **`LoadContext`**; **`maxCells`** applies **after** filtering by **`asOf`**.
- **[`Tx.FindSeamsAt`](../../primitives.go)** — like **`FindSeams`**, but only includes seams whose **[`SeamRecord.Validity`](../../internal/record/types.go)** contains **`asOf`**. Seams stored without a validity suffix decode as an open window (always included when **`asOf`** is used).
- **[`Tx.WalkRingFacets`](../../primitives.go)** — for each ring coordinate with an existing cell (and optional **`asOf`** filter on the cell’s validity), loads facet records for **`facet_id`** bits **`0..5`** set in the 6-bit **`facetMask`** (bits outside **`0x3f`** → **`ErrInvalidArgument`**). Typical cost **O(ring_cells × popcount(mask))** btree **`GetFacet`** operations; facets are returned in ascending **`facet_id`** order (missing keys omitted).

## Secondary indexes — `source/` and `time/` (Phase D)

Per [HEXXLA_DB.md](./HEXXLA_DB.md) Storage Layout, **`PutCell`** dual-writes secondary keys (empty values, like seam secondaries):

- **`source/<u16be len><source_id bytes>/<packed_coord>`** — when **`Provenance.SourceID`** is non-empty after trim. **`SourceID`** length is capped ([`index.MaxSourceIDBytes`](../../internal/index/source_key.go)); oversize returns **`ErrInvalidArgument`**.
- **`time/<int64be week_bucket>/<packed_coord>`** — when **`Validity.ValidFrom`** is set; **`week_bucket`** = **`ValidFrom` / (7×24h in nanoseconds)** ([`index.WeekBucketFromValidity`](../../internal/index/time_key.go)). No **`time/`** entry when **`ValidFrom`** is nil.

**Seams** use separate prefixes so keys do not collide with cell keys:

- **`seam-source/<u16be len><source_id>/<ulid>`** — when **[`SeamRecord.Provenance.SourceID`](../../internal/record/types.go)** is non-empty after trim ([`index.SeamSourceKey`](../../internal/index/seam_secondary_keys.go)).
- **`seam-time/<int64be week_bucket>/<ulid>`** — when **`SeamRecord.Validity.ValidFrom`** is set (same week bucket scheme as cells).

**`PutSeam`** removes stale seam secondaries only for format v1 overwrite semantics. Under MVCC v2, seam primaries and seam source/time secondaries are versioned by `commit_seq`, and read paths select the visible version for the transaction snapshot.

On v1 overwrite, stale secondaries are removed via **[`engine.BTree.Delete`](../../internal/engine/btree_delete.go)** before attaching new index keys.

Read paths for **cells**: **[`Tx.AscendCellsBySource`](../../cell_secondary.go)** (prefix scan by **`source_id`**), **[`Tx.AscendCellsInTimeBucket`](../../cell_secondary.go)** (one UTC week bucket), **[`Tx.AscendCellsByTag`](../../cell_secondary.go)** (prefix scan by **`tag`**; secondaries maintained by **`PutCell`**).

## Logical changefeed (Phase G)

Optional **provenance log** (append-only file next to the database, not page-image WAL tail):

- Enable with **[`Options.ChangelogEnabled`](../../options.go)** (see **[`Options.ChangelogPath`](../../options.go)**, **[`Options.ChangelogLazy`](../../options.go)** for path override and fsync tradeoffs).
- Read bounded batches with **[`DB.ReadChangelogSince`](../../db_changelog.go)** after commits from **`Update`** / **`Batch`**.

Semantics, cursors, at-least-once delivery, and durability modes are documented in **[`CHANGEFEED.md`](./CHANGEFEED.md)**.

## Encryption (M9)

Optional **AES-256-XTS** at the engine page boundary is configured via **[`Options`](../../options.go)** on **[`Open`](../../db.go)** (`EncryptionKey` and/or `Passphrase`). Transactions see **plaintext**; ciphertext applies to data pages on disk and in the WAL. Threat model, WAL behavior, and limitations are documented in **[`ENCRYPTION.md`](./ENCRYPTION.md)**.
