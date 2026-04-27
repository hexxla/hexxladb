---
description: Transparent per-value compression for B+ tree values (zstd)
---

# Content Compression

## Goal

Reduce database file size by transparently compressing B+ tree values on write
and decompressing on read. Compression is per-value (not per-page/block),
controlled by `Options.Compression`, and invisible to all callers.

## Background

Cell content, facets, and other values stored in HexxlaDB can be highly
compressible (markdown, code, JSON). Transparent compression reduces file
size, improves I/O throughput (fewer bytes to read/write), and obscures
plaintext in unencrypted databases. Secondary index values (empty `[]byte{}`)
are not worth compressing.

## Industry patterns

- **Pebble/RocksDB**: Block-level compression (Snappy, zstd) at SSTable layer.
- **BadgerDB**: Per-value zstd with `Options.Compression`.
- **SQLite**: No built-in; application-layer only.

We follow the **BadgerDB pattern**: per-value compression at the engine boundary.

## Design

### Compression envelope

Compressed values are wrapped in a 5-byte header:

| Offset | Size | Field                                                                       |
| ------ | ---- | --------------------------------------------------------------------------- |
| 0      | 1    | `0xFE` compression magic (distinct from overflow `0xFF`, record `'H'`=0x48) |
| 1      | 4    | **uncompressed_length** `uint32` big-endian (for pre-allocation)            |
| 5..    | var  | zstd-compressed payload                                                     |

On read: if `val[0] == 0xFE`, decompress; otherwise return raw. This check
runs **before** overflow stub detection (overflow stubs start with `0xFF`).

### Compression algorithm

**DEFLATE** via `compress/flate` (Go standard library, zero external dependencies).

- `flate.BestSpeed` (level 1) for minimal latency on small values.
- `*flate.Writer` instances pooled via `sync.Pool` (~256 KiB internal state each).
- `flate.NewReader` for decompression (stateless, cheap to create).
- Future: add `CompressionZstd` variant behind an optional dependency if needed.

### Minimum compression threshold

Values shorter than 64 bytes are stored uncompressed (compression overhead
exceeds savings). Values where compressed output >= uncompressed input are
stored raw (compression did not help).

### Integration point

Compression runs in `BTree.Put` **before** the overflow threshold check:

```text
Put(key, val)
  → compress(val) → compressedVal
  → if len(compressedVal) > inlineThreshold → overflow chain
  → store in leaf
```

Decompression runs in `BTree.GetUsingRoot` / `AscendRange` **after** overflow
reassembly:

```text
Get(key)
  → read leaf val
  → if overflow stub → readOverflowChain → full val
  → if compressed → decompress → raw val
  → return
```

### Options

```go
type CompressionType uint8

const (
    CompressionNone CompressionType = 0 // default — no compression
    CompressionZstd CompressionType = 1
)
```

- `Options.Compression CompressionType` — persisted in header.
- Opening a compressed DB without the compression option enabled: auto-detect
  from header and enable decompression. Writing new values uses the open-time
  setting.
- Mixed-mode: compressed and uncompressed values coexist. The per-value magic
  byte disambiguates.

### Header changes

| Offset | Size | Field                                         |
| ------ | ---- | --------------------------------------------- |
| 104    | 1    | **compression_type** `uint8` (0=none, 1=zstd) |

Reserved space at offset 104 (previously part of the 408-byte reserved block).

## Phases

### Phase 1: Engine compression layer

- `internal/engine/compress.go`: `compressValue`, `decompressValue`, envelope encode/decode.
- `internal/engine/compress_test.go`: unit tests.
- Pool zstd encoder/decoder in the Engine struct.

### Phase 2: Wire into BTree Put/Get/AscendRange

- `btree.go`: compress before overflow check in Put; decompress after overflow read in Get/AscendRange.
- Header: persist compression_type.

### Phase 3: Options and public API

- `engine.Options.Compression`, `hexxladb.Options.Compression`.
- `DB.Compression()` reader method.
- Auto-detect on open.

### Phase 4: Tests

- Round-trip: compress/decompress at all page sizes.
- Mixed-mode: compressed + uncompressed values coexist.
- Compression ratio: verify file shrinks for compressible data.
- Overflow + compression: large compressible value fits inline after compression.
- Overflow + compression: large incompressible value still overflows correctly.
- Backward compat: DB without compression opens normally.

### Phase 5: Docs

- ENGINE_FORMAT.md: compression envelope, header field.
- API_REFERENCE.md: Options.Compression, DB.Compression().
- CHANGELOG.md, TODOS.md, ROADMAP.md.

## Constraints

- All changes in `internal/engine` + root package Options. Domain/app untouched.
- Backward compatible: uncompressed DBs work without changes.
- No external dependencies — `compress/flate` is in the Go standard library.
- Modern Go: `errors.Is`/`errors.As`, integer range loops.

## Files to create/modify

```text
internal/engine/compress.go          — NEW: compressValue, decompressValue
internal/engine/compress_test.go     — NEW: compression tests
internal/engine/btree.go             — Put: compress; Get/AscendRange: decompress
internal/engine/engine.go            — encoder/decoder pool init
internal/engine/header.go            — compression_type field
internal/engine/const.go             — compression constants (if needed)
options.go                           — Options.Compression
db.go                                — DB.Compression()
```

## Order of work

1. Phase 1 (compress.go) + Phase 2 (btree integration) + Phase 3 (options)
2. Phase 4 (tests)
3. Phase 5 (docs)
