# Operating HexxlaDB (embedded)

**Audience:** Operators and integrators embedding [`package hexxladb`](../../doc.go) via [`Open`](../../db.go) / [`Close`](../../db.go).

## Files on disk

- **Primary database** — path passed to [`Open`](../../db.go) (e.g. `/var/lib/app/data.db`).
- **Write-ahead log** — `{primary}-wal` (same directory). Described in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).
- **Changelog** (optional) — separate append-only provenance log when [`Options.ChangelogEnabled`](../../options.go) is set; see [`CHANGEFEED.md`](./CHANGEFEED.md).

Both primary and WAL matter for durability: the engine appends redo records to the WAL, then applies them to the primary. After a clean shutdown the WAL may be truncated; after a crash, [`Open`](../../db.go) replays pending WAL records into the primary.

### File growth (extend-only allocation)

The engine uses an **extend-only** page allocator: deleted or pruned records reclaim logical space inside the B+ tree **as free space for reuse**, but the primary file length does **not** shrink automatically. Expect monotonic **file size** under churn until you compact via [`DB.Compact`](../../compact.go) or [`CompactTo`](../../compact.go) (see **Compaction** below). See [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for the page/WAL layout.

### Deletes, tombstones, and why the file size barely moves

On **format v2 (MVCC)**, [`DeleteCell`](../../delete_cell.go) does **not** remove the cell’s primary history: it appends a **tombstone** row (zero-length value at a new `commit_seq`). That **adds** a physical btree entry and usually **grows** WAL and sometimes the primary (new pages or split pages), even while the **visible** cell count drops.

So a pattern like “82 cells → delete 10 → file still **576 KiB**” is **normal**: freed space is mostly **internal reuse**, not a shorter file. To **reduce bytes on disk**:

1. Optionally [**`PruneCellVersions`**](../../mvcc_lifecycle.go) to drop **old** non-latest versions (cannot remove the latest tombstone for a coord while that key still exists).
2. Then [**`Compact`**](../../compact.go) to rewrite into a tight file (reclaims freelist/low-fill pages).

Without **prune + compact**, expect similar **file** size; [`StatsMVCC`](../../mvcc_lifecycle.go).`VersionedRows` typically **increases** after deletes (one new row per successful delete).

### `StatsMVCC` / `logical_cells` vs visible cell count

[`DB.StatsMVCC`](../../mvcc_lifecycle.go) counts **physical** `cell/<coord><seq>` rows and **distinct coords** that still have **any** stored version (including the latest **tombstone**):

- **`VersionedRows`** — total versioned primary rows (grows with puts **and** deletes).
- **`LogicalCells`** — number of distinct coordinates that still have at least one version row (coords you deleted are **still counted** until history is pruned away from the latest tombstone case per your retention story — the latest row per coord always remains until pruned per `PruneCellVersions` rules).

**Visible** cells (non-tombstone latest value) are what [`DB.HealthCheck`](../../health.go) **CellCount** reflects, or what you get from [`GetCell`](../../primitives.go) / query APIs. Do not equate **`logical_cells`** with “rows the user can see.”

### Ten delete calls but visible count dropped by eight

[`DeleteCell`](../../delete_cell.go) is **idempotent**: deleting a coord with **no visible cell** returns **nil** and writes nothing. So **N** delete tools calls can yield **fewer than N** visible-cell drops if:

- two coords were already empty or already tombstoned,
- coordinates were wrong (`q`,`r` typo),
- or the client retried the same delete.

Confirm with **HealthCheck `cell_count`**, or enumerate coords before/after.

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
- While **`CommitSeq ≤ RetainCommitsBehindHead`**, [`SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go) yields **`beforeSeq = 0`**, so [`PruneCellVersions`](../../mvcc_lifecycle.go) **`seq < beforeSeq`** matches **no** rows — **automatic prune ticks become a no-op** until commits accumulate beyond the retention depth. Embedders with short-lived MCP sessions therefore need a retention value **below** typical peak `CommitSeq` (inspect [`StatsMVCC`](../../mvcc_lifecycle.go) `.CommitSeq`) or explicit `beforeSeq`.
- Zero (default) disables automatic suggestions; operators supply `beforeSeq` explicitly to [`PruneCellVersions`](../../mvcc_lifecycle.go).
- Retention is in **commits**, not wall-clock. Map product SLAs to `RetainCommitsBehindHead` using your observed commits-per-interval.

Quick API reference:

| API                                                                                                     | Purpose                                                                                 |
| ------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| [`DB.StatsMVCC`](../../mvcc_lifecycle.go)                                                               | `CommitSeq`, logical cell count, versioned row count                                    |
| [`DB.SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go)                                                 | `beforeSeq` from open-time retention policy                                             |
| [`DB.PruneCellVersions`](../../mvcc_lifecycle.go)                                                       | Explicit bounded prune pass                                                             |
| [`DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go) / [`MVCCPrunePlan`](../../mvcc_lifecycle.go) | Profile defaults (`low-latency`, `balanced`, `long-history`)                            |
| [`PruneScheduler.Tick`](../../mvcc_lifecycle.go)                                                        | One bounded pass — call from your own timer; no background goroutine inside the library |

Recommended cadence: during low-traffic windows, loop `PruneScheduler.Tick` or `PruneCellVersions` until a pass deletes `0` rows, then re-check `StatsMVCC`.

## Compaction

Copy-compaction rewrites all B+ tree keys sequentially into a fresh file, reclaiming freelist gaps and pages with low fill factor. All data — including MVCC version rows and tombstones — is copied verbatim, preserving full snapshot history.

### Quick reference

| API                              | Purpose                                                                |
| -------------------------------- | ---------------------------------------------------------------------- |
| [`DB.Compact`](../../compact.go) | Compact an open database to `destPath`; holds a read lock during copy. |
| [`CompactTo`](../../compact.go)  | Standalone: open `srcPath`, compact to `destPath`, close both.         |

### Typical workflow

```go
err := db.Compact(ctx, "/tmp/compacted.db")
db.Close()
os.Rename("/tmp/compacted.db", originalPath)
db, _ = hexxladb.Open(originalPath, opts)
```

### Stripping old MVCC versions

`Compact` copies tombstones and old versions verbatim. To reclaim that space, prune first:

```go
db.PruneCellVersions(beforeSeq, 100_000) // remove stale versions
db.Compact(ctx, destPath)                // rewrite without dead pages
```

### Notes

- **Format preservation:** destination inherits format version, MVCC flag, `MaxValueBytes`, and encryption from the source header. Encryption credentials must be supplied via `opts` for encrypted sources.
- **Context cancellation:** partial destination is removed on abort.
- **Changelog:** the destination does not carry over the source changelog file. Re-enable changelog on the reopened destination if needed.

## Pre-release soak checklist

Use after meaningful storage/MVCC changes or before tagging a release. Capture machine type, git SHA, and wall-clock duration in your release notes.

| Step | Command                    | Pass criteria                                                                                                           |
| ---- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| 1    | `make ci`                  | Exits `0`; includes unit tests + race.                                                                                  |
| 2    | `make integration`         | Exits `0`; includes `TestIntegration_MVCC_sustainedPutCellSameKey` and `TestIntegration_MVCC_latticeAndHighChurnPrune`. |
| 3    | _(Optional)_ `make stress` | Large cell load, not MVCC churn; skip on resource-constrained CI.                                                       |
| 4    | Disk growth sanity         | Note DB + WAL (+ changelog if enabled) size before/after soak; bounded growth after prune per retention policy above.   |

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
