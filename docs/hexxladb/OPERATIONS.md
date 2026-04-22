# Operating HexxlaDB (embedded)

**Audience:** Operators and integrators embedding [`package hexxladb`](../../doc.go) via [`Open`](../../db.go) / [`Close`](../../db.go).

## Files on disk

- **Primary database** — path passed to [`Open`](../../db.go) (e.g. `/var/lib/app/data.db`).
- **Write-ahead log** — `{primary}-wal` (same directory, ASCII hyphen + `wal`). Described in [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md).

Both files matter for durability: the engine appends redo records to the WAL, then applies them to the primary. After a clean shutdown, the WAL may be truncated; after a crash, **`Open`** replays pending WAL records into the primary.

## Backup and copy

- **Preferred:** Close the database (`DB.Close`) so files are consistent, then copy **both** the primary and the WAL (if present and non-empty), or copy the directory after close.
- **Filesystem snapshots:** Snapshot the volume containing **both** files at the same logical point in time. Copying only the primary without the WAL (or mixing files from different times) can yield **corruption** or lost data.
- **Live copy** without application cooperation is not documented as safe; use vendor-specific tools or application-level export if you need hot backup.

## Encryption

Optional **AES-256-XTS** at the page layer is configured with [`Options`](../../options.go) — see **[`ENCRYPTION.md`](./ENCRYPTION.md)** for keys, passphrases, WAL ciphertext, and limitations.

Wrong key/passphrase now fails deterministically at open with **[`ErrEncryptionKeyMismatch`](../../errors.go)** once the database has an encryption verifier (new encrypted databases and upgraded legacy encrypted databases).

Use **[`RotateEncryption`](../../rotation.go)** for offline key rotation/re-encryption. It performs a logical copy into a temporary encrypted database and swaps files atomically (best-effort cleanup of old WAL/backup files).
For large databases, prefer **[`RotateEncryptionWithOptions`](../../rotation.go)** to stream rows in batches and emit progress callbacks.

## Observability

The reference binary **[`cmd/hexxladb`](../../cmd/hexxladb/main.go)** uses structured logging (`log/slog`) with configurable **`LOG_LEVEL`** (see [README](../../README.md)). Long-running services should follow the same pattern: log at the composition root and adapters, not inside [`internal/domain`](../../internal/domain).

## HEXXLA.md rollout alignment (library + ops)

Before claiming alignment with [HEXXLA.md](./HEXXLA.md) **production** expectations (not only API completeness):

1. Run the **Pre-release soak checklist** below (`make ci`, `make integration`; optional `make stress`).
2. If changefeed consumers exist: follow **[`CHANGEFEED.md`](./CHANGEFEED.md)** (idempotent handlers, lag metrics, reconciliation after **`ErrCommitFinalization`**).
3. Document **org-specific** retention (`RetainCommitsBehindHead`) and soak duration/disk notes outside this repo if required by your process.

**Evidence template:** copy tables from **[`OPERATOR_EVIDENCE.md`](./OPERATOR_EVIDENCE.md)** into your release record (SOAR, CHANGELOG appendix, internal wiki).

**Cadence:** re-run crash/backup/key-MAC/changelog drills in **OPERATIONS** incident section after major storage/MVCC/changelog releases or quarterly.

See **[`HEXXLA_LIBRARY_MAPPING.md`](./HEXXLA_LIBRARY_MAPPING.md)** (spec layers) and **[`HEXXLA_PRODUCT_WIRING.md`](./HEXXLA_PRODUCT_WIRING.md)** for what belongs in the DB library vs a future **app/adapters** service.

## MVCC lifecycle operations

For format-v2 databases (**`Options.EnableMVCC`** on new files), see **[`MVCC_RETENTION.md`](./MVCC_RETENTION.md)** for retention policy fields and **[`MVCC_TEMPORAL.md`](./MVCC_TEMPORAL.md)** for snapshot semantics.

Quick reference:

