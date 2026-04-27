---
description: Implement Tx.DeleteCell and DB.Compact — near-term roadmap items
branch: feat/delete-compact
status: active
---

# DeleteCell + Compact Implementation Plan

Audited against bbolt Compact, CockroachDB/Pebble MVCC tombstones, and Badger delete patterns.

## Item 1 — `Tx.DeleteCell`

### Behaviour

`DeleteCell(ctx context.Context, key lattice.PackedCoord) error`

Delete a cell and **all associated data** atomically within the current transaction:

1. **Primary key** — `cell/<packed>` (v1 hard-delete) or tombstone at `cell/<packed>/<commitSeq>` (v2 MVCC)
2. **Secondary indexes** — `source/<...>/<packed>`, `time/<...>/<packed>`, `tag/<...>/<packed>` removed for current visible version (and MVCC-versioned variants)
3. **Facets** — `facet/<packed>/<facetID>` (0..5) hard-deleted (v1) or tombstoned (v2 MVCC)
4. **Edges** — all `edge/<packed>/...` where `from == key` hard-deleted (outbound only)
5. **Seams** — NOT touched. Seams reference two cells; removing one endpoint is a domain decision. Orphaned seams surface via `HealthCheck`. Documented in API_REFERENCE.

### MVCC tombstone design (format v2)

**Tombstone = zero-length value** at `cell/<packed>/<writeSeq>` (matches Pebble/CockroachDB convention).

Visibility layer changes required in `getCellVisibleRaw`:

- After `SelectVisible` returns `(value, seq, true)`, check `len(value) == 0` → treat as deleted, return `(nil, 0, false, nil)`
- Same pattern for `getFacetVisibleRaw` — zero-length value = deleted facet

**Overlay tracking** — `tx.cellOverlay` stores `record.CellRecord` which can't represent "deleted". Add `tx.cellDeleted map[lattice.PackedCoord]bool` checked in `getCellVisibleRaw` before overlay lookup. If `cellDeleted[key]` is true, return not-found immediately.

**Snapshot consistency** — `ViewAt`/`ViewAtTime` with readSeq < delete's writeSeq still see the cell (tombstone has higher seq). `ViewAt` with readSeq >= writeSeq sees not-found. This is correct MVCC behaviour.

**Pruning** — tombstones are prunable via `PruneCellVersions` like any other stale version. After pruning, the physical row is removed.

### Error semantics

- Missing cell → **return nil** (idempotent, matches `BTree.Delete` which ignores missing keys, bbolt `Delete`, Badger `Delete`)
- `ErrTxReadOnly` — called inside `DB.View`
- `ErrClosed` / `ErrDatabaseClosed` — standard guards

### Implementation steps

1. **Tombstone visibility support** (prerequisite)
   - `getCellVisibleRaw`: after `SelectVisible`, if `len(value) == 0` return `(nil, 0, false, nil)`
   - `getFacetVisibleRaw`: same zero-length check
   - Add `tx.cellDeleted` map to `Tx` struct; check in `getCellVisibleRaw` before overlay
   - Test: write cell, write tombstone at higher seq, confirm GetCell returns not-found

2. **Add `Tx.DeleteCell` to `primitives.go`**
   - Guard: `requireWritable`, `ctx.Err`
   - Read current cell via `tx.GetCell(key)` — if not found, return nil (idempotent)
   - **v1 path:**
     - `tx.deleteDirect(index.CellKey(key))` — hard-delete primary
     - `tx.removeCellSecondaryIndex(oldRec, 0)` — remove source/time/tag
     - Delete facets: collect keys via `AscendRange(FacetRangeLower..FacetRangeUpper)`, `deleteDirect` each
     - Delete outbound edges: collect keys via `AscendRange(EdgeFromPrefix(key), nil)` with prefix check, `deleteDirect` each
   - **v2 (MVCC) path:**
     - Write tombstone: `tx.putDirect(index.CellKeyWithVersion(key, tx.writeSeq), []byte{})`
     - `tx.removeCellSecondaryIndex(oldRec, oldCommitSeq)` — remove current visible secondary keys
     - Tombstone facets: for each facetID 0..5, if visible facet exists, write `tx.putDirect(index.FacetKeyWithVersion(key, facetID, tx.writeSeq), []byte{})`
     - Delete outbound edges: same as v1 (edges are not MVCC-versioned)
     - Set `tx.cellDeleted[key] = true`, delete `tx.cellOverlay[key]` if present
   - `tx.noteChangelog(changelog.OpDeleteCell, index.CellKey(key), nil)`

