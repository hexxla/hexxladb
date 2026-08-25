# Engine on-disk format

HexxlaDB uses a **configurable page size** (4/8/16/64 KiB; default **4 KiB** for new databases), one **primary database file**, and one **WAL file**. Engine format v1 stores unversioned logical rows; format v2 adds MVCC commit sequencing. Numeric fields are **big-endian** unless noted. Bump **`format_version`** or the relevant magic for an incompatible layout change.

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
- **Byte offset** for page `p`: `p * PageSize` (so page 0 starts at file offset 0).
- **Allocation:** extend-only growth: writing page `p` extends the file to at least `(p+1) * PageSize` bytes. Dead pages are reclaimed only by explicit compaction.

## File header (first 512 bytes of page 0)

| Offset | Size | Field                                                                                                                                               |
| ------ | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0      | 8    | **Magic** ASCII `HEXXLADB` + NUL                                                                                                                    |
| 8      | 4    | **format_version** `uint32` (**1** = single-version cells; **2** = MVCC versioned keys; see [HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md))       |
| 12     | 4    | **page_size** `uint32` — one of 4096, 8192, 16384, or 65536                                                                                         |
| 16     | 8    | **last_wal_seq** `uint64` — last WAL sequence applied to the primary file                                                                           |
| 24     | 8    | **next_page_id** `uint64` — next extend-only allocator page id                                                                                      |
| 32     | 8    | **btree_root_page** `uint64` — B+ tree root (**0** = empty); see [ORDERED_STORE.md](./ORDERED_STORE.md)                                             |
| 40     | 4    | **features** `uint32` — bit **0** = encrypted data pages; bit **1** = keyed WAL MAC enabled; see [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md) |
| 44     | 16   | **encryption_salt** — used with Argon2id passphrase mode and keyed encryption verifier derivation                                                   |
| 60     | 8    | **commit_seq** `uint64` — last committed logical sequence (**format_version ≥ 2**); **zero** when **format_version == 1** (treated as unused)       |
| 68     | 32   | **encryption_key_check** — keyed verifier for deterministic wrong-key detection on encrypted DBs                                                    |
| 100    | 4    | **max_value_bytes** `uint32` — per-database max B+ tree value size; **0** = default (8192)                                                          |
| 104    | 2    | **embedding_dimension** `uint16` — zero until configured or detected on first vector write                                                          |
| 106    | 1    | **embedding_metric** `uint8` — persisted distance metric when the embedding dimension is non-zero                                                   |
| 107    | 405  | **reserved** (zero)                                                                                                                                 |

An unrecognized **format_version** fails open with `ErrUnsupportedFormatVersion`; the engine never guesses or auto-upgrades. The public `MigrateV1ToV2` workflow performs the supported source-preserving v1-to-v2 conversion into a distinct destination.

## WAL file

- Append-only **redo** log. Each **record** is self-contained.
- **Replay:** on `Open`, records with **seq > last_wal_seq** in the file header are applied. Then **`last_wal_seq`** is updated and the reusable WAL extent is normalized (crash-safe ordering: append record → sync WAL → write pages → sync primary → advance and sync header).

### WAL record (v1)

| Field       | Type                    | Notes                                                                                                                                                            |
| ----------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **seq**     | `uint64`                | Monotonic per database; new records continue after the last applied sequence.                                                                                    |
| **page_id** | `uint64`                | Target data page; **≥ 1**. Page 0 is published separately after data-page durability.                                                                            |
| **crc32**   | `uint32`                | IEEE CRC-32 of **payload**.                                                                                                                                      |
| **payload** | `[pageSize]byte`         | Full page image; ciphertext when encryption hooks are enabled. See [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).                                           |
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

The engine has no persistent freelist. `next_page_id` advances as new tree and overflow pages are allocated, so deletes, rewrites, and MVCC pruning can leave unreachable pages. `DB.StorageStats` measures whole unreachable pages and explicit copy-compaction rewrites live keys into a smaller replacement. Persistent page reuse remains deferred until allocator state can be proven safe across WAL replay.

## Page I/O hooks (encryption seam)

- Optional **BeforeWrite** / **AfterRead** transforms on page bytes (plaintext default). Public [`Open`](../../db.go) can configure **AES-256-XTS** for data pages via [`Options`](../../options.go); see [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).
