# Durability and redo (engine)

**Audience:** Engineers changing [`internal/engine`](../../internal/engine), transaction semantics, or evaluating **group commit / fewer `fsync`** trade-offs.

HexxlaDB’s embedded engine uses a **redo WAL** beside the primary file. This document describes **write transactions** (grouped WAL + dirty pages), the **legacy immediate** `WritePage` path, how **`Open`** recovery works, and how **`DB.Update`** uses the **group WAL** commit pipeline.

---

## `DB.Update` and engine write transactions

[`DB.Update`](../../tx.go) / [`Batch`](../../tx.go) call [`Engine.BeginWriteTxn`](../../internal/engine/writetxn.go) under the `DB` **exclusive** lock, run the callback, then on success commit the engine before changelog append and MVCC header finalization. On callback error (or a failed engine commit), [`Engine.AbortWriteTxn`](../../internal/engine/writetxn.go) discards the in-memory transaction.

**Within a write transaction**, [`WritePage`](../../internal/engine/engine.go) does **not** hit the WAL or primary immediately: it enqueues **redo in memory** and records **dirty** page payloads; [`readPagePooled`](../../internal/engine/engine.go) / [`ReadHeader`](../../internal/engine/engine.go) / [`UpdateHeader`](../../internal/engine/engine.go) use an in-memory view so the btree can **read your writes** before commit.

### Group WAL (default in `hexxladb`)

The shared library path uses a **group WAL flusher** (see also [`TX.md`](./TX.md) for lock scope):

1. A successful commit enqueues a **flusher job** via [`Engine.CommitWriteTxnBeginAsync`](../../internal/engine/writetxn.go) (or the synchronous handoff in [`Engine.CommitWriteTxn`](../../internal/engine/writetxn.go) when grouped). Redo is written and barriers applied in **batches** that may include **more than one** logical `DB.Update` per batch.
2. For each **applied batch** of flusher work, ordering is: **all** `wal.Write` for all redo in the batch, then **one** `wal.Sync`, then all primary data `WriteAt`s, then **one** primary `Sync`, then **one** `writeHeaderAt` (merged `LastWALSeq` and header) and a final primary `Sync`.

