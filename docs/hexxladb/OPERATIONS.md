# Operating HexxlaDB (embedded)

**Audience:** Operators and integrators embedding [`package hexxladb`](../../doc.go) via [`Open`](../../db.go) / [`Close`](../../db.go).

## Files on disk

- **Primary database** — path passed to [`Open`](../../db.go) (e.g. `/var/lib/app/data.db`).
- **Write-ahead log** — `{primary}-wal` (same directory). Described in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).
- **Changelog** (optional) — separate append-only provenance log when [`Options.ChangelogEnabled`](../../options.go) is set; see [`CHANGEFEED.md`](./CHANGEFEED.md).

Both primary and WAL matter for durability: the engine appends redo records to the WAL, then applies them to the primary. After a clean shutdown the WAL may be truncated; after a crash, [`Open`](../../db.go) replays pending WAL records into the primary.

### File growth (extend-only allocation)

The engine uses an **extend-only** page allocator: deleted or pruned records reclaim logical space inside the B+ tree, but the primary file length does **not** shrink automatically and there is **no freelist compaction** in v1. Expect monotonic file growth under churn until you vacuum via an offline copy/migration strategy (copy into a fresh database and swap files). See [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for the page/WAL layout.

## Backup and copy

- **Preferred:** Close the database ([`DB.Close`](../../db.go)) so files are consistent, then copy **both** primary and WAL (if present and non-empty), or copy the directory after close.
- **Filesystem snapshots:** Snapshot the volume containing both files at the same logical point in time. Copying only the primary without the WAL (or mixing files from different times) can yield **corruption** or lost data.
- **Live copy** without application cooperation is not documented as safe; use application-level export if you need hot backup.

## Encryption

Optional **AES-256-XTS** at the page layer is configured with [`Options`](../../options.go) — see [`ENCRYPTION.md`](./ENCRYPTION.md) for keys, passphrases, WAL ciphertext, and limitations.

Wrong key/passphrase fails deterministically at open with [`ErrEncryptionKeyMismatch`](../../errors.go) once the database has an encryption verifier (new encrypted databases and upgraded legacy encrypted databases).

Use [`RotateEncryption`](../../rotation.go) for offline key rotation/re-encryption. For large databases, prefer [`RotateEncryptionWithOptions`](../../rotation.go) to stream rows in batches and emit progress callbacks.

## Observability

The reference binary [`cmd/hexxladb`](../../cmd/hexxladb/main.go) uses structured logging (`log/slog`) with configurable `LOG_LEVEL` (see [README](../../README.md)). Long-running services should follow the same pattern: log at the composition root and adapters, not inside [`internal/domain`](../../internal/domain).

## MVCC retention and pruning

For format-v2 databases (open a **new** database with [`Options.EnableMVCC`](../../options.go)):

- [`Options.MVCCRetention.RetainCommitsBehindHead`](../../options.go) configures how much commit history to retain when deriving a suggested prune watermark. Only versions with strictly lower `commit_seq` than `(header.CommitSeq - RetainCommitsBehindHead)` may be reclaimed, and never the latest visible version per logical cell.
- Zero (default) disables automatic suggestions; operators supply `beforeSeq` explicitly to [`PruneCellVersions`](../../mvcc_lifecycle.go).
- Retention is in **commits**, not wall-clock. Map product SLAs to `RetainCommitsBehindHead` using your observed commits-per-interval.

Quick API reference:

| API | Purpose |
|-----|---------|
| [`DB.StatsMVCC`](../../mvcc_lifecycle.go) | `CommitSeq`, logical cell count, versioned row count |
| [`DB.SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go) | `beforeSeq` from open-time retention policy |
| [`DB.PruneCellVersions`](../../mvcc_lifecycle.go) | Explicit bounded prune pass |
| [`DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go) / [`MVCCPrunePlan`](../../mvcc_lifecycle.go) | Profile defaults (`low-latency`, `balanced`, `long-history`) |
| [`PruneScheduler.Tick`](../../mvcc_lifecycle.go) | One bounded pass — call from your own timer; no background goroutine inside the library |

Recommended cadence: during low-traffic windows, loop `PruneScheduler.Tick` or `PruneCellVersions` until a pass deletes `0` rows, then re-check `StatsMVCC`.

## Pre-release soak checklist

Use after meaningful storage/MVCC changes or before tagging a release. Capture machine type, git SHA, and wall-clock duration in your release notes.

| Step | Command                        | Pass criteria                                                                                                                           |
| ---- | ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | `make ci`                      | Exits `0`; includes unit tests + race.                                                                                                  |
| 2    | `make integration`             | Exits `0`; includes `TestIntegration_MVCC_sustainedPutCellSameKey` and `TestIntegration_MVCC_latticeAndHighChurnPrune`.                 |
| 3    | _(Optional)_ `make stress`     | Large cell load, not MVCC churn; skip on resource-constrained CI.                                                                       |
| 4    | Disk growth sanity             | Note DB + WAL (+ changelog if enabled) size before/after soak; bounded growth after prune per retention policy above.                   |

Tune retention and pruning for your workload and soak longer in staging if retention windows are large.

## Crash recovery drill

1. Kill the embedding process during an active [`Update`](../../tx.go) (SIGKILL).
2. [`Open`](../../db.go) the same path; verify WAL replay succeeds and [`View`](../../tx.go) reads match expectations.
3. If [`ErrCorruptDatabase`](../../errors.go): restore from last known-good primary + WAL pair (see **Backup and copy**).

## Backup drill

1. `Close` the database (or stop the sole writer).
2. Copy primary and `{primary}-wal` together from the same instant.
3. Restore on a staging host, `Open`, run read probes (`GetCell`, `StatsMVCC`).

## Incident response checklist

### 1) Encryption key mismatch

**Signal:** `Open` returns `ErrEncryptionKeyMismatch` (deterministic verifier failure).

**Response:** Confirm key derivation path (env/secrets manager); never guess keys in production. Recovery: restore from backup taken with correct key material, or offline [`RotateEncryption`](../../rotation.go) after establishing a readable copy.

### 2) WAL / primary corruption on open

**Signal:** `ErrCorruptDatabase` or WAL replay failure (wrapped engine errors).

**Response:** Restore primary + `{primary}-wal` from the same logical instant (see **Backup and copy**). Do not mix WAL from another run.

### 3) MVCC btree errors during prune (`engine: corrupt B+ tree page`)

**Signal:** `PruneCellVersions` or `StatsMVCC` ascent fails mid-operation.

**Response:** Stop writing; backup files; restore from known-good snapshot. Only use `Update` / primitives — avoid raw `Tx.Put` reordering `cell/` vs `__meta/commit-time/` keys on format v2.

### 4) Changelog tail corruption

**Signal:** `ErrChangelogCorrupt` from `ReadChangelogSince`.

**Response:** Pause consumers; truncate/repair changelog per ops policy; re-bootstrap derived state from DB truth + [`CHANGEFEED.md`](./CHANGEFEED.md) reconciliation steps.

## Post-v1 backlog

Cross-node replication, automated prune policy in-process, materialized changefeed consumers, and hybrid embed/service routing are out of the current spec. Track the repository's technical roadmap in [`docs/ROADMAP.md`](../ROADMAP.md).
