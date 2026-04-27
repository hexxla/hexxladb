---
description: Configurable page size + dynamic leaf capacity + overflow pages for efficient storage
branch: feat/efficient-storage
status: active
---

# Efficient Storage Plan

**Goal:** Store any content efficiently — from a one-word prompt to a multi-KB code
block paste. The database should only use the space it actually needs and never
reject user content because of size limits.

**Reference designs:** SQLite (configurable page size, overflow page chains),
bbolt (contiguous overflow pages, fill-based node splitting, `minKeysPerPage=2`).

## Current state analysis

| Metric            | Current            | Target                                   |
| ----------------- | ------------------ | ---------------------------------------- |
| Page size         | 64 KiB hardcoded   | 4 KiB default, configurable 4/8/16/64    |
| Leaf entries/page | max 32 (constant)  | fill-based (fits ~55 at 4K, ~900 at 64K) |
| Max value size    | 16384 bytes        | unlimited (overflow pages)               |
| 82-cell demo size | 5.6 MB (1.2% util) | ~200–400 KB                              |
| WAL record size   | 64 KiB fixed       | matches page size                        |

**Files touched (engine-internal, 67 `PageSize` refs):**

| File                 | Refs         | What changes                                                                                                            |
| -------------------- | ------------ | ----------------------------------------------------------------------------------------------------------------------- |
| `const.go`           | 2            | `const PageSize` → `DefaultPageSize = 4096` + `LegacyPageSize = 65536`                                                  |
| `engine.go`          | 12           | `e.pageSize` field, `pageBufPool` keyed by size, `WritePage`/`ReadPage` use `e.pageSize`                                |
| `header.go`          | 8            | `readHeaderAt`/`writeHeaderAt` use page-size-aware reads; `ReadHeaderFile` bootstraps page size from header bytes 12–15 |
| `btree_page.go`      | 4            | `buildLeafPage`/`buildInternalPage` accept `pageSize int`; `parseLeafPage`/`parseInternalPage` derive from `len(page)`  |
| `btree.go`           | 0 (indirect) | `insertIntoLeaf` uses fill-based split; `insertIntoInternal` likewise                                                   |
| `btree_delete.go`    | 4 (indirect) | `minLeafKeys` and merge thresholds become dynamic                                                                       |
| `wal.go`             | 7            | `walRecordSize(pageSize)`, `encodeWALRecord` takes `pageSize`, replay loop reads record size from DB                    |
| `page_offset.go`     | 1            | `pageByteOffset` takes `pageSize` param                                                                                 |
| `max_value_bytes.go` | 0            | Phase 3: `validMaxValueBytes` expanded or removed                                                                       |
| `options.go`         | 0            | Add `PageSize uint32` field                                                                                             |

---

## Phase 1 — Configurable page size (engine-internal only)

### Step 1.1: `const PageSize` → runtime `e.pageSize`

**`const.go`:**

```go
// DefaultPageSize is the page size for newly created databases.
const DefaultPageSize = 4096

// LegacyPageSize is the page size used by databases created before
// configurable page size was introduced (format v1 and early v2).
const LegacyPageSize = 65536

// validPageSizes lists accepted page sizes (must be power of 2).
var validPageSizes = [...]uint32{4096, 8192, 16384, 65536}
```

**`options.go`:** Add `PageSize uint32` — 0 means `DefaultPageSize`.

**`engine.go`:** Add `pageSize int` field to `Engine`. Set in `Open`:

- **New file:** use `opts.PageSize` (or `DefaultPageSize` if zero)
- **Existing file:** read 4 bytes at offset 12 (big-endian `uint32`) to bootstrap
  page size before reading the full header page. Validate it's in `validPageSizes`.
  Ignore `opts.PageSize` (header wins — immutable after creation).

