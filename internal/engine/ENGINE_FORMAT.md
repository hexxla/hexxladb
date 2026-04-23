# Engine on-disk format (M3 design gate)

HexxlaDB v1 engine shell: **64 KiB pages**, one **primary database file** and one **WAL file**. Numeric fields are **big-endian** unless noted. Bump **`format_version`** / magic if layouts change incompatibly.

## Paths

- **Primary:** the path passed to `Open` (e.g. `dir/my.db`).
- **WAL:** `{primary}-wal` (ASCII hyphen + `wal`).

## Page size

- **`PageSize` = 65536** (64 KiB), compile-time constant in code.
- [HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md) allows 16 or 64 KiB; this repo locks **64 KiB** for v1.

## Database file layout

- **Page 0** is the **meta/header page**: the first **512 bytes** hold a fixed **file header**; the remainder of page 0 is reserved (zeros).
- **Page IDs:** logical **`uint64`**. **Page 0** is header-only. **Pages with id ≥ 1** are data pages.
- **Byte offset** for page `p`: `p * PageSize` (so page 0 starts at file offset 0).
- **Allocation (shell):** append-only growth: writing page `p` extends the file to at least `(p+1) * PageSize` bytes.

## File header (first 512 bytes of page 0)

| Offset | Size | Field                                                                                                                                            |
| ------ | ---- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| 0      | 8    | **Magic** ASCII `HEXXLADB` + NUL                                                                                                                 |
| 8      | 4    | **format_version** `uint32` (**1** = single-version cells; **2** = MVCC versioned keys; see [HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md))          |
| 12     | 4    | **page_size** `uint32` (65536)                                                                                                                   |
| 16     | 8    | **last_wal_seq** `uint64` — last WAL sequence applied to the primary file                                                                        |
| 24     | 8    | **next_page_id** `uint64` — allocator hint for M4+ (shell may still update)                                                                      |
| 32     | 8    | **btree_root_page** `uint64` — B+ tree root (**0** = empty); see [ORDERED_STORE.md](./ORDERED_STORE.md)                                          |
| 40     | 4    | **features** `uint32` — bit **0** = encrypted data pages; bit **1** = keyed WAL MAC enabled; see [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md) |
| 44     | 16   | **encryption_salt** — used with Argon2id passphrase mode and keyed encryption verifier derivation                                                  |
| 60     | 8    | **commit_seq** `uint64` — last committed logical sequence (**format_version ≥ 2**); **zero** when **format_version == 1** (treated as unused)     |
| 68     | 32   | **encryption_key_check** — keyed verifier for deterministic wrong-key detection on encrypted DBs                                                  |
| 100    | 412  | **reserved** (zero)                                                                                                                              |

Unrecognized **format_version** → open fails (forward-only policy; migration tooling later).

## WAL file

- Append-only **redo** log. Each **record** is self-contained.
- **Replay:** on `Open`, records with **seq > last_wal_seq** in the file header are applied (page write). Then **`last_wal_seq`** is updated and the WAL may be **truncated** to length 0 for the shell (crash-safe ordering: append record → fsync WAL → write page → fsync DB → advance header).

### WAL record (v1)

| Field       | Type             | Notes                                                                                                                                                          |
| ----------- | ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **seq**     | `uint64`         | Monotonic per database; must be **last_wal_seq + 1** for new appends after recovery                                                                            |
| **page_id** | `uint64`         | Target page; **≥ 1** (page 0 is not WAL-patched by shell tests)                                                                                                |
| **crc32**   | `uint32`         | IEEE CRC-32 of **payload**                                                                                                                                     |
| **payload** | `[PageSize]byte` | Full page image (same bytes written to the primary — **ciphertext** when encryption hooks are enabled; see [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md)) |
| **mac**     | `[32]byte` (optional) | Present when header feature bit 1 is set. HMAC-SHA256 over `seq || page_id || payload` using key derived from encryption key material. |

Records are read sequentially from the start of the WAL file. Partial tail → **`ErrCorruptWAL`**.

## Freelist

- **M3 shell:** no separate freelist structure; **next_page_id** in the header documents intent for **M4**. Extending the file is sufficient for tests.

## Page I/O hooks (encryption seam)

- Optional **BeforeWrite** / **AfterRead** transforms on page bytes (plaintext default). Public [`Open`](../../db.go) can configure **AES-256-XTS** for data pages via [`Options`](../../options.go); see [ENCRYPTION.md](../../docs/hexxladb/ENCRYPTION.md).
