# Engine on-disk format

HexxlaDB uses a **configurable logical page size** (4/8/16/64 KiB; default **4 KiB**), one **primary database file**, and one **WAL file**. Engine format v1 stores unversioned logical rows; format v2 adds MVCC commit sequencing; encrypted format v3 adds authenticated headers, data-page envelopes, and WAL header publication. Numeric fields are **big-endian** unless noted. Bump **`format_version`** or the relevant magic for an incompatible layout change.

## Paths

- **Primary:** the path passed to `Open` (e.g. `dir/my.db`).
- **WAL:** `{primary}-wal` (ASCII hyphen + `wal`).

## Page size

- **Configurable per-database.** Accepted values: **4096**, **8192**, **16384**, **65536** bytes.
- Set via `Options.PageSize` at creation; persisted in the file header at offset 12 (`uint32`).
- **Default:** 4096 (4 KiB) for new databases. Existing databases (legacy 64 KiB) continue to work.
- Readable at runtime via `Engine.PageSizeInt()` / `DB.PageSize()`.

## Database file layout

- **Page 0** is the **meta/header page**: the first **512 bytes** hold a fixed **file header**; the remainder of page 0 is reserved (zeros).
- **Page IDs:** logical **`uint64`**. **Page 0** is header-only. **Pages with id ≥ 1** are data pages.
- **Formats v1/v2:** physical data-page size equals `PageSize`; page `p` starts at `p * PageSize`.
- **Authenticated format v3:** each data page has a 48-byte envelope, so `PhysicalPageSize = PageSize + 48`. Page 0 remains exactly `PageSize`; data page `p ≥ 1` starts at `PageSize + (p-1) * PhysicalPageSize`.
- **Allocation:** authenticated v3 reuses transactionally persisted free pages before extension; v1/v2 remain extend-only. Explicit compaction still repacks fragmented and partially filled pages.

## File header (first 512 bytes of page 0)

| Offset | Size | Field                                                                                                                                               |
| ------ | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0      | 8    | **Magic** ASCII `HEXXLADB` + NUL                                                                                                                    |
| 8      | 4    | **format_version** `uint32` (**1** = single-version; **2** = MVCC; **3** = MVCC plus authenticated encrypted pages)                              |
| 12     | 4    | **page_size** `uint32` — one of 4096, 8192, 16384, or 65536                                                                                         |
| 16     | 8    | **last_wal_seq** `uint64` — last WAL sequence applied to the primary file                                                                           |
| 24     | 8    | **next_page_id** `uint64` — exclusive logical allocation boundary / next extension page id                                                           |
| 32     | 8    | **btree_root_page** `uint64` — B+ tree root (**0** = empty); see [ORDERED_STORE.md](./ORDERED_STORE.md)                                             |
| 40     | 4    | **features** `uint32` — bit **0** = encrypted pages; bit **1** = keyed WAL MAC; bit **2** = incomplete compaction candidate; bit **3** = authenticated v3 pages |
| 44     | 16   | **encryption_salt** — used with Argon2id passphrase mode and keyed encryption verifier derivation                                                   |
| 60     | 8    | **commit_seq** `uint64` — last committed logical sequence (**format_version ≥ 2**); **zero** when **format_version == 1** (treated as unused)       |
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
| 491    | 21   | **reserved** (zero)                                                                                                                                   |

An unrecognized **format_version** fails open with `ErrUnsupportedFormatVersion`; the engine never guesses or auto-upgrades. `MigrateV1ToV2` creates a distinct v2 destination. `MigrateToAuthenticated` creates a distinct encrypted v3 destination from v1 or v2. Older libraries refuse v3; there is no in-place downgrade.

## Authenticated data-page envelope (format v3)

| Offset | Size | Field |
| --- | --- | --- |
| 0 | 8 | **rewrite_generation** `uint64`, equal to the page redo sequence |
| 8 | 24 | random **XChaCha20-Poly1305 nonce** |
| 32 | `PageSize` | ciphertext |
| `32 + PageSize` | 16 | authentication tag |

Associated data binds the literal format label, database salt/identity, format version, logical page size, page id, and rewrite generation. Modification of the generation, nonce, ciphertext, tag, or page id fails authentication. The authenticated header additionally pins the current B+ tree root generation. A replay of an older valid non-root image into the same page slot is not distinguishable without a trusted per-page generation catalog; coordinated primary/header rollback similarly needs an external monotonic trust anchor. See [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).

## WAL file

- Append-only **redo** log. Each **record** is self-contained.
- **Replay:** on `Open`, records with **seq > last_wal_seq** in the file header are applied. For v3, redo pages are accepted only with a matching authenticated header commit marker. Recovery writes pages, syncs the primary, publishes the authenticated header, syncs again, and only then normalizes the reusable WAL extent.

### WAL record (v1)

| Field       | Type                    | Notes                                                                                                                                                            |
| ----------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **seq**     | `uint64`                | Monotonic per database; new records continue after the last applied sequence.                                                                                    |
| **page_id** | `uint64`                | Target data page (**≥ 1**), or **0** for the v3 authenticated-header commit marker.                                                                               |
| **crc32**   | `uint32`                | IEEE CRC-32 of **payload**.                                                                                                                                      |
| **payload** | `[physicalPageSize]byte` | Full physical page image. In v3, a page-0 marker stores the authenticated logical header followed by zero padding.                                                |
| **mac**     | `[32]byte` (conditional) | Present when header feature bit 1 is set. HMAC-SHA256 over `seq || page_id || payload`, using key material derived from the configured database encryption secret. |

Records are read sequentially from the start of the WAL file. Partial tail → **`ErrCorruptWAL`**.

## Value compression

Values ≥ 64 bytes are transparently compressed before storage using DEFLATE (`compress/flate`, Go standard library). Compression is always-on and requires no configuration. Compressed values carry a 5-byte envelope:

| Offset | Size | Field                                                                          |
| ------ | ---- | ------------------------------------------------------------------------------ |
| 0      | 1    | `0xFE` compression magic (distinct from overflow `0xFF` and record `'H'`=0x48) |
| 1      | 4    | **uncompressed_length** `uint32` big-endian                                    |
| 5..    | var  | DEFLATE-compressed payload (`compress/flate`, level 1)                         |

If compression does not reduce size, the value is stored raw (no envelope). Compressed and uncompressed values coexist; the per-value `0xFE` magic disambiguates on read. Compression runs **before** the overflow threshold check, so compressible values may fit inline even if the raw size exceeds the threshold.

## Overflow pages

Values larger than the **inline threshold** (`pageSize - btreeHeaderSize - maxKeyBytes - 4`, typically ~3.7 KiB at 4 KiB page size) are stored in a chain of overflow pages. The leaf entry holds a 14-byte **overflow stub** instead of the raw value.

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
never truncated, and non-tail ranges are never hole-punched. V1/v2 continue to
advance `next_page_id`; copy-compaction remains the way to reclaim their dead
pages and to repack low-fill or fragmented v3 layouts.

## Page I/O hooks (encryption seam)

- Optional **BeforeWrite** / **AfterRead** transforms on page bytes (plaintext default). New databases created with official encryption use authenticated v3 XChaCha20-Poly1305 pages. Legacy encrypted v1/v2 files retain their AES-256-XTS transform and physical size. See [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).