**Bootstrap sequence** (critical — can't read full header without knowing page size):

```
1. Open file
2. If file size == 0: new DB, use opts.PageSize or DefaultPageSize
3. If file size > 0: read bytes [12:16] → page_size uint32
   - If page_size == 0 or not in validPageSizes → check if LegacyPageSize
     (backward compat: old DBs wrote 65536 at offset 12)
4. Set e.pageSize, then read full header page at that size
```

**All 67 `PageSize` references** → `e.pageSize` (or param on functions that
don't have engine access, like `buildLeafPage`).

### Step 1.2: Fill-based leaf/internal node splitting

**Remove** `maxLeafEntries = 32` and `maxInternalChildren = 32`.

**Replace with** (bbolt pattern):

```go
const minKeysPerPage = 2  // minimum entries to allow a split
const fillPercent = 0.5   // split threshold as fraction of page size
```

**`insertIntoLeaf`** — split decision:

```
Current:  if len(keys) <= maxLeafEntries → no split
New:      if serializedSize(keys, vals) <= pageSize → no split
          (serializedSize = btreeHeaderSize + Σ(4 + len(k) + len(v)))
```

**`insertIntoLeaf` split point** — bbolt `splitIndex` pattern:

```
threshold = int(pageSize * fillPercent)
Walk entries accumulating size; split when size > threshold
and we have at least minKeysPerPage entries on each side.
```

**`buildLeafPage(pageSize, parent, next, keys, vals)`** — takes `pageSize` param,
allocates `make([]byte, pageSize)`. Validation: total serialized size must fit.

**`parseLeafPage(page)`** — derives page size from `len(page)`. Max entry count
validation becomes `n <= uint16(pageSize/6)` (minimum entry = 4 header + 1 key + 1 val).

**`btree_delete.go`** — merge thresholds:

```go
func maxLeafEntries(pageSize int) int {
    return (pageSize - btreeHeaderSize) / 6  // conservative upper bound
}
func minLeafKeys(pageSize int) int {
    return minKeysPerPage  // constant 2, matching bbolt
}
```

Merge condition: `serializedSize(left) + serializedSize(right) <= pageSize`
(instead of `len(left.keys)+len(right.keys) <= maxLeafEntries`).

### Step 1.3: WAL record size follows page size

**`wal.go`:**

```go
func walRecordSize(pageSize int) int {
    return walRecordOverhead + pageSize
}
```

**`encodeWALRecord`** — `len(payload)` already checked; change panic message
from hardcoded `PageSize` to passed `pageSize` param.

**`parseAndReplayWAL`** — needs `pageSize int` param to know record boundaries.
Bootstrap: engine reads page size from header bytes [12:16] before WAL replay.

**`group_wal.go`** — `walFlushBatch` uses `walRecordSize()` → pass `e.pageSize`.

### Step 1.4: Page buffer pool

**Current:** `sync.Pool` with `make([]byte, PageSize)`.

**New:** `e.pageBufPool` field on `Engine` initialized in `Open`:

```go
e.pageBufPool = sync.Pool{
    New: func() any {
        b := make([]byte, e.pageSize)
        return &b
    },
}
```

Move from package-level to instance-level. Buffers from one page size
must never be returned to a pool expecting another size.

### Step 1.5: Tests

- **Parametric:** Run existing `TestBTree*`, `TestWriteTxn*`, `TestWAL*` at
  page sizes {4096, 65536} using `t.Run` sub-tests.
- **Backward compat:** Create a DB at 65536 (legacy), close, reopen — verify
  `e.pageSize == 65536` and all data readable.
- **New default:** Create a DB with default options → verify `e.pageSize == 4096`.
- **Invalid page size:** `Open` with `PageSize: 3000` → error.
- **Benchmark:** `BenchmarkBTreePut` at 4K and 64K; expect 4K to be comparable
  or better for small values due to less I/O per WAL write.

---

## Phase 2 — Public API + Compact propagation

### Step 2.1: `Options.PageSize` in root package

**`options.go`** (root): Add `PageSize uint32` field.
**`db_open.go`**: Forward to `engine.Options{PageSize: opts.PageSize}`.
Doc: "Set at database creation time. Ignored when opening an existing database.
Accepted values: 4096, 8192, 16384, 65536. Default: 4096."

### Step 2.2: `DB.PageSize() int`

```go
func (db *DB) PageSize() int { return db.activeEng().PageSizeInt() }
```

### Step 2.3: Compact preserves page size

**`compact.go` — `compactDestOpts`:** Read `srcHdr.PageSize` and forward to dest.
Test: compact 4K source → 4K dest; compact 64K source → 64K dest.

### Step 2.4: Demo file size improvement

- Run demo with default 4K pages
- Print page size and file size in Phase 12 output
- Expected: 5.6 MB → ~200–400 KB

---

## Phase 3 — Overflow pages for large values

**Design:** SQLite-style linked-list overflow (simpler than bbolt contiguous).
When a value is too large to fit inline in a leaf entry, store an inline
prefix + pointer to an overflow page chain.

### Overflow page format

```
Offset  Size  Field
0       4     magic "HXOF" (overflow page identifier)
4       8     next_page_id (0 = last in chain)
12      4     payload_len (uint32, bytes of useful data in this page)
16      ...   payload (up to pageSize - 16 bytes)
```

### Inline threshold

```go
// maxInlineValue returns the largest value that fits inline in a leaf entry.
// Leaf entry overhead: 2 (keyLen) + 2 (valLen) + keyLen bytes.
// Reserve space for at least minKeysPerPage entries.
func maxInlineValue(pageSize, keyLen int) int {
    usable := pageSize - btreeHeaderSize
    perEntry := 4 + keyLen  // header + key (minimum, no value)
    reserved := perEntry * minKeysPerPage
    return usable - reserved - 4 - keyLen  // space for one more entry's key+header
}
```

When `len(val) > maxInlineValue`:

1. Store first N bytes inline (where N = `maxInlineValue - 8` to make room
   for an 8-byte overflow page pointer appended after inline prefix)
2. Write remaining bytes across overflow pages
3. Leaf entry format: `[keyLen][valLen=N+8][key][inline_prefix][overflow_page_id]`
4. Detect overflow on read: if `valLen > maxInlineValue`, last 8 bytes of
   value are the overflow page ID

### Read path

```go
func (t *BTree) resolveOverflow(inlineVal []byte) ([]byte, error) {
    if no overflow marker → return inlineVal
    overflowID := last 8 bytes of inlineVal
    prefix := inlineVal[:len(inlineVal)-8]
    var full []byte
    full = append(full, prefix...)
    for pageID := overflowID; pageID != 0; {
        page := readPage(pageID)
        parse overflow header → next, payloadLen, payload
        full = append(full, payload[:payloadLen]...)
        pageID = next
    }
    return full, nil
}
```

### Write path

- `BTree.Put` detects oversized value, calls `writeOverflow`
- `writeOverflow` allocates pages via `allocPageID`, chains them
- Stores inline prefix + first overflow page ID as the leaf value

### Delete / Compact

- `BTree.Delete` on overflow key: read inline value to find overflow chain,
  mark overflow pages as free (or leave for compact to skip)
- `CompactTo` iterates all keys; for overflow values, the inline prefix +
  pointer are copied verbatim; overflow pages are also copied by the
  full-key iteration (they appear as regular pages in the file)
  **Actually:** overflow pages are NOT in the B+ tree — they're separate.
  Compact must detect overflow entries and copy their chains explicitly.

### `MaxValueBytes` changes

- Remove the `validMaxValueBytes` whitelist
- Set default `MaxValueBytes = 0` meaning unlimited (or a generous 10 MiB)
- Keep the header field for backward compat; 0 means unlimited
- Old databases with explicit limits: honored as before

### Tests

- Write 1 KB, 10 KB, 100 KB, 1 MB values — all round-trip correctly
- Write at exact page boundary (pageSize - overhead)
- Delete overflow value — chain pages freed
- Compact DB with overflow values — all data preserved
- HealthCheck on DB with overflow values — clean
- Encrypted DB with overflow pages — works
- Benchmark: large value read/write latency

---

## Phase 4 — Content compression (deferred, separate branch)

Transparent zstd compression of cell record values. Applied at the
`record.AppendEnvelope` / `record.DecodeEnvelope` layer, not at the
engine level. A format bit in the envelope header distinguishes
compressed vs plain. Deferred to a separate branch after Phases 1–3 ship.

---

## Execution order

1. **Phase 1** — engine-internal page size (all changes in `internal/engine/`)
2. **Phase 2** — public API, compact, demo (root package + docs)
3. **Phase 3** — overflow pages (engine + root package)
4. Phase 4 — compression (separate branch, deferred)

## File change map

| Phase | Files created | Files modified                                                                                                                                                              |
| ----- | ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | (none)        | `const.go`, `options.go`, `engine.go`, `header.go`, `btree_page.go`, `btree.go`, `btree_delete.go`, `wal.go`, `page_offset.go`, `group_wal.go`, `*_test.go` (13 test files) |
| 2     | (none)        | root `options.go`, `db_open.go`, `db.go`, `compact.go`, `compact_test.go`, `doc.go`, demo `main.go`, `API_REFERENCE.md`, `OPERATIONS.md`, `ENGINE_FORMAT.md`                |
| 3     | `overflow.go` | `btree_page.go`, `btree.go`, `btree_delete.go`, `compact.go`, root `options.go`, `max_value_bytes.go`, `ENGINE_FORMAT.md`, `ORDERED_STORE.md`                               |

## Completion checklist

- [ ] `engine.Options.PageSize` with validation (4096/8192/16384/65536)
- [ ] `Engine.pageSize` runtime field; bootstrap from header bytes [12:16]
- [ ] All 67 `PageSize` refs → `e.pageSize` or param
- [ ] Fill-based leaf splitting (bbolt `splitIndex` pattern)
- [ ] Fill-based internal node splitting
- [ ] Fill-based merge thresholds in `btree_delete.go`
- [ ] WAL record size parameterized by page size
- [ ] Instance-level page buffer pool
- [ ] Parametric tests at 4096 and 65536
- [ ] Backward compat: open legacy 65536 DB
- [ ] Public API: `Options.PageSize`, `DB.PageSize()`
- [ ] Compact preserves page size
- [ ] Demo shows file size improvement
- [ ] Overflow page format and read/write
- [ ] `MaxValueBytes` cap removed
- [ ] Overflow chain handled in Compact
- [ ] `make ci` green
- [ ] Docs: `ENGINE_FORMAT.md`, `ORDERED_STORE.md`, `API_REFERENCE.md`, `CHANGELOG.md`
