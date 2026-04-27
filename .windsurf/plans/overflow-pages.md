---
description: Overflow pages for large values — raise MaxValueBytes beyond single-page limit
---

# Overflow Pages

## Goal

Support values larger than a single B+ tree leaf entry by chaining **overflow pages**.
Raise `MaxValueBytes` ceiling from 16 KiB to at least 1 MiB. Existing databases (no overflow)
continue to work unmodified.

## Background

Currently, values are stored inline in leaf page entries. The hard limit is
`maxValueBytes` (configurable: 512..16384). Values that exceed one leaf entry's
share of the page are rejected with `ErrValueTooLarge`. This blocks real-world
use cases: multi-kilobyte code blocks, large prompts, chunked documents.

**SQLite reference:** SQLite uses overflow pages for payloads exceeding the
"local payload" threshold (`M`). Surplus bytes spill into a singly-linked chain
of overflow pages. Each overflow page stores `usableSpace - 4` bytes of payload
plus a 4-byte `nextPage` pointer. This is the proven pattern we follow.

## Design

### On-disk format

1. **Inline threshold:** `T = pageSize - btreeHeaderSize - 4 - maxKeyBytes - 8`
   (roughly the max payload that fits in a leaf alongside the key, key length,
   value length fields, and the 8-byte overflow page pointer).
   In practice, for 4 KiB pages: T ≈ 3768 bytes.
   Values ≤ T bytes are stored inline (no change to current format).

2. **Overflow indicator:** When `len(val) > T`, the leaf entry stores:
   - `keyLen(2) + key + valLen(2)` as normal (valLen = full logical length)
   - First `T` bytes of value inline
   - `uint64` overflow page ID (8 bytes) at end of entry

3. **Overflow page chain:** Each overflow page has:
   - Bytes `[0..7]`: `uint64` next page ID (0 = last page in chain)
   - Bytes `[8..pageSize-1]`: payload chunk (`pageSize - 8` usable bytes)

4. **Header changes:** New field at a free offset in the 512-byte header:
   - `overflow_enabled` flag (1 byte) — allows forward detection by old code
   - Or: use a format_version bump (v3) if cleaner

### Engine changes (internal/engine only — hex boundary)

All overflow logic is encapsulated in `internal/engine`. No domain/app changes.

**Phase 1: Overflow write path**
- `btree.go` Put: if `len(val) > inlineThreshold(pageSize)`, split into inline
  prefix + overflow chain. Allocate pages via `NextPageID`.
- New `overflow.go`: `writeOverflowChain(eng, data []byte) (firstPageID uint64, err error)`
- Update `buildLeafPage` to encode the overflow pointer in the entry.
- WAL: overflow pages are ordinary data pages — no WAL format change needed.

**Phase 2: Overflow read path**
- `btree.go` Get/scan: detect overflow pointer, read chain, reassemble value.
- New: `readOverflowChain(eng, firstPageID uint64) ([]byte, error)`
- `parseLeafPage` must detect and handle overflow entries.

**Phase 3: Overflow delete path**
- When deleting a key with overflow, free the overflow pages.
- Since the engine is extend-only (no freelist), overflow pages become dead space
  until `Compact`. This matches the existing allocator model.
- `Compact` already copies all live data — overflow chains are naturally
  compacted because only referenced pages are copied.

**Phase 4: Raise MaxValueBytes**
- Add new valid sizes: `32768, 65536, 131072, 262144, 524288, 1048576`
- Update `validMaxValueBytes` array.
- Update `Options.MaxValueBytes` documentation.
- Existing databases with smaller limits continue working; the limit is per-DB.

**Phase 5: Tests**
- Unit: overflow write/read round-trip at all page sizes.
- Unit: overflow chain spanning 1, 2, 5, 20 pages.
- Unit: delete key with overflow chain.
- Unit: Compact preserves overflow values.
- Parametric: all valid page sizes × several value sizes.
- Integration: demo with large cell content (10+ KiB).
- Backward compat: databases without overflow open normally.

**Phase 6: Documentation**
- `ENGINE_FORMAT.md`: overflow page layout.
- `ORDERED_STORE.md`: inline threshold and overflow chain.
- `API_REFERENCE.md`: updated MaxValueBytes accepted values.
- `CHANGELOG.md`, `TODOS.md`, `ROADMAP.md`.

## Constraints

- **Hex boundary:** All changes in `internal/engine`. Domain/app/adapters untouched.
- **Backward compatible:** Old databases (no overflow) work without migration.
- **No freelist:** Overflow pages are dead after delete; reclaimed by Compact.
- **Encryption:** Overflow pages encrypt like any data page (AES-256-XTS with page ID tweak).
- **WAL:** Overflow pages are normal WAL records; no format change.
- **Modern Go:** Use `errors.Is`/`errors.As`, integer range loops, `min`/`max` builtins.

## Files to create/modify

```
internal/engine/overflow.go          — NEW: writeOverflowChain, readOverflowChain, inlineThreshold
internal/engine/overflow_test.go     — NEW: overflow chain tests
internal/engine/btree.go             — Put: overflow write path
internal/engine/btree_page.go        — parseLeafPage: overflow read; buildLeafPage: overflow write
internal/engine/btree_delete.go      — delete: free overflow pages
internal/engine/max_value_bytes.go   — add larger accepted values
internal/engine/const.go             — overflow page magic/constants (if needed)
internal/engine/compact.go           — verify overflow chain copy (may be implicit)
internal/engine/ENGINE_FORMAT.md     — overflow page layout doc
internal/engine/ORDERED_STORE.md     — inline threshold + overflow
```

## Order of work

1. Phase 1 (write) + Phase 2 (read) together — must be atomic for tests
2. Phase 3 (delete)
3. Phase 4 (raise MaxValueBytes)
4. Phase 5 (tests)
5. Phase 6 (docs)