3. **Add `changelog.OpDeleteCell` constant** in `internal/changelog/changelog.go`

4. **Add tests in `delete_cell_test.go`**
   - Delete existing cell (v1) — primary gone, secondaries gone, facets gone, edges gone
   - Delete missing cell — no error (idempotent)
   - Delete in read-only tx — error
   - Delete with MVCC — tombstone written, `GetCell` returns false
   - MVCC snapshot: `ViewAt` before delete still sees cell; `ViewAt` after sees not-found
   - Delete cell with facets — all facets not-found after delete
   - Delete cell with outbound edges — edges removed
   - Delete then re-put at same coord — new cell visible
   - Secondary index cleanup: source/time/tag scans no longer contain deleted cell
   - HealthCheck after delete — clean report (no orphaned indexes)
   - Same-tx: put cell, delete cell, get cell → not-found (overlay correctness)

5. **Update `doc.go`, `API_REFERENCE.md`** — document method, seam/edge orphan behaviour

6. **`make ci`** — green

---

## Item 2 — `DB.Compact`

### Behaviour

`Compact(ctx context.Context, destPath string) error`

Copy-compaction: walk all B+ tree keys, write them sequentially into a fresh database file, producing a minimal-size copy. Modeled after bbolt's `Compact(dst, src *DB, txMaxSize int64)`.

### Design

- **`CompactTo(ctx, srcPath, destPath string, opts *Options) error`** — standalone function
  - Opens source via `Open(srcPath, opts)` (read-only View)
  - Opens fresh dest via `Open(destPath, opts)` with same format version, MVCC flag, MaxValueBytes, encryption
  - `AscendRange(nil, nil)` over source → `putDirect` into dest within `Update` tx
  - Context checked every 1000 keys for cancellation
  - On error or cancellation: close dest, remove temp file, return error
  - Closes both on success
- **`DB.Compact(ctx, destPath string) error`** — convenience on open DB
  - Holds read lock (View) on source, writes to destPath
  - Caller can then close source and rename dest over source if desired
- **Verbatim copy** — ALL physical keys copied as-is, including MVCC version rows and tombstones. This preserves full history. Callers who want to strip history should `PruneCellVersions` before compacting (Pebble/RocksDB approach).
- **Space reclaimed from:** freelist gaps, deleted-but-not-tombstoned keys (v1), pages with low fill factor
- **Preserves:** format version, MVCC flag, MaxValueBytes, encryption key
- Context cancellation: partial dest file cleaned up on abort

### Implementation steps

1. **Add `CompactTo` function in `compact.go`**
   - Open source, open dest with matching options
   - `source.View` → `AscendRange(nil, nil)` → batch `dest.Update` with `putDirect`
   - Check `ctx.Err()` every 1000 keys; on cancel, clean up temp file
   - Close both handles
   - Return nil on success

2. **Add `DB.Compact` convenience method**
   - `db.View` to hold read lock
   - Open fresh dest at destPath
   - Copy all keys from source btree to dest btree
   - Close dest
   - Return nil (caller manages rename/reopen)

3. **Add tests in `compact_test.go`**
   - Compact empty DB — produces valid empty DB
   - Compact DB with cells/facets/edges/seams — all data preserved and readable in dest
   - Compact MVCC DB — tombstones and version history preserved
   - Compact after PruneCellVersions — dest has fewer keys, smaller file
   - Compact encrypted DB — dest readable with same key
   - Compact respects MaxValueBytes from source header
   - Compact with context cancellation — aborts cleanly, no partial dest
   - File size: dest <= source (strictly less when source had deletes/gaps)
   - HealthCheck on dest — clean report

4. **Update `doc.go`, `API_REFERENCE.md`, `OPERATIONS.md`** — document compaction workflow

5. **`make ci`** — green

---

## Execution Order

1. `Tx.DeleteCell` first (tombstone visibility support is a prerequisite)
2. `DB.Compact` second (benefits from DeleteCell leaving reclaimable gaps)

## Completion Checklist

- [ ] Tombstone visibility in `getCellVisibleRaw` / `getFacetVisibleRaw`
- [ ] `tx.cellDeleted` overlay tracking
- [ ] `Tx.DeleteCell` implemented + tests
- [ ] `changelog.OpDeleteCell` added
- [ ] `DB.Compact` / `CompactTo` implemented + tests
- [ ] API_REFERENCE.md updated
- [ ] OPERATIONS.md updated (compaction section)
- [ ] CHANGELOG.md updated
- [ ] TODOS.md updated
- [ ] `make ci` green
- [ ] Commit + merge to main
