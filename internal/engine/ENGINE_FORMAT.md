# Engine on-disk format

HexxlaDB uses a **configurable logical page size** (4/8/16/64 KiB; default **4 KiB**), one **primary database file**, and one **WAL file**. Plaintext format v2 and encrypted format v3 share MVCC commit sequencing. V3 adds authenticated headers and data-page envelopes. Numeric fields are **big-endian** unless noted. Bump **`format_version`** or the relevant magic for an incompatible layout change.

## Paths

- **Primary:** the path passed to `Open` (e.g. `dir/my.db`).
- **WAL:** `{primary}-wal` (ASCII hyphen + `wal`).
- The public database opener coordinates file-set replacement through a stable
  `{primary}.lock` inode. It is recreated at restored paths and excluded from
  backup data.

## Page size

- **Configurable per-database.** Accepted values: **4096**, **8192**, **16384**, **65536** bytes.
- Set via `Options.PageSize` at creation; persisted in the file header at offset 12 (`uint32`).
- **Default:** 4096 (4 KiB) for new databases. Supported v2 and v3 databases retain the page size recorded at creation.
- Readable at runtime via `Engine.PageSizeInt()` / `DB.PageSize()`.

## Database file layout

- **Page 0** is the **meta/header page**: the first **512 bytes** hold a fixed **file header**; the remainder of page 0 is reserved (zeros).
- **Page IDs:** logical **`uint64`**. **Page 0** is header-only. **Pages with id ≥ 1** are data pages.
- **Plaintext format v2:** physical data-page size equals `PageSize`; page `p` starts at `p * PageSize`.
- **Authenticated format v3:** each data page has a 48-byte envelope, so `PhysicalPageSize = PageSize + 48`. Page 0 remains exactly `PageSize`; data page `p ≥ 1` starts at `PageSize + (p-1) * PhysicalPageSize`.
- **Allocation:** authenticated v3 reuses transactionally persisted free pages before extension; v2 remains extend-only. Explicit compaction still repacks fragmented and partially filled pages.

## File header (first 512 bytes of page 0)

| Offset | Size | Field                                                                                                                                               |
| ------ | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0      | 8    | **Magic** ASCII `HEXXLADB` + NUL                                                                                                                    |
| 8      | 4    | **format_version** `uint32` (**2** = plaintext MVCC; **3** = MVCC plus authenticated encrypted pages)                              |
| 12     | 4    | **page_size** `uint32` — one of 4096, 8192, 16384, or 65536                                                                                         |
| 16     | 8    | **last_wal_seq** `uint64` — last WAL sequence applied to the primary file                                                                           |
| 24     | 8    | **next_page_id** `uint64` — exclusive logical allocation boundary / next extension page id                                                           |
| 32     | 8    | **btree_root_page** `uint64` — B+ tree root (**0** = empty); see [ORDERED_STORE.md](./ORDERED_STORE.md)                                             |
| 40     | 4    | **features** `uint32` — bit **0** = encrypted pages; bit **1** = keyed WAL MAC; bit **2** = incomplete compaction candidate; bit **3** = authenticated v3 pages; bit **4** = complete WAL framing |
| 44     | 16   | **encryption_salt** — used with Argon2id passphrase mode and keyed encryption verifier derivation                                                   |
| 60     | 8    | **commit_seq** `uint64` — last committed logical sequence       |
| 68     | 32   | **encryption_key_check** — keyed verifier for deterministic wrong-key detection on encrypted DBs                                                    |
| 100    | 4    | **max_value_bytes** `uint32` — per-database max B+ tree value size; **0** = default (8192)                                                          |
| 104    | 2    | **embedding_dimension** `uint16` — zero until configured or detected on first vector write                                                          |
| 106    | 1    | **embedding_metric** `uint8` — persisted distance metric when the embedding dimension is non-zero                                                   |
| 107    | 32   | **header_auth_tag** — HMAC-SHA256 over the 512-byte header with this field zeroed; format v3 only                                                    |
| 139    | 8    | **btree_root_generation** `uint64` — expected authenticated rewrite generation for the current root; format v3 only                                  |
| 147    | 8    | **freelist_head_page** `uint64` — first external freelist metadata page, or zero; format v3 only                                                     |
| 155    | 8    | **freelist_head_generation** `uint64` — expected rewrite generation for the external head, or zero                                                   |
| 163    | 8    | **freelist_count** `uint64` — reusable data-page ids, excluding allocator metadata pages                                                             |
| 171    | 320  | **inline_freelist** — forty `uint64` free page ids; unused slots are zero                                                                             |
| 491    | 17   | **reserved** (zero) |
| 508    | 4    | V2 IEEE CRC-32 over bytes 0–507 when feature bit 4 is set, otherwise reserved zero. V3 reserves these bytes as zero. |

