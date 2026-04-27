# Ordered store (M4 — B+ tree on engine pages)

This document specifies the **v1 B+ tree** stored in the primary database file. It builds on **[ENGINE_FORMAT.md](./ENGINE_FORMAT.md)** (configurable page size, WAL). Bump **`format_version`** if any incompatible layout changes.

## Goals

- **Morton-aligned keys:** logical order matches [`PackedCoord.Compare`](../../internal/lattice/packed.go) when using [`internal/index`](../../internal/index) encodings (e.g. `cell/` keys).
- **Owned structure:** no third-party LSM/SQLite; all nodes live in ordinary data pages (`page_id >= 1`).
- **M5 concurrency:** **single writer, multiple readers** at the API boundary; the tree does **not** take locks. Callers may wrap **`Engine`** / **`BTree`** with a future `RWMutex`. Iterators must not retain stale page buffers across concurrent writes (copy bytes or hold the lock).

## File header additions

The first **512 bytes** of page 0 are the file header. Fields **32–39** (in addition to ENGINE_FORMAT):

| Offset | Size | Field                                                               |
| ------ | ---- | ------------------------------------------------------------------- |
| 32     | 8    | **btree_root_page** `uint64` — root page id; **0** means empty tree |

Older databases that zeroed this region behave as **empty tree** (`root = 0`).

## Page layout (all tree nodes)

Every tree page uses the full **page size** (configurable per-database: 4/8/16/64 KiB). Byte order: **big-endian** for multi-byte numeric fields.

### Common header (first 64 bytes)

| Offset | Size | Field                                                                       |
| ------ | ---- | --------------------------------------------------------------------------- |
| 0      | 4    | Magic ASCII **`HXBT`**                                                      |
| 4      | 1    | **version** `uint8` (1)                                                     |
| 5      | 1    | **kind** `uint8` — **1** = leaf, **2** = internal                           |
| 6      | 2    | **nkeys** `uint16` — number of keys (leaf: KV pairs; internal: separators)  |
| 8      | 8    | **next** `uint64` — leaf: next leaf page id (**0** = none); internal: **0** |
| 16     | 8    | **parent** `uint64` — parent page id (**0** = root)                         |
| 24     | 40   | **reserved** (zero)                                                         |

### Leaf payload (offset 64+)

For each entry `i` in `[0, nkeys)`:

| Field       | Type            |
| ----------- | --------------- |
| **key_len** | `uint16`        |
| **val_len** | `uint16`        |
| **key**     | `key_len` bytes |
| **value**   | `val_len` bytes |

Keys are sorted in **ascending lexicographic byte order**. Values are opaque to the tree.

### Internal payload (offset 64+)

| Field                                                                            | Type                              |
| -------------------------------------------------------------------------------- | --------------------------------- |
| **ptr0**                                                                         | `uint64` — leftmost child page id |
| For `i` in `0 .. nkeys-1`: **key_len** `uint16`, **key** bytes, **ptr** `uint64` | separator + right child           |

Invariant: child page **ptr0** contains keys `< key[0]`; **ptr[i+1]** contains keys `>= key[i]` (standard B+ internal routing).

## Capacity and splits

- Leaf and internal node capacity is **dynamic**, derived from the database's page size. Inserts use **fill-based splitting**: a node splits when its serialized size exceeds ~50% of the page. See `btree_page.go` for `maxLeafEntriesForPage` / `maxInternalChildrenForPage`.
- **Overflow pages:** values larger than the **inline threshold** (`pageSize - btreeHeaderSize - maxKeyBytes - 4`) are stored in a chain of overflow pages; the leaf entry holds a 13-byte stub (`0x01` marker + `uint32` logical length + `uint64` first page ID). See `overflow.go` and `ENGINE_FORMAT.md` for the on-disk layout.
- **Root:** when the root splits, a new internal node becomes the root; **`btree_root_page`** in the file header is updated.

## Allocator

- New pages use the engine’s **`next_page_id`** discipline: the next allocated id is the current **`NextPageID`** from the header before **`WritePage`** (see ENGINE_FORMAT). The B+ tree does not maintain a separate freelist in M4 (**extend-only**); reuse is a later milestone.

## Key encoding (application layer)

Byte keys are **opaque** to **`BTree`**. Canonical **`cell/`** keys live in **[`internal/index`](../index/)** so scans align with lattice operations and **[HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md)** storage layout.

## WAL / durability

Tree pages are ordinary data pages: **`WritePage`** appends WAL records and updates the header’s **`last_wal_seq`**. No separate WAL format beyond ENGINE_FORMAT.

## MVCC (format v2) and raw `Put`

The B+ tree remains **byte-key ordered** and opaque to MVCC versioning. Application writers should use **`Tx.PutCell`** / **`Update`** paths so **`cell/`** and **`__meta/`** timeline keys stay consistent. Raw **`Tx.Put`** with **`cell/`** keys inserted before **`__meta/commit-time/`** keys can stress delete/rebalance paths; avoid unless you control ordering end-to-end.
