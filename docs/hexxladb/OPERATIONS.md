# Operating HexxlaDB (embedded)

**Audience:** Operators and integrators embedding [`package hexxladb`](../../doc.go) via [`Open`](../../db.go) / [`Close`](../../db.go).

## Files on disk

- **Primary database** — path passed to [`Open`](../../db.go) (e.g. `/var/lib/app/data.db`).
- **Write-ahead log** — `{primary}-wal` (same directory). Described in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).
- **Changelog** (optional) — separate append-only provenance log when [`Options.ChangelogEnabled`](../../options.go) is set; see [`CHANGEFEED.md`](./CHANGEFEED.md).

Both primary and WAL matter for durability: the engine appends redo records to the WAL, then applies them to the primary. After a clean shutdown the WAL may be truncated; after a crash, [`Open`](../../db.go) replays pending WAL records into the primary.

### File growth, reuse, and tail reclaim

Authenticated format v3 transactionally records whole B+ tree and overflow
pages that become unreachable. Later write transactions reuse the lowest free
page ids before extending the primary. The allocator metadata is authenticated
and committed through the same WAL header marker as the tree mutation; aborts
publish neither side. Plaintext and legacy formats retain extend-only
allocation.

Reuse prevents steady-state growth but does not shrink the file by itself.
[`DB.ReclaimTail`](../../reclaim.go) explicitly removes a contiguous suffix made
entirely of reusable or allocator-metadata pages. It first durably lowers the
allocator boundary, then truncates and syncs the primary. Interruption between
those steps can leave harmless excess bytes; the next call reconciles them.
Non-tail holes are never punched. Use [`DB.Compact`](../../compact.go) or
[`CompactTo`](../../compact.go) to repack low-fill pages and fragmented layouts.
See [`ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md) for the exact
page, freelist, and WAL layout.

[`DB.StorageStats`](../../storage_stats.go) provides one consistent measurement while holding the database read lock:

- **`PrimaryBytes`, `WALBytes`, `ChangelogBytes`** are current physical file lengths. `ChangelogBytes` is zero when the sidecar is disabled.
- **`AllocatedPages` and `ReachablePages`** include the header page. Reachability walks the current B+ tree, every referenced overflow chain, and authenticated allocator metadata.
- **`AllocatorPages`** counts external freelist metadata pages; **`ReusablePages`** counts transactionally reusable data-page slots.
- **`LiveBytes`** is the physical page-rounded size of the header, tree, overflow, and allocator pages. It includes unused capacity inside reachable pages; it is not logical payload size.
- **`ReclaimableBytes`** is exact whole-page space outside the tree and allocator metadata, including `ReusablePages`. Compaction may recover more by repacking low-fill reachable pages.

The walk is proportional to reachable pages and blocks writers, so sample it during a maintenance or low-traffic window rather than on every request.

### Deletes, tombstones, and why the file size barely moves

On **MVCC formats v2 and v3**, [`DeleteCell`](../../delete_cell.go) does **not** remove the cell’s primary history: it appends a **tombstone** row (zero-length value at a new `commit_seq`). That **adds** a physical btree entry and usually **grows** WAL and sometimes the primary (new pages or split pages), even while the **visible** cell count drops.

So a pattern like “82 cells → delete 10 → file still **576 KiB**” is **normal**: obsolete pages remain allocated until compaction. To **reduce bytes on disk**:

1. Optionally [**`PruneCellVersions`**](../../mvcc_lifecycle.go) to drop versions superseded by the cutoff while preserving the state visible at that cutoff.
2. On authenticated v3, call [**`ReclaimTail`**](../../reclaim.go) for cheap contiguous-tail truncation when `ReusablePages` is non-zero.
3. Use [**`Compact`**](../../compact.go) when you need to rewrite into a tight file and reclaim fragmented or low-fill pages.

Without **prune + compact**, expect similar **file** size; [`StatsMVCC`](../../mvcc_lifecycle.go).`VersionedRows` typically **increases** after deletes (one new row per successful delete).

### `StatsMVCC` / `logical_cells` vs visible cell count

[`DB.StatsMVCC`](../../mvcc_lifecycle.go) counts **physical** `cell/<coord><seq>` rows and **distinct coords** that still have **any** stored version (including the latest **tombstone**):

- **`VersionedRows`** — total versioned primary rows (grows with puts **and** deletes).
- **`LogicalCells`** — number of distinct coordinates that still have at least one version row. A deleted coordinate remains counted because its latest tombstone is retained indefinitely by the current pruning contract, unless a later cell value supersedes it.

**Visible** cells (non-tombstone latest value) are what [`DB.HealthCheck`](../../health.go) **CellCount** reflects, or what you get from [`GetCell`](../../cells.go) / query APIs. Do not equate **`logical_cells`** with “rows the user can see.”

`HealthCheck` always decodes and validates every visible cell and seam primary
record and returns `ErrCorruptDatabase` for malformed keys, encodings, or
key/record mismatches. `HealthCheckConfig{}` disables only the optional orphan
and secondary-index consistency checks; use `DefaultHealthCheckConfig()` for a
full check. The scan holds a stable read snapshot, so other readers may proceed
but writers wait until it finishes. Run it during a maintenance or low-traffic
window and use a cancellable context for large databases.

Deleting a coordinate with no visible cell is idempotent. Use
[DeleteCellWithOutcome](../../delete_cell.go) to distinguish a new tombstone
from a retry or an already-empty coordinate.

## Terminal explorer

Run `task seed` once to create the synthetic `.tmp/tui-sample/fixture.db`,
then use `task tui`. To create another fixture, set `TUI_FIXTURE_DIR` to a
new directory under an existing parent. The generator refuses an existing
target directory.

Use `task tui TUI_DB=/path/to/existing.db`, or pass `-path` to the
`hexxladb-tui` binary. The binary also accepts `HEXXLA_DB_PATH` as a
fallback. The explorer requires an existing primary and takes the exclusive
database lock. Opening can replay WAL changes and create sidecars, so close
the owning application before opening its database in the explorer.

For encrypted databases, supply either `HEXXLA_PASSPHRASE` or a
standard-base64 `HEXXLA_ENCRYPTION_KEY`, matching the credential used to open
the database in the application. The CLI commands `info`, `check`, and
`stats` use the same credential variables.

Use `1`–`5` or Tab to switch views and `?` for contextual shortcuts.
The footer shows the main actions for the active view.

- **Cells** lists up to 200 visible cells. The `›` next to a coordinate
  marks the selected row. Use `/` for lexical search and Enter to inspect
  a row. In narrow windows, the Inspector shows the selected cell's tags.
  Browsing and lexical search report partial results at the 20,000-row scan limit. Press `e` to cycle search modes.
- **Hex Grid** shows occupancy and per-ring density. Use the arrow keys to
  move, `+` or `-` to zoom, `v` to toggle the visibility overlay, and
  Enter to inspect the center.
- **Inspector** shows typed cell details, including facets and cluster hints.
  Use `c` for a radial context pack or `d` for cell details. Scroll with
  the arrow keys or Page Up and Page Down. Esc returns to the originating view.
- **Seams** lists visible seams. **Diff** shows retained MVCC cell and seam
  history over the last ten commit sequences. The diff lists cell changes and
  summarizes the number of seam changes.

Semantic and hybrid search send the query text to local Ollama at
`localhost:11434` using `all-minilm`. These modes require embeddings from a
compatible model and inspect up to 2,000 embedding candidates. Use
`hexxladb check` for the visible cell total and integrity checks,
`hexxladb stats` for retained MVCC rows, and `hexxladb info` for file metadata.

## Backup and copy

HexxlaDB permits exactly one open handle per primary database file, across handles and processes. A competing [`Open`](../../db.go) fails immediately with [`ErrDatabaseLocked`](../../errors.go); do not bypass this lock or independently manipulate the WAL. Close the owning handle before using offline tools such as [`CompactTo`](../../compact.go) or encryption rotation.

## Remote access boundary

The database remains embedded: it does not listen on a network or provide
transport authentication, authorization, tenancy, or request admission. When
remote access is required, run one application process that exclusively owns
the database handle and expose application-specific operations through that
process. Remote clients must never open or share the primary, WAL, or changelog
files.

[`examples/remote_access`](../../examples/remote_access) validates this pattern
with a small standard-library HTTP owner service. It defaults to and enforces an
explicit loopback listener, requires a long bearer token and authenticated-v3
encryption key, strictly bounds requests, applies admission and rate limits,
and configures server timeouts and graceful shutdown. Its HTTP boundary test
proves cell put/get through the owner while a competing database owner still
gets `ErrDatabaseLocked`.

The example is not a production server. Terminate TLS at a trusted local proxy
and supply product-specific identity, authorization, tenancy, audit,
observability, secret rotation, and workload-derived limits. Treat the host,
owner process, and proxy as one trust boundary. A stolen bearer token grants the
example's complete API authority, its rate limit is global and process-local,
and an owner restart is required to replace credentials. The owner remains an
availability and serialized-write boundary; replication, consensus, and
automatic failover are separate product decisions. See the example's README
for its exact threat model and run instructions.

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

## Offline conversion of retired files

The current library opens plaintext engine v2 and authenticated engine v3. A
single-version v1 primary or legacy AES-XTS primary returns
`ErrUnsupportedFormatVersion`. An unfinished offline conversion candidate
returns `ErrMigrationIncomplete`.

Preserve a verified copy of the primary, WAL, and optional changelog before
conversion. Use the matching released HexxlaDB binary to recover any pending
WAL on a copy. The [v0.6.0 conversion runbook](https://github.com/hexxla/hexxladb/blob/v0.6.0/docs/hexxladb/OPERATIONS.md#format-v1-to-v2-migration)
documents its `migrate-v1-to-v2` and `migrate-to-authenticated` commands. Use
the former with a plaintext v1 source and plaintext destination, or the latter
to convert a v1/v2 source to authenticated v3.

The pinned converter does not carry changelog history or consumer cursors into
the destination. If it reports existing changelog state, reconcile or archive
that state and explicitly authorize a reset before converting. Verify the
candidate with the current library and retain the original recovery set until
the candidate and its backup have passed a restore drill.

## Encryption

Official encryption on a new database creates authenticated engine format v3
with XChaCha20-Poly1305 data pages, an authenticated header, keyed WAL, and—when
enabled—authenticated encrypted changelog format v2. See
[`ENCRYPTION.md`](./ENCRYPTION.md) for the exact threat model, physical overhead,
and residual same-slot/whole-set rollback limits.

Wrong key/passphrase fails deterministically at open with [`ErrEncryptionKeyMismatch`](../../errors.go).

Use [`RotateEncryption`](../../rotation.go) for offline key rotation/re-encryption. For large databases, prefer [`RotateEncryptionWithOptions`](../../rotation.go) to stream rows in batches and emit progress callbacks. If the changelog is enabled, it must remain enabled at the same effective path in both option sets; rotation preserves its logical records and re-encrypts its frames.

If a host/process interruption leaves `ErrRotationIncomplete`, keep the database
offline and call `RecoverInterruptedRotation(path, originalOptions)`. Only the
original changelog enablement/path is inspected; key material is not required.
Recovery rolls the uncommitted swap back to the old primary and changelog;
reopen with the old credentials, verify, and retry rotation. If rotation returns
`ErrRotationCleanup`, the new files are already committed: verify with the new
credentials and securely remove any reported `.rotate.bak` artifact.

## Observability

The [`examples/remote_access`](../../examples/remote_access) owner-service boundary and [`cmd/tui`](../../cmd/tui) use structured `log/slog` logging at their composition roots. Long-running services should follow that pattern and own their log-level configuration; the `hexxladb` inspection CLI prints directly to stdout/stderr. Keep logging at the application and operator boundaries.

## MVCC retention and pruning

For both storage profiles:

- [`Options.MVCCRetention.RetainCommitsBehindHead`](../../options.go) suggests
  a cutoff of `max(0, CommitSeq - RetainCommitsBehindHead)`. A zero policy
  disables suggestions. A suggested cutoff of zero leaves all versions in place.
- [`PruneCellVersions`](../../mvcc_lifecycle.go) keeps the version or
  tombstone visible at the cutoff and removes older superseded versions in
  bounded passes. It clamps the cutoff to the current head.
- [`OldestSnapshotSeq`](../../mvcc_snapshot_floor.go) reports the earliest
  guaranteed snapshot. Older sequence, time, and tagged reads return
  `ErrSnapshotExpired`. Tags expire with history.
- Retention uses commit sequences. Use your observed commits per interval to
  translate a time-based requirement into a suggested cutoff.

The CLI exposes the same operations. Choose the cutoff from your retention
policy and inspect the reported boundary before doing more maintenance.

```sh
hexxladb stats memory.db
hexxladb prune --before-seq 4000 --max-delete 2048 memory.db
hexxladb reclaim-tail memory.db
hexxladb compact --dry-run -o memory.compacted.db memory.db
```

Prune runs one bounded pass. On authenticated v3, tail reclaim returns only
a contiguous free suffix to the filesystem. Use the compaction workflow below
to repack a fragmented database. These commands take the exclusive file lock
and accept the same encryption environment variables as `stats`.

Quick API reference:

| API                                                                                                     | Purpose                                                                                 |
| ------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| [`DB.StatsMVCC`](../../mvcc_lifecycle.go)                                                               | `CommitSeq`, logical cell count, versioned row count                                    |
| [`DB.StorageStats`](../../storage_stats.go)                                                             | Primary/WAL/changelog sizes and persistent live/reclaimable page bytes                  |
| [`DB.ReclaimTail`](../../reclaim.go)                                                                    | Safely truncate a contiguous authenticated-freelist suffix                              |
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
| [`PreflightCompactTo`](../../maintenance_preflight.go)                | Inspect source storage, destination collisions, and conservative free-space needs. |
| [`DB.CompactWithOptions`](../../compact.go)                           | Open-handle compaction with a bounded batch size and durable progress checkpoints. |
| [`CompactToWithOptions`](../../compact.go)                            | Offline equivalent with credentials supplied through `Options`.                   |

### Typical workflow

1. Call `PreflightCompactTo` for a closed source, or record `before, err := db.StorageStats()` for an open handle. Review `ReclaimableBytes` and the conservative destination-space requirement.
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
    VerifyDestination: true,
    OnProgress: func(p hexxladb.CompactProgress) {
        copied = p.CopiedKeys
    },
})
```

`BatchSize` is zero for the bounded default (4096 keys) or 1–4096.
`VerifyDestination` runs the full health check before the API publishes a
successful return. Cancel through `ctx`. A cancelled, failed, or unhealthy copy
removes the partial destination primary and WAL; retry with the same destination
path restarts safely from the stable source. Compaction does not persist a
byte-offset resume cursor.

The destination's first durable header carries an incomplete-compaction feature
bit. The current library refuses an artifact left by process or host interruption
with `ErrCompactionIncomplete`, preventing a partial key copy from being mistaken
for a valid candidate. After confirming no compaction process is active, remove
that destination primary and WAL and retry from the preserved source; compaction
is intentionally restart-only, not resumable. The bit is cleared durably only
after the copy, source commit-sequence publication, and optional health check.

Use the CLI for a closed database:

```bash
hexxladb compact --dry-run -o memory.compacted.db memory.db
hexxladb compact --batch-size 512 -o memory.compacted.db memory.db
```

The CLI preflights, prints durable progress, enables destination verification,
then closes and reopens the candidate for a second health/storage check. It
reads a passphrase from `HEXXLA_PASSPHRASE` or a standard-base64 key from
`HEXXLA_ENCRYPTION_KEY`, never from the command line. It leaves replacement and
rollback-set retention to the operator.

### Stripping old MVCC versions

`Compact` copies tombstones and old versions verbatim. To reclaim that space, prune first:

```go
db.PruneCellVersions(beforeSeq, 100_000) // remove stale versions
db.Compact(ctx, destPath)                // rewrite without dead pages
```

### Repairing MVCC secondary indexes

Older writers could remove a deleted cell's historical tag, source, and week
entries or leave stale entries after an edit in the same commit. To rebuild
those indexes for the history still present in an MVCC database, close the
owning process, keep a backup of its recovery set, and run:

```bash
hexxladb compact --rebuild-mvcc-indexes -o memory.repaired.db memory.db
```

The command uses [`RepairMVCCIndexesTo`](../../compact.go) to rebuild cell and
seam secondary indexes from retained primary versions in bounded batches.
It verifies the candidate before publishing it and preserves commit sequences,
tombstones, and seams whose endpoints were deleted. Check an affected
historical query on the candidate, then use
the replacement and changelog rules below. Keep the original as the rollback
copy. Versions already removed by pruning require a backup that retains them.
The ordinary compaction preflight estimates copy capacity. Allow additional
space for rebuilt index entries, especially when historical rows are missing.

### Notes

- **Format preservation:** destination inherits format version, MVCC flag, `MaxValueBytes`, and encryption from the source header. Encryption credentials must be supplied via `opts` for encrypted sources.
- **Exclusive paths:** `destPath` must not exist and is never overwritten. `CompactTo` also requires `srcPath` to be closed because it acquires the database lock itself.
- **Encrypted open handles:** `DB.Compact` cannot recover caller credentials from an open handle and returns `ErrEncryptionKeyRequired` for encrypted databases. Close it and use `CompactTo(ctx, srcPath, destPath, opts)` with the original credentials.
- **Context cancellation:** partial destination is removed on abort, and the source is unchanged. A retry starts a fresh copy.
- **Backups:** compaction creates a candidate primary, not a backup policy. Keep the source primary, WAL, and changelog together until the replacement has reopened and passed validation. Never copy an open primary/WAL pair independently.
- **Changelog:** compaction copies authoritative primary keys, including durable cursors and outbox intents, but does not copy the append-only sidecar. Registered consumers bind the candidate to the matching logical history, and `Open` fails with `ErrChangelogConsumerInvalidated` when it is missing or replaced. For plaintext replacement, retain the original sidecar at the configured path. A compacted encrypted primary has a new encryption salt, so the old encrypted sidecar cannot be reused: defer replacement when retained history is required, or open the candidate with changelog disabled, list and explicitly delete every cursor at its expected sequence, rebuild downstream state from database truth, then enable a fresh sidecar and re-register consumers. Preserve the archived source set until this validation passes.

## Deferred embedding ingestion and HNSW rebuild

Ordinary `PutEmbedding` updates a current HNSW graph in the same commit. For a bounded
bulk load, write vectors with `PutEmbeddingWithOptions` and
`DeferIndexMaintenance: true`, then publish a replacement graph:

```go
if err := db.Update(func(tx *hexxladb.Tx) error {
    for _, item := range batch {
        if err := tx.PutEmbeddingWithOptions(
            item.Coord,
            item.Vector,
            hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: true},
        ); err != nil {
            return err
        }
    }
    return nil
}); err != nil {
    return err
}

stats, err := db.RebuildEmbeddingIndex(ctx, &hexxladb.EmbeddingIndexRebuildOptions{
    MaxVectors:     10_000,
    MaxMemoryBytes: 2 << 30,
})
```

The first deferred write marks the persisted graph stale; queries remain
correct through exact flat search, as reported by `SearchByEmbeddingWithStats`.
Rebuild preflight counts vectors, estimates peak heap/transient WAL, and checks
free space before advancing the stale revision. It then snapshots authoritative
vectors, builds outside the write transaction, verifies layer-zero
reachability from the entry point, and publishes the graph in one commit. Normal readers and writers may run
during graph construction. Any embedding mutation advances the revision and
causes publication to return `ErrEmbeddingIndexChanged`; retry from the
preflight. If the build leaves unreachable vectors, it returns
`ErrEmbeddingIndexDisconnected` and exact search stays active. Failed or
cancelled rebuilds preserve the prior graph records. Rebuild an existing
index to apply the new neighbor-selection policy across the graph.

Zero options use a 10,000-vector and 2 GiB estimate ceiling. The hard supported
rebuild limit is 20,000 vectors, based on the retained 32-dimensional evidence;
the 384-dimensional retained tier is 10,000. The 20k×32d reference build peaked
near 964 MB even though steady heap returned to about 29 MB, so do not infer a
memory ceiling from post-build heap. Ensure the workload can tolerate exact
scan latency for the complete stale interval, and use a cancellable context.
Rebuild creates no sidecar or temporary database, but its atomic publication
uses transient WAL and can expose reclaimable primary pages; inspect
`StorageStats` and compact explicitly when maintenance policy requires it.

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

The repository's conservative reference qualification is runnable as
`task soak-pilot`. It defaults to five measured minutes, 10,000 cells with one
32-dimensional vector each,
20 operations per second, 95% reads and 5% serialized writes, authenticated v3
primary encryption, an authenticated encrypted changelog, one durable consumer,
4 KiB pages, and a 64 MiB page cache. The read mix covers point reads, bounded
FOV, HNSW search, and bounded tag scans. It performs and validates an encrypted
online backup plus a primary reopen. Runs longer than the 15-minute backup
interval also repeat the backup/restore drill during the measured workload.
Deterministic seeding precedes the measured window and is reported separately.

The reference gates are zero operation or health errors, at least 95% of target
throughput, at least 5,000 total operations, and minimum samples of 1,000 point
reads, 250 FOV reads, 300 vector searches, 200 tag scans, 50 ordinary cell
writes, and 10 vector updates. Their p95 latency must remain at or below 5 ms
for point reads, 10 ms for FOV, 25 ms
for vector search and ordinary cell writes, 50 ms for bounded tag scans, and
2 seconds for the distinct HNSW-maintaining vector-update class. One in every
ten writes updates a vector; keeping that distribution separate prevents rare
graph maintenance from hiding an ordinary-write regression. Heap in-use is
capped at 1 GiB; combined primary, WAL, and changelog storage is capped at
2 GiB. One temporary backup recovery set exists only during each restore drill
and is removed immediately afterward; every restore must finish within 30
minutes. The 15-minute backup interval is the declared RPO. The runner stops
on a correctness or resource error,
uses no existing database, emits aggregate-only JSON to
`.tmp/evidence/pilot-soak.json`, and removes its exact run directory on exit.
If a process is killed without running its exit trap, the next invocation
refuses to reuse the stale directory instead of adding another workload.

Run the reference qualification from a clean release commit:

```sh
task soak-pilot
```

Retain the JSON report with the exact release commit, machine/storage
description, and named owner. The minimum operation samples, deterministic
storage churn tests, vector-scale churn evidence, integration suite, and
recovery drills are the qualification boundary; elapsed time alone is not a
release gate. Deployments with different profiles should record their own
declared limits and evidence.

For write-path diagnosis, sample [`DB.WriteStats`](../../write_stats.go) twice and subtract the cumulative fields. `LockWait` identifies reader/writer contention before an update starts; `Callback`, `Durability`, and `Finalization` divide the time spent holding the exclusive lock. Pair this with [`DB.WALStats`](../../db.go) to observe WAL commit attempts and redo syncs.

| Step | Command                    | Pass criteria                                                                                                           |
| ---- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| 1    | `task ci`                  | Exits `0`; includes unit tests + race.                                                                                  |
| 2    | `task integration`         | Exits `0`; includes `TestIntegration_MVCC_sustainedPutCellSameKey` and `TestIntegration_MVCC_latticeAndHighChurnPrune`. |
| 3    | _(Optional)_ `task stress` | Large cell load, not MVCC churn; skip on resource-constrained CI.                                                       |
| 4    | `task soak-pilot`          | Aggregate report passes every sample, latency, throughput, backup/restore, health, heap, and storage gate.              |

Tune retention and pruning for your workload and soak longer in staging if retention windows are large.

## Release rehearsal and publication

The tag workflow validates the tagged commit before it can publish. It requires
the tag to be reachable from `main`, runs the complete CI and integration suites
plus decoder fuzz smoke tests, validates `.goreleaser.yml`, and packages the
CLI and TUI for Linux, macOS, and Windows on amd64 and arm64. Publication then
creates SHA-256 checksums, a detached GPG signature for the checksum file, and
an SPDX JSON SBOM for every release archive.

Configure the GitHub `release` environment before creating a tag:

1. require an approving reviewer and disallow administrator bypass;
2. store an armored signing key as `GPG_PRIVATE_KEY` and its passphrase as
   `GPG_PASSPHRASE` in environment secrets;
3. retain the public key outside GitHub and publish its fingerprint through an
   independently controlled channel;
4. permit the workflow's release job to write repository contents only after
   the validation job and environment approval succeed.

Run the Release workflow manually on a candidate reachable from `main` to
produce archives, checksums, and SBOMs without publishing. To rehearse signing
locally with the trusted release key, install the pinned GoReleaser and Syft
versions from the workflow and set `GPG_FINGERPRINT` in your shell:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

After the candidate evidence is reviewed, create and push an annotated tag.
Do not move or reuse a published tag. Download the resulting archives on a
clean Linux/amd64 staging host, verify the checksum signature and archive
checksum and extract both binaries. Run `hexxladb check` against the restored
copy. For an encrypted backup, supply the matching environment credential.
Record the workflow run, signing-key fingerprint, installation result,
backup/restore result, and upgrade/refusal drill with the release evidence.

Rollback means deploying the previously verified application build against a
recovery-set copy whose format it supports. Never open the sole upgraded data
copy with an older library. First prove the older build either opens a staging
copy safely or refuses it as documented in `VERSIONING.md`; restore the paired
primary, WAL, and changelog backup when the formats are not backward-readable.

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

**Response:** Stop writing; backup files; restore from known-good snapshot. Only use `Update` / primitives — avoid raw `Tx.Put` reordering `cell/` vs `__meta/commit-time/` keys on MVCC formats v2 and v3.

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