Retired and unknown engine formats fail with `ErrUnsupportedFormatVersion` before WAL replay. See [offline conversion](../../docs/hexxladb/OPERATIONS.md#offline-conversion-of-retired-files) for engine v1 and AES-XTS files.

## Authenticated data-page envelope (format v3)

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | **rewrite_generation** `uint64`, equal to the page redo sequence |
| 8 | 24 | random **XChaCha20-Poly1305 nonce** |
| 32 | `PageSize` | ciphertext |
| `32 + PageSize` | 16 | authentication tag |

Associated data binds the literal format label, database salt/identity, format version, logical page size, page id, and rewrite generation. Modification of the generation, nonce, ciphertext, tag, or page id fails authentication. The authenticated header additionally pins the current B+ tree root generation. A replay of an older valid non-root image into the same page slot is not distinguishable without a trusted per-page generation catalog; coordinated primary/header rollback similarly needs an external monotonic trust anchor. See [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).

## WAL file

The WAL holds redo for one serialized write transaction. Recovery validates the
entire file with a record-sized buffer before replaying active pages. For
current commits, validation binds the complete transaction before trusting
the primary header's watermark.

- **V2 writes:** The first active record uses `page_id=0`. Its payload
  contains the final primary header in bytes 0–511 and a big-endian `uint64`
  page-record count at byte 512. Page records follow in increasing sequence
  order. The marker and last page record share the final redo sequence.
  A header-only commit has count zero and its own sequence.
- **V3 writes:** The authenticated header marker follows the page
  records. Its sequence is the last page sequence, or a newly allocated one
  for a header-only commit.
- **Unmarked WAL:** A pending WAL without a commit header is refused.

Current writers set feature bit 4 in the committed header. In both formats,
the marker contains a page-record count at byte 512, `HXWAL002` at byte 520,
and a 32-byte SHA-256 digest at byte 528. The digest covers the ordered
concatenation of each page record's `seq || page_id || payload`.
V3's marker MAC authenticates this descriptor. V2 uses its record CRC and
digest to detect accidental corruption. A framed database rejects pending
redo that strips the required descriptor.

A complete framed WAL can repair a torn mutable primary header, provided its
bootstrap format, page size, and encryption identity remain readable. A v2
header checksum or v3 header authenticator must validate before recovery
trusts its watermark. Keep a pre-upgrade recovery set for older writers once
feature bit 4 has been persisted.

Recovery writes pages, syncs the primary, publishes and syncs the validated
header, then clears the WAL.

### WAL record

| Field       | Type                    | Notes                                                                                                                                                            |
| ----------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **seq**     | `uint64`                | Monotonic per database; new records continue after the last applied sequence.                                                                                    |
| **page_id** | `uint64`                | Target data page (**≥ 1**), or **0** for a commit header marker.                                                                               |
| **crc32**   | `uint32`                | IEEE CRC-32 of **payload**.                                                                                                                                      |
| **payload** | `[physicalPageSize]byte` | Full physical page image. A page-zero marker stores the final header and format-specific framing.                                                |
| **mac**     | `[32]byte` (conditional) | Present when header feature bit 1 is set. HMAC-SHA256 over `seq || page_id || payload`, using key material derived from the configured database encryption secret. |

Records are read sequentially from the start of the WAL file. A partial tail returns **`ErrCorruptWAL`** before replay modifies the primary.

## Value compression

Values ≥ 64 bytes are transparently compressed before storage using DEFLATE (`compress/flate`, Go standard library). Compression is always-on and requires no configuration. Compressed values carry a 5-byte envelope:

| Offset | Size | Field                                                                          |
| ------ | ---- | ------------------------------------------------------------------------------ |
| 0      | 1    | `0xFE` compression magic (distinct from overflow `0xFF` and record `'H'`=0x48) |
| 1      | 4    | **uncompressed_length** `uint32` big-endian                                    |
| 5..    | var  | DEFLATE-compressed payload (`compress/flate`, level 1)                         |

Values normally stay raw if compression does not reduce size. Raw values matching the compression marker or the exact 14-byte overflow-stub signature are wrapped even when that increases their size. Compression runs before the overflow threshold check. Reads bound the declared uncompressed length by the largest supported logical value size, independently of a subsequently lowered write limit.

## Overflow pages

Stored values larger than `max(pageSize/3, 256)` use overflow pages. The leaf entry holds a 14-byte overflow stub. Reads and releases validate the declared length and require exactly the corresponding number of pages. The stored-length limit allows bounded DEFLATE framing overhead for an escaped maximum-sized raw value.

### Overflow stub (in leaf)

| Offset | Size | Field                                                                               |
| ------ | ---- | ----------------------------------------------------------------------------------- |
| 0      | 2    | `0xFF 0x4F` overflow magic (`0xFF` cannot be the first byte of any record envelope) |
| 2      | 4    | **logical_length** `uint32` — full value size                                       |
| 6      | 8    | **first_page_id** `uint64` — first overflow page                                    |

### Overflow page layout

| Offset | Size         | Field                                                     |
| ------ | ------------ | --------------------------------------------------------- |
| 0      | 8            | **next_page_id** `uint64` — next overflow page (0 = last) |
| 8      | pageSize - 8 | payload chunk                                             |

Overflow pages are ordinary data pages: they are written via `WritePage`, appear in the WAL, and encrypt like any other page.

## Allocation and reclamation

Format v3 stores up to forty free page ids directly in the authenticated header.
Larger sets use one or more allocator-owned metadata pages. Each metadata page
has this logical plaintext layout before the ordinary v3 page envelope:

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | ASCII magic `HXFREE01` |
| 8 | 2 | free-id count in this page |
| 10 | 6 | reserved (zero) |
| 16 | 8 | next metadata page id, or zero |
| 24 | 8 | expected rewrite generation of the next metadata page, or zero |
| 32 | variable | `uint64` free page ids; capacity is `(PageSize - 32) / 8` |

The authenticated header pins the first metadata page generation; every page
pins the next, so an older allocator image cannot be substituted into the
current chain. Free ids and allocator pages are disjoint and duplicate ids fail
open. B+ tree merges, empty-root collapse, overflow deletion, and overflow
replacement queue releases in the active write transaction. Abort discards the
queue. Commit writes allocator metadata and the tree through one WAL/header
publication boundary. Allocation consumes the lowest free id before incrementing
`next_page_id`.

`DB.ReclaimTail` may lower `next_page_id` only across a contiguous suffix of
free or allocator-metadata pages. The lowered authenticated header is durable
before the primary is truncated. A crash in between leaves excess physical
bytes beyond `next_page_id`; a retry truncates those bytes. Reachable pages are
never truncated, and non-tail ranges are never hole-punched. Plaintext v2 continues to
advance `next_page_id`; copy-compaction remains the way to reclaim their dead
pages and to repack low-fill or fragmented v3 layouts.

## Page I/O

Plaintext v2 stores logical page bytes directly. Encrypted v3 uses the authenticated page transform at the engine boundary. See [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).
