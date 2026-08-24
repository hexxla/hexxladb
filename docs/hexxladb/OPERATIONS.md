# Operating HexxlaDB (embedded)

**Audience:** Operators and integrators embedding [`package hexxladb`](../../doc.go) via [`Open`](../../db.go) / [`Close`](../../db.go).

## Files on disk

- **Primary database** — path passed to [`Open`](../../db.go) (e.g. `/var/lib/app/data.db`).
- **Write-ahead log** — `{primary}-wal` (same directory). Described in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).
- **Changelog** (optional) — separate append-only provenance log when [`Options.ChangelogEnabled`](../../options.go) is set; see [`CHANGEFEED.md`](./CHANGEFEED.md).

Both primary and WAL matter for durability: the engine appends redo records to the WAL, then applies them to the primary. After a clean shutdown the WAL may be truncated; after a crash, [`Open`](../../db.go) replays pending WAL records into the primary.

### File growth (extend-only allocation)

The engine uses an **extend-only** page allocator with no freelist: pages made unreachable by deletes, pruning, or tree rewrites become dead space and are not reused. The primary file length therefore does **not** shrink automatically. Expect monotonic **file size** under churn until you compact via [`DB.Compact`](../../compact.go) or [`CompactTo`](../../compact.go) (see **Compaction** below). See [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for the page/WAL layout.

[`DB.StorageStats`](../../storage_stats.go) provides one consistent measurement while holding the database read lock:

- **`PrimaryBytes`, `WALBytes`, `ChangelogBytes`** are current physical file lengths. `ChangelogBytes` is zero when the sidecar is disabled.
- **`AllocatedPages` and `ReachablePages`** include the header page. Reachability is calculated by walking the current B+ tree and every referenced overflow chain, so it survives reopen rather than relying on an in-memory delete counter.
- **`LiveBytes`** is `ReachablePages × PageSize`. It is page-rounded and includes unused capacity inside reachable pages; it is not logical payload size.
- **`ReclaimableBytes`** is the exact whole-page dead space in the current primary. Compaction may recover more by repacking low-fill reachable pages.

The walk is proportional to reachable pages and blocks writers, so sample it during a maintenance or low-traffic window rather than on every request. The older [`MVCCStats.WastedBytes`](../../mvcc_lifecycle.go) field counts only overflow payload freed since open and is deprecated for capacity decisions.

### Deletes, tombstones, and why the file size barely moves

On **format v2 (MVCC)**, [`DeleteCell`](../../delete_cell.go) does **not** remove the cell’s primary history: it appends a **tombstone** row (zero-length value at a new `commit_seq`). That **adds** a physical btree entry and usually **grows** WAL and sometimes the primary (new pages or split pages), even while the **visible** cell count drops.

So a pattern like “82 cells → delete 10 → file still **576 KiB**” is **normal**: obsolete pages remain allocated until compaction. To **reduce bytes on disk**:

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

HexxlaDB permits exactly one open handle per primary database file, across handles and processes. A competing [`Open`](../../db.go) fails immediately with [`ErrDatabaseLocked`](../../errors.go); do not bypass this lock or independently manipulate the WAL. Close the owning handle before using offline tools such as [`CompactTo`](../../compact.go) or encryption rotation.

[`DB.BackupTo`](../../backup.go) is the supported online backup path:

```go
if err := db.BackupTo(ctx, "/var/backups/hexxladb/data.db"); err != nil {
    return err
}
```

It holds the database read lock while copying and syncing one consistent recovery set. Writes and [`DB.Close`](../../db.go) wait for the full capture; existing reads may continue. Backup time and writer pause are proportional to the combined primary, WAL, and changelog size, so use a context deadline and schedule large captures around the application's write-latency budget.

The output uses fixed companion paths:

- primary: the `destPath` argument;
- WAL: `destPath + "-wal"`;
- changelog: `destPath + "-changelog"` when changelog was enabled, including when the source used a custom changelog path.

All destination component paths must be absent and their parent directory must be controlled by the embedding application. They are created with exclusive semantics and mode `0600`; an existing component is preserved and causes the operation to fail. Cancellation is checked between 1 MiB copy chunks. A returned error removes only components created by that call, never the source or a pre-existing or path-replaced destination. A process or host failure can still leave files from an interrupted call, so publish or replicate the set only after `BackupTo` returns success.

The primary and WAL are copied under the same lock after any in-flight writer has completed its durability and changefeed-finalization boundary. Do not replace this API with independent live file copies. If the handle requires close/reopen recovery after [`ErrCommitFinalization`](../../errors.go), `BackupTo` rejects the capture until recovery completes.

Encryption is preserved byte-for-byte; `BackupTo` does not need or retain credentials and does not rotate keys. Restore an encrypted backup with the same key or passphrase. Restore a changelog-enabled backup with `ChangelogEnabled: true`; the backup sidecar uses the default path beside the destination primary. Validate every backup policy with a restore drill: open the captured primary with its required options, run [`DB.HealthCheck`](../../health.go) plus application read probes, and close it cleanly before treating the artifact as recoverable.

Offline alternatives remain supported:

- close the database, then copy the primary, WAL, and optional changelog together;
- take a crash-consistent filesystem snapshot of the volume containing all enabled components at the same logical instant.

Copying only the primary, mixing a primary and WAL from different instants, or omitting the only retained changelog history is unsafe. `BackupTo` is a point-in-time backup, not replication, continuous availability, compaction, or re-encryption; HexxlaDB remains an embedded single-owner database.

When changelog is enabled, the primary may contain unacknowledged `__meta/changelog-outbox/` intents. A clean `Close` syncs and acknowledges lazy-mode intents before returning. `BackupTo` and a crash-consistent snapshot can capture pending intents; reopening the restored primary with its paired changelog completes projection automatically and may redeliver an already appended operation. Consumers must retain their at-least-once idempotency rules.

Durable named consumer cursors and their logical-history checkpoint live in the primary, so `BackupTo` preserves them with the matching sidecar. Restore with changelog enabled to validate the binding before resuming. `ErrChangelogConsumerInvalidated` means the primary and retained logical history do not match; do not reset the cursor or delete the sidecar until the correct backup set has been sought. See [`CHANGEFEED.md`](./CHANGEFEED.md#durable-consumer-cursors) for compare-and-advance and explicit re-bootstrap rules.

## Encryption

Optional **AES-256-XTS** at the page layer is configured with [`Options`](../../options.go). When the logical changelog is enabled, official database encryption also selects authenticated encrypted changelog format v2. See [`ENCRYPTION.md`](./ENCRYPTION.md) for keys, visible metadata, legacy-log handling, and the unauthenticated primary-page limitation.

Wrong key/passphrase fails deterministically at open with [`ErrEncryptionKeyMismatch`](../../errors.go) once the database has an encryption verifier (new encrypted databases and upgraded legacy encrypted databases).

Use [`RotateEncryption`](../../rotation.go) for offline key rotation/re-encryption. For large databases, prefer [`RotateEncryptionWithOptions`](../../rotation.go) to stream rows in batches and emit progress callbacks. If the changelog is enabled, it must remain enabled at the same effective path in both option sets; rotation preserves its logical records and re-encrypts its frames.

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
| [`DB.StorageStats`](../../storage_stats.go)                                                             | Primary/WAL/changelog sizes and persistent live/reclaimable page bytes                  |
| [`DB.SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go)                                                 | `beforeSeq` from open-time retention policy                                             |
| [`DB.PruneCellVersions`](../../mvcc_lifecycle.go)                                                       | Explicit bounded prune pass                                                             |
| [`DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go) / [`MVCCPrunePlan`](../../mvcc_lifecycle.go) | Profile defaults (`low-latency`, `balanced`, `long-history`)                            |
| [`PruneScheduler.Tick`](../../mvcc_lifecycle.go)                                                        | One bounded pass — call from your own timer; no background goroutine inside the library |

Recommended cadence: during low-traffic windows, loop `PruneScheduler.Tick` or `PruneCellVersions` until a pass deletes `0` rows, then re-check `StatsMVCC`.

## Compaction

Copy-compaction rewrites all B+ tree keys sequentially into a fresh file, reclaiming unreachable and low-fill pages. All data — including MVCC version rows, commit-timeline rows, and tombstones — is copied verbatim, preserving full snapshot history.

### Quick reference

| API                                                                  | Purpose                                                                            |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| [`DB.Compact`](../../compact.go)                                     | Compact an open database to `destPath`; holds a read lock during copy.             |
| [`CompactTo`](../../compact.go)                                      | Standalone: open `srcPath`, compact to `destPath`, close both.                     |
| [`DB.CompactWithOptions`](../../compact.go)                           | Open-handle compaction with a bounded batch size and durable progress checkpoints. |
| [`CompactToWithOptions`](../../compact.go)                            | Offline equivalent with credentials supplied through `Options`.                   |

### Typical workflow

1. Record `before, err := db.StorageStats()` and choose a maintenance window based on `ReclaimableBytes` plus the free space needed to hold a second primary.
2. Run bounded prune passes until one returns zero, then measure again. Prune never shrinks the primary.
3. Take or verify a recoverable backup of the source primary, WAL, and optional changelog before replacement.
4. Create a destination with `CompactWithOptions` (open plaintext source) or `CompactToWithOptions` (closed source, including encrypted sources). Use a context deadline and a progress callback; progress is emitted only after each destination batch is durable.
5. Validate the destination by opening it, checking `StorageStats`, and running application read probes or `HealthCheck` before replacing the source during an exclusive shutdown window.
6. Archive the original primary and WAL rather than overwriting the only recoverable copy. Replace files according to the changefeed rules below, reopen, and remove the archive only after validation.

Example copy phase:

```go
var copied uint64
err := db.CompactWithOptions(ctx, "/tmp/compacted.db", &hexxladb.CompactOptions{
    BatchSize: 512,
    OnProgress: func(p hexxladb.CompactProgress) {
        copied = p.CopiedKeys
    },
})
```

`BatchSize` is zero for the bounded default (4096 keys) or 1–4096. Cancel through `ctx`. A cancelled or failed copy removes the partial destination primary and WAL; retry with the same destination path restarts safely from the stable source. Compaction does not persist a byte-offset resume cursor.

### Stripping old MVCC versions

`Compact` copies tombstones and old versions verbatim. To reclaim that space, prune first:

```go
db.PruneCellVersions(beforeSeq, 100_000) // remove stale versions
db.Compact(ctx, destPath)                // rewrite without dead pages
```

### Notes

- **Format preservation:** destination inherits format version, MVCC flag, `MaxValueBytes`, and encryption from the source header. Encryption credentials must be supplied via `opts` for encrypted sources.
- **Exclusive paths:** `destPath` must not exist and is never overwritten. `CompactTo` also requires `srcPath` to be closed because it acquires the database lock itself.
- **Encrypted open handles:** `DB.Compact` cannot recover caller credentials from an open handle and returns `ErrEncryptionKeyRequired` for encrypted databases. Close it and use `CompactTo(ctx, srcPath, destPath, opts)` with the original credentials.
- **Context cancellation:** partial destination is removed on abort, and the source is unchanged. A retry starts a fresh copy.
- **Backups:** compaction creates a candidate primary, not a backup policy. Keep the source primary, WAL, and changelog together until the replacement has reopened and passed validation. Never copy an open primary/WAL pair independently.
- **Changelog:** compaction copies authoritative primary keys, including durable cursors and outbox intents, but does not copy the append-only sidecar. Registered consumers bind the candidate to the matching logical history, and `Open` fails with `ErrChangelogConsumerInvalidated` when it is missing or replaced. For plaintext replacement, retain the original sidecar at the configured path. A compacted encrypted primary has a new encryption salt, so the old encrypted sidecar cannot be reused: defer replacement when retained history is required, or open the candidate with changelog disabled, list and explicitly delete every cursor at its expected sequence, rebuild downstream state from database truth, then enable a fresh sidecar and re-register consumers. Preserve the archived source set until this validation passes.

## Super-hex derived occupancy index

`SuperHexSummaryIndex` is process-local and rebuildable; it is not stored in the primary database. Open the database with `Options.ChangelogEnabled`, construct the desired aperture-7 level, call `Rebuild` once, then call `Sync` until it returns `processed == 0`:

```go
idx, err := hexxladb.NewSuperHexSummaryIndex(2)
if err != nil {
    return err
}
if err := idx.Rebuild(ctx, db); err != nil {
    return err
}
for {
    processed, err := idx.Sync(ctx, db, 256)
    if err != nil {
        return err
    }
    if processed == 0 {
        break
    }
}
```

Rebuild after process restart, after changing hierarchy level, or after replacing/compacting the source database. An index is bound to the `*DB` used for `Rebuild`; syncing it from another handle fails rather than mixing histories.

Use `SummaryForCoord` when starting from a normal cell coordinate. Use `Summary` when the parent coordinate came from a previous `SuperHexSummary` or `Summaries` result.
Use `LastSeq` to expose consumer lag against the changelog head in application metrics.

For repeatable Dijkstra, deterministic FOV, and super-hex evidence collection,
run `task evidence`. The controlled and aggregate observation streams, privacy
constraints, output files, and decision gates are documented in
[`PERFORMANCE_EVIDENCE.md`](PERFORMANCE_EVIDENCE.md).

## Pre-release soak checklist

Use after meaningful storage/MVCC changes or before tagging a release. Capture machine type, git SHA, and wall-clock duration in your release notes.

For write-path diagnosis, sample [`DB.WriteStats`](../../write_stats.go) twice and subtract the cumulative fields. `LockWait` identifies reader/writer contention before an update starts; `Callback`, `Durability`, and `Finalization` divide the time spent holding the exclusive lock. Pair this with [`DB.GroupWALStats`](../../db.go): serialized public writes should report zero multi-job batches, while each authoritative commit still has one WAL sync. A positive `GroupWALMaxBatchWait` adds directly to public write and reader-blocking latency and is intended only for direct engine users that can enqueue jobs concurrently.

| Step | Command                    | Pass criteria                                                                                                           |
| ---- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| 1    | `task ci`                  | Exits `0`; includes unit tests + race.                                                                                  |
| 2    | `task integration`         | Exits `0`; includes `TestIntegration_MVCC_sustainedPutCellSameKey` and `TestIntegration_MVCC_latticeAndHighChurnPrune`. |
| 3    | _(Optional)_ `task stress` | Large cell load, not MVCC churn; skip on resource-constrained CI.                                                       |
| 4    | Disk growth sanity         | Note DB + WAL (+ changelog if enabled) size before/after soak; bounded growth after prune per retention policy above.   |

Tune retention and pruning for your workload and soak longer in staging if retention windows are large.

## Crash recovery drill

1. Kill the embedding process during an active [`Update`](../../tx.go) (SIGKILL).
2. [`Open`](../../db.go) the same path with the same changelog settings; verify WAL replay and durable-outbox projection succeed and [`View`](../../tx.go) reads match expectations.
3. Resume consumers from [`GetChangelogConsumerCursor`](../../changelog_consumers.go) and verify any redelivered operation is handled idempotently before compare-and-advance.
4. If [`ErrCorruptDatabase`](../../errors.go): restore from last known-good primary + WAL pair (see **Backup and copy**).

## Backup drill

1. `Close` the database (or stop the sole writer).
2. Copy primary, `{primary}-wal`, and the optional changelog together from the same instant.
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

The historical `leaf page full` variant caused by an incomplete cascading split is fixed. The B+ tree insert path now guarantees that emitted pages fit within `pageSize` and remain reachable from the root. A persisted `ErrCorruptTree` on a database written by a fixed build indicates genuine file or media corruption, not that historical split defect; restore from backup.

### 4) Changelog tail corruption

**Signal:** `ErrChangelogCorrupt` from `ReadChangelogSince`.

**Response:** Pause consumers. If durable primary outbox intent exists, `Open` automatically removes only an incomplete final frame and reprojects it. Complete frames with invalid CRC/authentication still fail closed; preserve the files, restore the paired backup or follow an application-approved repair policy, then re-bootstrap derived state from database truth. Never delete private outbox keys manually.

### 5) Commit finalization failure

**Signal:** `Update`/`Batch` returns `ErrCommitFinalization`; known durable projection failures also match `ErrCommitDurable`.

**Response:** Stop writes on that handle. Do not retry a mutation when `ErrCommitDurable` matches. Close and reopen with the same changelog configuration, then inspect authoritative state and resume the consumer from its durable cursor. If reopen still fails, correct the reported filesystem/changelog error while preserving the primary and its outbox.

### 6) Durable consumer history invalidated

**Signal:** changelog-enabled `Open` returns `ErrChangelogConsumerInvalidated`.

**Response:** Preserve the primary, WAL, and sidecar. Locate their matching backup or archived compaction set; a registered cursor must never be applied to unrelated history. If the replacement was intentional and no matching history can or should be retained, open with changelog disabled, list and compare-delete every cursor, rebuild each downstream projection from authoritative database state, then enable a fresh sidecar and re-register at zero.

## Scope boundaries

Cross-node replication, automated prune policy in-process, in-process materialized consumer workers or push delivery, and hybrid embed/service routing are out of the current spec. Track the repository's technical roadmap in [`docs/ROADMAP.md`](../ROADMAP.md).