Coalescing and `MaxBatchWait` (via [`Options.GroupWALMaxBatchWait`](../../options.go)) are documented under **Tuning** below. Redo sequence allocation is **monotonic**; until a batch is on disk, data pages are served from an **overlay** and the post-commit header may be **staged** for consistent reads (see [Group WAL invariants](#group-wal-invariants)). If a commit or later finalization fails, the API may return **`ErrCommitFinalization`** (see [`errors.go`](../../errors.go) and [`tx.go`](../../tx.go)).

**Direct** use of the engine (tests, internal tooling) can call `WritePage` **without** `BeginWriteTxn`: each call uses the **immediate** path below (one WAL sync per page). Internal tests may open the engine with **group WAL disabled** to exercise the non-group `CommitWriteTxn` path.

---

## Group WAL invariants

1. **Ordering:** Redo records and `LastWALSeq` follow a single total order; `seq` is allocated from a monotonic counter when writes overlap the flusher.

2. **Barriers (per applied batch):** `wal.Write` for all records in the batch, then one `wal.Sync`, then all affected primary `WriteAt`s, one primary `Sync`, one `writeHeaderAt` (last job’s header merged, `LastWALSeq` = max redo `seq` in the batch) and final primary `Sync`.

3. **Visibility (engine):** With no active `wtxn`, [`readPagePooled`](../../internal/engine/engine.go) may serve the **overlay**; [`visibleHeader`](../../internal/engine/engine.go) can merge the **staged** header with on-disk so readers and the next `BeginWriteTxn` see a consistent tree.

4. **Visibility (DB + MVCC):** [`View`](../../tx.go) may run while an [`Update`](../../tx.go) has **released** `db.mu` during the async `wait` after `CommitWriteTxnBeginAsync`—see [`TX.md`](./TX.md). [`UpdateHeader(CommitSeq)`](../../tx.go) runs with `db.mu` held after `wait` returns, so `CommitSeq` is not published until that point. [`writeSeq`](../../tx.go) is assigned with [`writeSeqNext`](../../db.go) so MVCC `writeSeq` values stay unique when `Update`s overlap.

5. **Failure:** If a batch operation fails, the flusher signals all jobs, clears staging, and drops overlay state for the failed work. `Close` stops the flusher and drains work.

6. **Strict success = durable:** Each logical commit still **blocks** until the batch that contains it is fully applied to storage (same durability class as a non-group per-update barrier).

7. **Observability / tests:** [`Engine.GroupWALStats`](../../internal/engine/group_wal.go) reports batch counts and `wal.Sync` calls; the root package also exposes [`DB.GroupWALStats`](../../db.go) for the same metrics without importing `internal/engine`. [`TestGroupWAL_twoJobsOneBatch`](../../internal/engine/group_wal_merge_test.go) checks that two enqueued jobs merge into one flusher batch.

8. **Prune and immediate deletes:** The library’s [`PruneCellVersions`](../../mvcc_lifecycle.go) issues one B+-tree delete per reclaimed key, each on the **immediate** `WritePage` path (no `BeginWriteTxn` coalescing). Coalescing many deletes in a **single** engine write transaction would reduce per-delete WAL syncs but requires a designed API; today operators rely on bounded `maxDelete` and repeated passes.

### Tuning

- [`Options.GroupWALMaxBatchWait`](../../options.go) sets the flusher’s coalescing window (default **2ms** in the engine when zero).

---

## Legacy immediate `WritePage` (no open write transaction)

For each call when **no** write transaction is active, [`WritePage`](../../internal/engine/engine.go) performs:

1. Encrypt/transform plaintext (`Hooks.BeforeWrite`).
2. Append one **redo record** to the WAL (`wal.Write`).
3. **`wal.Sync()`** — redo record is durable on storage before primary is trusted.
4. [`persistRedoPage`](../../internal/engine/engine.go): primary `WriteAt` + `Sync`, header update + `Sync`.

Invariant: **a WAL record for sequence `seq` is fsync’d before** the corresponding primary page for that mutation is fsync’d. Replay on startup ([`parseAndReplayWAL`](../../internal/engine/wal.go)) can therefore treat the WAL as the source of truth if the primary lags.

---

## Recovery model

On [`Open`](../../internal/engine/engine.go), the engine reads the WAL and applies records with `seq > LastWALSeq` from the header (handling MAC/CRC as configured). That heals **primary pages** when the WAL contains newer redo than the data file (e.g. crash after WAL sync but before primary write completed).

Tests: [`TestOpen_replaysPendingWAL`](../../internal/engine/engine_test.go), [`TestReplay_restoresStalePrimaryWhenWALAhead`](../../internal/engine/group_commit_spike_test.go).

---

## `DB` commit vs header-only paths

- **With group WAL (API default):** Multiple `DB.Update` calls can share a single `wal.Sync` in one flusher batch. Per-update guarantees remain **durable** when `Update` returns success.
- **Without group WAL (direct engine / tests only):** One `wal.Sync` + primary barriers per committed `CommitWriteTxn` as in the non-group code path in [`CommitWriteTxn`](../../internal/engine/writetxn.go).

**MVCC:** After a successful engine commit, **changelog** append and **`CommitSeq`** ([`UpdateHeader`](../../internal/engine/engine.go) on the open engine) run as in [`TX.md`](./TX.md).

---

## Write-path scope

**Write transaction path (non-group or single-job batch):** In-memory **dirty** pages, **`wal.Sync`** for redo, batched primary `Sync` in [`CommitWriteTxn`](../../internal/engine/writetxn.go) for the non-group path, and the group flusher for the `hexxladb` default.

**Still per page (separate code path):** The legacy **immediate** `WritePage` path in [`WritePage`](../../internal/engine/engine.go) + [`persistRedoPage`](../../internal/engine/engine.go) still does one primary `WriteAt`+`Sync` (and header) per operation, by design.

**Not supported:** **SQLite-style** on-disk WAL merge into the read path (reads combine WAL + primary without a dirty cache).

[`TestSpike_twoWALRecordsOneSyncThenPersist`](../../internal/engine/group_commit_spike_test.go) and [`internal/engine/writetxn_test.go`](../../internal/engine/writetxn_test.go) assert write ordering.

---

## Primary sync policy

**Write-txn** `CommitWriteTxn` (non-group) does **one** `db.Sync` for all batched data pages. The **legacy** immediate `WritePage` path is unchanged. **Group WAL** can fuse multiple updates into one `wal.Sync` and one primary flush batch.

Supported controls and verification:

- **`fdatasync`** vs full-file **`Sync`** on the primary: [`Options.UsePrimaryFdatasync`](../../options.go) enables a data-only flush (e.g. `fdatasync(2)` on Linux) instead of `fsync(2)` for engine primary barriers. On other platforms the engine falls back to `Sync`. Validate on the **same filesystem** as production before defaulting on.
- **Integration crash harness** (see [`crash_ordering_integration_test.go`](../../crash_ordering_integration_test.go) with `//go:build integration`): set `HEXXLADB_TEST_CRASH_AT` to a named phase, `HEXXLADB_TEST_CRASH_READY` to a marker file path, spawn a test subprocess, wait for the marker, then **SIGKILL**; reopen and assert a non-torn value or absence. Phases: `group_wal_appended`, `group_wal_synced`, `group_primary_written`, `group_primary_synced`, `group_header_written` (group batch); direct engine (non-group) path uses `classic_*` in [`writetxn.go`](../../internal/engine/writetxn.go) for internal tests.

Deferring **`db.Sync`** on the primary or header **without** a formal crash model would risk readers seeing **partial** commits or **header `LastWALSeq`** diverging from replay. The write-txn ordering (WAL durable → all primary data writes → one primary `Sync` → header → `Sync`, or the equivalent **per batch** in group WAL) matches **replay idempotency** up to `LastWALSeq` (see [Recovery model](#recovery-model) and [`parseAndReplayWAL`](../../internal/engine/wal.go)).

---

## References

- Public transaction semantics: [`TX.md`](./TX.md)
- Record layout / replay: [`internal/engine/wal.go`](../../internal/engine/wal.go)
