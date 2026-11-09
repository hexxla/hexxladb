# Ordered store (B+ tree on engine pages)

This document specifies the B+ tree node format stored in the primary database file. It builds on **[ENGINE_FORMAT.md](./ENGINE_FORMAT.md)** (configurable page size, WAL). Bump the engine **`format_version`** for an incompatible layout change.

## Goals

- **Morton-aligned keys:** logical order matches [`PackedCoord.Compare`](../../internal/lattice/packed.go) when using [`internal/index`](../../internal/index) encodings (e.g. `cell/` keys).
- **Owned structure:** no third-party LSM/SQLite; all nodes live in ordinary data pages (`page_id >= 1`).
- **Concurrency boundary:** the tree does **not** take its own locks. The module-root database and engine write-transaction layers enforce single-writer ownership and snapshot-safe reads; direct internal callers must use those same ownership rules. Iterators copy returned keys and values rather than retaining mutable page buffers.

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

Separators are lower fences. Child **ptr0** contains keys below `key[0]`.
Other children contain keys at or above their lower fence and below the next
fence, when present. Removing a child's minimum can leave a gap above its
fence. Insertion order does not affect the routing contract.

Deletion merges same-parent leaves when their serialized entries fit on one
page. It unlinks an empty leaf before returning its page to the allocator.
Underfull leaves remain valid when neither sibling fits.

## Capacity and splits

- Leaf and internal node capacity is **dynamic**, derived from the database's page size. Inserts split only when the serialized node no longer fits; cascading splits greedily left-fill every emitted page and propagate all promoted children until the root fits.
- **Overflow pages:** values larger than the **inline threshold** (`pageSize - btreeHeaderSize - maxKeyBytes - 4`) are stored in a chain of overflow pages; the leaf entry holds a 14-byte stub (`0xFF 0x4F` magic + `uint32` logical length + `uint64` first page ID). See `overflow.go` and `ENGINE_FORMAT.md` for the on-disk layout.
- **Root:** when the root splits, a new internal node becomes the root; **`btree_root_page`** in the file header is updated.

## Allocator

- New pages use the engine’s **`next_page_id`** discipline: the next allocated id is the current **`NextPageID`** from the header before **`WritePage`** (see ENGINE_FORMAT). Plaintext v2 allocation is extend-only; operators reclaim dead pages through explicit compaction. Authenticated v3 persists generation-linked reusable page ids inside the authenticated transaction/WAL boundary, consumes the lowest free id before extending, and supports bounded tail reclaim.

## Key encoding (application layer)

Byte keys are **opaque** to **`BTree`**. Canonical **`cell/`** keys live in **[`internal/index`](../index/)** so scans align with lattice operations and **[HEXXLA_DB.md](../../docs/hexxladb/HEXXLA_DB.md)** storage layout.

## WAL / durability

Tree pages are ordinary data pages. Writes stage in the active engine
transaction and become durable with its WAL commit header. Direct page writes
use the same durability sequence.

## MVCC

The B+ tree orders opaque byte keys. The library owns logical versions,
timeline rows, and secondary indexes. The tree accepts those key families in
any insertion order.
