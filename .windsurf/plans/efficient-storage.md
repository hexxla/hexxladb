---
description: Configurable page size + dynamic leaf capacity for efficient storage
branch: feat/efficient-storage
status: active
---

# Efficient Storage Plan

**Goal:** A user can store anything from a one-word prompt to a multi-KB code block
efficiently. The database should only use the space it actually needs.

**Current problems:**

- `PageSize` is a hardcoded 64 KiB constant — 82 cells = 5.6 MB (1.2% utilization)
- `maxLeafEntries = 32` is an artificial cap; most leaves hold <5 entries before
  the count triggers a split, leaving ~60 KB unused per page
- `MaxValueBytes` caps at 16384 — a user pasting a large code block can't store it
- WAL records are 1 full page each — 64 KiB per WAL record is wasteful

---

## Phase 1 — Configurable page size (engine-internal)

All changes inside `internal/engine/`. No root API changes yet.

### 1.1 Replace `const PageSize` with runtime field

- Add `pageSize int` field to `Engine` struct
- Add `PageSize uint32` to `engine.Options` — accepted values: 4096, 8192, 16384, 65536
- Default for new databases: **4096** (4 KiB)
- On open of existing DB: read `page_size` from header (already stored)
- Replace all `PageSize` constant references with `e.pageSize` or method receiver
- The `PageSize` constant becomes `DefaultPageSize = 4096` (for new DBs)
- Keep `LegacyPageSize = 65536` for documentation

### 1.2 Dynamic leaf/internal node capacity

- Remove `maxLeafEntries = 32` and `maxInternalChildren = 32` constants
- Leaf capacity: fill-based — a leaf splits when adding an entry would exceed
  `pageSize - btreeHeaderSize` bytes, not when entry count exceeds a constant
- Internal capacity: similarly derived from page size
- `buildLeafPage` / `buildInternalPage`: accept `pageSize int` parameter
- `parseLeafPage` / `parseInternalPage`: derive page size from `len(page)`

### 1.3 WAL record size follows page size

- WAL records already write exactly one page; page size change flows naturally
- Update `encodeWALRecord` / WAL read loop to use `e.pageSize` instead of constant
- WAL MAC covers `seq || page_id || payload` — payload size now varies per DB

### 1.4 Page buffer pool per size

- Current: `sync.Pool` with fixed 64 KiB buffers
- Change: pool keyed by page size, or just allocate at runtime page size

### 1.5 Tests

- Existing tests pass at 4096 (default) and 65536 (legacy)
- New tests: open DB at each supported page size, write cells, verify reads
- Benchmark: compare write/read throughput at 4K vs 64K page sizes
- Verify: existing 64 KiB test databases can still be opened (backward compat)

---

## Phase 2 — Expose in public API

### 2.1 `Options.PageSize`

- Add `PageSize uint32` to root `Options` struct
- Forward to `engine.Options.PageSize` in `db_open.go`
- Document: "Set at creation time; cannot be changed after first open"

### 2.2 `DB.PageSize() int`

- Expose the effective page size for introspection

### 2.3 Compact preserves page size

- `CompactTo` reads source header page size and propagates to dest
- Add test: compact 4K → 4K, compact 64K → 64K

### 2.4 Update demo

- Remove or reduce `MaxValueBytes` constraint in demo if unnecessary
- Show file size improvement with default 4K pages

---

## Phase 3 — Raise value size limits

### 3.1 Overflow pages for large values

- Values that exceed `pageSize - btreeHeaderSize - keyLen - 4` get split:
  inline prefix (fits in leaf) + overflow page chain
- Overflow page format: `[magic][next_page_id][payload]`
- Read: detect overflow marker in leaf, follow chain
- Write: allocate overflow pages, store inline prefix + first overflow page ID
- Delete: free overflow pages (mark for reuse or leave for compact)

### 3.2 Raise `MaxValueBytes` cap

- Current valid set: {512, 1024, 2048, 4096, 8192, 16384}
- With overflow pages: remove the cap entirely, or raise to 1 MiB
- Large code blocks, conversation histories, embedded documents all work

### 3.3 Tests

- Store a 100 KB value, read it back, verify integrity
- Store values at various sizes across page boundary
- Compact DB with overflow pages — chains preserved
- Benchmark large value read/write

---

## Phase 4 — Content compression (optional, after page size)

### 4.1 Transparent zstd compression

- `Options.Compression` enum: None, Zstd
- Compress cell values on write, decompress on read
- Applied at the record encoding layer (before B+ tree put)
- Metadata bit in record envelope to distinguish compressed vs plain

### 4.2 Tests and benchmarks

- Round-trip: write compressed, read back matches original
- Benchmark: compression ratio and throughput for typical prompts

---

## Execution order

1. Phase 1 (engine-internal page size) — biggest impact, self-contained
2. Phase 2 (public API) — expose the win
3. Phase 3 (overflow pages) — enables arbitrarily large values
4. Phase 4 (compression) — further optimization

## Completion checklist

- [ ] `engine.Options.PageSize` with validation
- [ ] `Engine.pageSize` runtime field replaces `const PageSize`
- [ ] Dynamic leaf/internal capacity (fill-based splitting)
- [ ] WAL records sized to page size
- [ ] All existing tests pass at 4096 and 65536
- [ ] `Options.PageSize` in public API
- [ ] `DB.PageSize()` introspection method
- [ ] Compact preserves page size
- [ ] Demo shows file size improvement
- [ ] `make ci` green
- [ ] Overflow pages for large values (Phase 3)
- [ ] Content compression (Phase 4)
