---
description: Implement Tx.DeleteCell and DB.Compact — near-term roadmap items
branch: feat/delete-compact
status: active
---

# DeleteCell + Compact Implementation Plan

## Item 1 — `Tx.DeleteCell`

### Behaviour

`DeleteCell(ctx context.Context, key lattice.PackedCoord) error`

Delete a cell and **all associated data** atomically within the current transaction:

1. **Primary key** — `cell/<packed>` (v1) or all `cell/<packed>/<commitSeq>` MVCC rows (v2)
2. **Secondary indexes** — `source/<...>/<packed>`, `time/<...>/<packed>`, `tag/<...>/<packed>` (and MVCC variants)
3. **Facets** — `facet/<packed>/<facetID>` (0..5) (and MVCC variants)
4. **Edges** — all `edge/<packed>/...` where `from == key` (outbound edges only — inbound edges from other cells reference this cell but are owned by their from-cell; orphan detection is a HealthCheck concern)

### MVCC behaviour (format v2)

On MVCC databases, `DeleteCell` writes a **tombstone** — a version-suffixed cell key with a zero-length value — rather than hard-deleting physical rows. This ensures `ViewAt` / `ViewAtTime` snapshots before the delete remain consistent. The tombstone is prunable via `PruneCellVersions` like any other stale version.

Secondary indexes for the current visible version are removed (they point to a now-invisible cell). The tombstone row ensures `GetCell` returns `(zero, false, nil)` for the current snapshot.

### Error semantics

- `ErrCellNotFound` — cell does not exist at the given coordinate (no-op alternative: return nil — **decision: return nil** for idempotency, matching `BTree.Delete` which ignores missing keys)
- `ErrTxReadOnly` — called inside `DB.View`
- `ErrClosed` / `ErrDatabaseClosed` — standard guards

### Implementation steps

1. **Add `Tx.DeleteCell` to `primitives.go`**
   - Guard: `requireWritable`, `ctx.Err`
   - Read current cell via `tx.GetCell(key)` — if not found, return nil (idempotent)
   - v1 path: `tx.deleteDirect(index.CellKey(key))`
   - v2 path: write tombstone `tx.putDirect(index.CellKeyWithVersion(key, tx.writeSeq), nil)`
   - Call `tx.removeCellSecondaryIndex(oldRec, commitSeq)` — existing helper handles source/time/tag
   - Delete facets: `AscendRange` over `FacetRangeLower(key)..FacetRangeUpper(key)` collecting keys, then `deleteDirect` each (v1); for MVCC use `FacetCellAllVersionsRange` and write tombstones
   - Delete outbound edges: `AscendRange` with `EdgeFromPrefix(key)` collecting keys, then `deleteDirect` each
   - Fire `tx.noteChangelog(changelog.OpDeleteCell, ...)` — new op code
   - Fire `AfterPutCell` hook? **No** — add `AfterDeleteCellHook` later if needed; keep scope minimal
   - Clear `tx.cellOverlay[key]` if present (MVCC overlay)

2. **Add `changelog.OpDeleteCell` constant** in `internal/changelog/changelog.go`

3. **Add tests in `delete_cell_test.go`**
   - Delete existing cell — primary gone, secondaries gone, facets gone, edges gone
   - Delete missing cell — no error (idempotent)
   - Delete in read-only tx — `ErrTxReadOnly`
   - Delete with MVCC — tombstone written, `GetCell` returns false, `ViewAt` before delete still sees cell
   - Delete cell with facets — all facets removed
   - Delete cell with outbound edges — all outbound edges removed
   - Delete then re-put at same coord — works correctly
   - Secondary index cleanup: source/time/tag indexes no longer contain deleted cell
   - HealthCheck after delete — clean report

4. **Update `doc.go`, `API_REFERENCE.md`** — document new method

5. **`make ci`** — green

---

## Item 2 — `DB.Compact`

### Behaviour

`Compact(ctx context.Context, destPath string) error`

Offline copy-compaction: walk all live B+ tree keys, write them sequentially into a fresh database file, producing a minimal-size copy.

### Design

- Caller must close the source DB before calling Compact (or: Compact opens a read-only view internally)
- **Simpler approach**: standalone function `CompactTo(ctx, srcPath, destPath string, opts *Options) error`
  - Opens source read-only, creates fresh dest via `Open(destPath, opts)`
  - `AscendRange(nil, nil, ...)` over source → `btree.Put` into dest
  - Closes both
  - Caller renames dest over source if desired
- Preserves: encryption settings (re-encrypts with same key), MaxValueBytes, format version
- Does NOT preserve: MVCC pruned rows (they're gone), freelist gaps (compacted away)
- Context cancellation: checked every N keys (e.g. every 1000) for early abort

### Implementation steps

1. **Add `CompactTo` function in `compact.go`**
   - Open source DB read-only (View)
   - Open dest DB with same options
   - AscendRange(nil, nil) over source btree, Put each key into dest
   - Check ctx every 1000 keys
   - Close dest, close source read-only handle
   - Return nil on success

2. **Add `DB.Compact` convenience method**
   - Creates temp file alongside DB path
   - Calls `CompactTo`
   - Atomic rename temp → original path
   - Reopens DB from compacted file

3. **Add tests in `compact_test.go`**
   - Compact empty DB — produces valid empty DB
   - Compact DB with cells/facets/edges/seams — all data preserved in dest
   - Compact MVCC DB with pruned versions — only live versions in dest
   - Compact encrypted DB — dest is also encrypted, readable with same key
   - Compact respects MaxValueBytes
   - Compact with context cancellation — aborts cleanly
   - File size: dest <= source (strictly less if source had deleted keys)

4. **Update `doc.go`, `API_REFERENCE.md`, `OPERATIONS.md`** — document compaction

5. **`make ci`** — green

---

## Execution Order

1. `Tx.DeleteCell` first — it's the more impactful API gap
2. `DB.Compact` second — benefits from DeleteCell being available (deleted keys leave gaps that Compact reclaims)

## Completion Checklist

- [ ] `Tx.DeleteCell` implemented + tests
- [ ] `changelog.OpDeleteCell` added
- [ ] `DB.Compact` / `CompactTo` implemented + tests
- [ ] API_REFERENCE.md updated
- [ ] OPERATIONS.md updated (compaction section)
- [ ] CHANGELOG.md updated
- [ ] TODOS.md updated
- [ ] `make ci` green
- [ ] Commit + merge to main