- **[`Options.MVCCRetention`](../../options.go)** — optional `RetainCommitsBehindHead` for [`SuggestedPruneBeforeSeq`](../../mvcc_lifecycle.go).
- **[`DB.StatsMVCC`](../../mvcc_lifecycle.go)** — current `CommitSeq`, logical cell count, versioned row count.
- **[`DB.PruneCellVersions`](../../mvcc_lifecycle.go)** — explicit bounded prune passes.
- **[`DB.PruneCellVersionsByProfile`](../../mvcc_lifecycle.go)** / **[`MVCCPrunePlan`](../../mvcc_lifecycle.go)** — profile defaults (`low-latency`, `balanced`, `long-history`).
- **[`PruneScheduler`](../../mvcc_lifecycle.go)** — call **`Tick`** from your own timer (no background goroutine inside the library).

Recommended cadence: during low-traffic windows, loop **`PruneScheduler.Tick`** or **`PruneCellVersions`** until a pass deletes `0` rows, then re-check **`StatsMVCC`**.

## Pre-release soak checklist (repeatable)

Use after meaningful storage/MVCC changes or before tagging a release. Capture machine type, git SHA, and wall-clock duration in your release notes.

| Step | Command / action | Pass criteria |
|------|------------------|----------------|
| 1 | **`make ci`** | Exits `0`; includes unit tests + race (`go test -race`) per repo Makefile. |
| 2 | **`make integration`** | Exits `0`; includes **`TestIntegration_MVCC_sustainedPutCellSameKey`** (MVCC churn + prune). |
| 3 | *(Optional)* **`make stress`** | See [CONTRIBUTING.md](../../CONTRIBUTING.md)—large cell load, not MVCC churn; **`TMPDIR`** defaults to repo **`./.tmp`**; skip on resource-constrained CI. |
| 4 | Disk growth sanity | Note DB + WAL (+ changelog if enabled) size before/after soak; bounded growth after prune per **[`MVCC_RETENTION.md`](./MVCC_RETENTION.md)**. |

This is **evidence**, not certification: tune retention and pruning for your workload and soak longer in staging if retention windows are large.

## Crash recovery drill (operator)

1. Kill the embedding process during an active **`Update`** (SIGKILL).
2. **`Open`** the same path; verify WAL replay succeeds and **`View`** reads match expectations.
3. If **`ErrCorruptDatabase`**: restore from last known-good **primary + WAL** pair (see **Backup and copy**).

## Backup drill

1. **`Close`** the database (or stop the sole writer).
2. Copy **primary** and **`{primary}-wal`** together from the same instant.
3. Restore on a staging host, **`Open`**, run read probes (`GetCell`, `StatsMVCC`).

## Incident response quick checklist

### 1) Encryption key mismatch

**Signal:** **`Open`** returns **`ErrEncryptionKeyMismatch`** (deterministic verifier failure).

**Response:** Confirm key derivation path (env/secrets manager); never guess keys in production. Recovery: restore from backup taken with correct key material, or offline **[`RotateEncryption`](../../rotation.go)** after establishing a readable copy from a backup.

### 2) WAL / primary corruption on open

**Signal:** **`ErrCorruptDatabase`** or WAL replay failure (wrapped engine errors).

**Response:** Restore **primary + `{primary}-wal`** from the same logical instant (see **Backup and copy**). Do not mix WAL from another run.

### 3) MVCC btree errors during prune (`engine: corrupt B+ tree page`)

**Signal:** **`PruneCellVersions`** or **`StatsMVCC`** ascent fails mid-operation.

**Response:** Stop writing; backup files; restore from known-good snapshot. Only use **`Update`** / primitives—avoid raw **`Tx.Put`** reordering **`cell/`** vs **`__meta/commit-time/`** keys on format v2 (see **[`MVCC_DESIGN.md`](./MVCC_DESIGN.md)**).

### 4) Changelog tail corruption

**Signal:** **`ErrChangelogCorrupt`** from **`ReadChangelogSince`**.

**Response:** Pause consumers; truncate/repair changelog per ops policy; re-bootstrap derived state from DB truth + **[`CHANGEFEED.md`](./CHANGEFEED.md)** reconciliation steps.

## Benchmarks and fuzzing (development)

Developers can measure hot paths and stress decoders locally:

- Benchmarks: `make bench` — see **[`BENCHMARKS.md`](./BENCHMARKS.md)** for a sample numbers table (machine-specific).
- Fuzz: `make fuzz` — short smoke only; for deeper runs, use `go test -fuzz=...` with a larger `-fuzztime` (see [CONTRIBUTING.md](../../CONTRIBUTING.md)).
