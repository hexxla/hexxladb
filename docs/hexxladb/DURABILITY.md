# Durability and redo (engine)

**Audience:** Engineers changing [`internal/engine`](../../internal/engine), transaction semantics, or evaluating **group commit / fewer `fsync`** trade-offs.

HexxlaDB’s embedded engine uses a **redo WAL** beside the primary file. This document describes the **ordering guarantees today** ([`Engine.WritePage`](../../internal/engine/engine.go)), how **`Open`** recovery works, where **`DB.Update`** fits, and why **batching WAL `Sync`** without broader changes is constrained.

---

## Current `WritePage` ordering (single page)

For each durable page mutation, [`WritePage`](../../internal/engine/engine.go) performs:

1. Encrypt/transform plaintext (`Hooks.BeforeWrite`).
2. Append one **redo record** to the WAL (`wal.Write`).
3. **`wal.Sync()`** — redo record is durable on storage before primary is trusted.
4. **`db.WriteAt`** plaintext to the primary file at the page offset.
5. **`db.Sync()`** on the primary.
6. Read/modify/write **header page** (`LastWALSeq`, `NextPageID`).
7. **`db.Sync()`** again so header + primary state match.

Invariant: **a WAL record for sequence `seq` is fsync’d before** the corresponding primary page for that mutation is fsync’d. Replay on startup ([`parseAndReplayWAL`](../../internal/engine/wal.go)) can therefore treat the WAL as the source of truth if the primary lags.

---

## Recovery model

On [`Open`](../../internal/engine/engine.go), the engine reads the WAL and applies records with `seq > LastWALSeq` from the header (handling MAC/CRC as configured). That heals **primary pages** when the WAL contains newer redo than the data file (e.g. crash after WAL sync but before primary write completed).

Tests: [`TestOpen_replaysPendingWAL`](../../internal/engine/engine_test.go), [`TestReplay_restoresStalePrimaryWhenWALAhead`](../../internal/engine/group_commit_spike_test.go).

---

## Public API boundary (`DB.Update` / `Batch`)

- **[`DB.Update`](../../tx.go)** / **`Batch`** hold an **exclusive lock** for the callback; **no separate group-commit pipeline** batches engine `fsync`s across logical operations unless the engine explicitly adds one.
- **MVCC:** After the callback, **`CommitSeq`** may be advanced and changelog append runs—those are **above** `WritePage` but still **one writer at a time** for a given `DB`.

See also [`TX.md`](./TX.md) (“no WAL coalescing in v1”).

---

## Group commit (P3) — what would have to change

**Goal:** reduce **`wal.Sync`** (and possibly **`db.Sync`**) calls per logical transaction.

**Constraint:** [`BTree`](../../internal/engine/btree.go) issues **`WritePage`** during **`Update`** and expects **subsequent [`ReadPage`](../../internal/engine/engine.go)** on the same handle to observe **persisted** pages for that process. Today that implies **primary data is written before** the btree continues along code paths that re-read pages.

So **skipping `wal.Sync` between two `WritePage` calls** while still writing the primary after each call would **break WAL-before-primary** unless the primary write is also deferred—which requires either:

- A **transaction-scoped dirty page cache** in the engine (serve reads from RAM until commit), or
- A **SQLite-style WAL read path** (reads merge WAL + primary), or
- **Deferring all primary writes** to end of **`Update`** (same as dirty cache / shadow state).

**Feasible building block (implemented in engine tests):** append **multiple** redo records with **`wal.Write`**, call **`wal.Sync()` once**, then apply **each** page with the same **primary + header fsync** path as today (see spike test). That **reduces WAL sync count** when multiple pages are known **before** applying primaries (not how `WritePage` is structured today without a refactor of btree I/O).

---

## Evaluating fewer `db.Sync` calls on the primary (`evaluate-primary-sync`)

Deferring **`db.Sync`** on the primary or header **without** a formal crash model risks:

- Readers or OS cache observing **partial** commits.
- **Header `LastWALSeq`** diverging from what is safely replayable after crash.

Any change should state **which barrier** corresponds to **“committed”** for [`API`](../../primitives.go) callers and preserve **replay idempotency** up to `LastWALSeq`.

Recommended next steps before implementation:

1. Formalize **commit barrier** semantics (per `Update`, per `PutCell`, etc.).
2. Prove **replay** + **property tests** under abrupt termination (power-loss simulation, kill -9 during critical sections).
3. Consider **`fdatasync`** vs **`Sync`** only after profiling **same filesystem** as production.

---

## References

- Performance audit context: [`docs/agent-feedback/BLAZINGLY_FAST_AUDIT.md`](../agent-feedback/BLAZINGLY_FAST_AUDIT.md) §2.1, §6 (P3).
- WAL record layout / replay: [`internal/engine/wal.go`](../../internal/engine/wal.go).
