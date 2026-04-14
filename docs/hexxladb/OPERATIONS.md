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

Optional **AES-256-XTS** at the page layer is configured with [`Options`](../../options.go) — see **[`ENCRYPTION.md`](./ENCRYPTION.md)** for keys, passphrases, WAL ciphertext, and limitations (no integrity MAC in v1; wrong key may not fail at `Open`).

**Key rotation** (re-encrypting an existing file with a new key) is **not** automated in v1; plan maintenance windows or external tooling if required.

## Observability

The reference binary **[`cmd/hexxladb`](../../cmd/hexxladb/main.go)** uses structured logging (`log/slog`) with configurable **`LOG_LEVEL`** (see [README](../../README.md)). Long-running services should follow the same pattern: log at the composition root and adapters, not inside [`internal/domain`](../../internal/domain).

## Benchmarks and fuzzing (development)

Developers can measure hot paths and stress decoders locally:

- Benchmarks: `make bench` — see **[`BENCHMARKS.md`](./BENCHMARKS.md)** for a sample numbers table (machine-specific).
- Fuzz: `make fuzz` — short smoke only; for deeper runs, use `go test -fuzz=...` with a larger `-fuzztime` (see [CONTRIBUTING.md](../../CONTRIBUTING.md)).
