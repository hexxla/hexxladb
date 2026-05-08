# HexxlaDB Engine Improvement Candidates

Source: comparative analysis of Badger v4 (`/home/anon/Documents/GitHub/badger/`),
bbolt, LevelDB/Pebble, and LMDB against HexxlaDB's `internal/engine` package.

---

## Recommended Improvements

Distilled from BadgerDB, bbolt, and Pebble source analysis.
Improvements are ordered by expected impact on HexxlaDB's primary workload
(LLM agent sessions: frequent reads, sequential-key writes, sparse deletes).

---

### 1 — Hot Page Cache with CLOCK-Pro ✅ IMPLEMENTED

#### Problem

Every B+ tree operation (`Get`, `Put`, `AscendRange`, `Delete`) performs a
root-to-leaf traversal. For a tree of height H, that is H `pread` syscalls.
At the default page size of 4096 bytes and 100k cells, the tree is roughly
4 levels tall. Every `GetCell` call therefore does 4 `pread` calls against
the primary file — even if the OS page cache is warm, that is 4 kernel
crossings per lookup.

Currently `pageBufPool` (`sync.Pool`) recycles page-sized byte slices to
avoid per-read heap allocations, but it does **not** retain hot pages between
reads. The same root and level-1 pages are re-read from disk (or OS cache)
on every operation.

#### Source evidence

- **bbolt** (`db.go:1113`): `db.page(id)` is pointer arithmetic into an mmap.
  Zero I/O. The OS kernel is the LRU. Cannot use mmap in HexxlaDB (encrypted pages),
  but this shows the cost gap is real.
- **Badger**: two Ristretto caches (`blockCache`, `indexCache`) — cache hit = zero
  file I/O.
- **Pebble** (`internal/cache/clockpro.go`): `4×NumCPU` sharded **CLOCK-Pro** cache
  with three entry states — `etHot`, `etCold`, `etTest` (ghost). Ghost entries
  (data freed, key retained) detect re-access of recently-evicted pages and promote
  them to hot without re-reading. This is the correct algorithm for scan-heavy
  workloads like HexxlaDB's `AscendRange`.

#### Why CLOCK-Pro, not plain LRU

Plain LRU suffers from **scan pollution**: a single `AscendRange` over many leaf
pages evicts the working set. CLOCK-Pro's ghost list (`etTest`) detects that a
page was recently evicted and re-accessed, and ratchets `coldTarget` up to protect
it next time. For HexxlaDB's `QueryCells` / `LoadContext` scan patterns, this
directly reduces cold-start cost on repeated queries over the same region.

#### HexxlaDB improvement

Implement a sharded CLOCK-Pro page cache in `internal/engine/page_cache.go`.

```go
type pageCache struct {
    shards []pcShard  // len = min(4*runtime.GOMAXPROCS(0), 16)
}

type pcShard struct {
    mu       sync.Mutex
    entries  map[uint64]*pcEntry  // pageID → entry
    handCold *pcEntry
    handHot  *pcEntry
    handTest *pcEntry
    sizeHot, sizeCold, sizeTest int64
    maxSize  int64
}
```

Shard index: `pageID * 11400714819323198485 >> 32 % numShards` (Fibonacci hash,
from Pebble's `clockpro.go:55-66`).

Wire into `readPageFromDisk`: check cache before `pread`, populate on miss.
Invalidate on `WritePage` — writer already holds exclusive access, so cache
invalidation is safe without additional locking.

Expose `Options.PageCacheSize int64` (bytes; 0 = 4 MiB default, -1 = disabled).

**Status:** Implemented in `internal/engine/page_cache.go`. Enabled by default (4 MiB) via `mergeEnginePageCache` in `db_open.go`. `Engine.PageCacheStats()` returns cumulative hits/misses.

For a tree of height 4:

- Without cache: 4 `pread` calls per `Get`
- With root + level 1 cached: 2 `pread` calls per `Get`
- With root + level 1 + level 2 cached: 1 `pread` per `Get`

For `AscendRange`, caching eliminates descent cost entirely — only leaf pages
are read from disk. For 50 concurrent agent sessions with 1000 turns each:
~40k → ~10k `pread` calls per context load cycle.

---

### 2 — Cached DB Header ✅ IMPLEMENTED

#### Problem

`DB.View` and `DB.ViewAt` call `db.eng.ReadHeader()` on every invocation:

```go
// tx.go:48
hdr, err := db.eng.ReadHeader()
tx := &Tx{..., readSeq: hdr.CommitSeq, cachedBTreeRoot: hdr.BTreeRoot}
```

`ReadHeader` does a `pread` of page 0 (the header page) from disk on every
`View` call, even when the header has not changed since the last read.

#### Source evidence

- **Badger** (`txn.go`): `oracle.readTs()` returns `o.nextTxnTs - 1` — a
  pure `atomic.Load`. No I/O. The oracle is updated only by committing writers.
- **bbolt** (`tx.go:629-641`): `tx.page(id)` resolves to a mmap pointer. Reading
  meta on `beginTx` is a pointer dereference, not a syscall.
- **Pebble**: sequence numbers are `base.AtomicSeqNum` — atomic loads only.

All three databases pay zero I/O for readers to get their snapshot point.

#### HexxlaDB improvement

Cache `CommitSeq` and `BTreeRoot` as a single `atomic.Pointer[cachedHeader]`
in the `DB` struct. Writers update it under `db.mu.Lock()` after commit.
Readers load it with no I/O.

**Status:** Implemented. `dbCachedHeader` stored as `atomic.Pointer[dbCachedHeader]` on `DB`. Populated at `Open` and refreshed after every `Update` commit. `View`, `ViewAt`, `ViewAtTime` all read from the cache — zero `pread(page0)` on the hot read path.

---

### 3 — Configurable Leaf Fill Percent — N/A

#### Problem

HexxlaDB's B+ tree leaf split position is hardcoded at ~50% fill
(`btree_page.go`). For HexxlaDB's primary write pattern — sequential
hex-coordinate keys written by appending agent turns — a 50% fill means every
leaf page is half-empty after a split, doubling leaf count and worsening scan
locality.

#### Source evidence

- **bbolt** (`bucket.go:27`, `node.go:237-243`): `DefaultFillPercent = 0.5`,
  configurable per `Bucket` from 0.1 to 1.0. For append-only workloads,
  `FillPercent = 1.0` halves the number of leaf splits.
- `splitIndex()` (`node.go:271-291`) finds the split position as the first
  index where cumulative page size exceeds `pageSize * FillPercent`.

#### HexxlaDB improvement

Add `LeafFillPercent float64` to `engine.Options` (default 0.5, range 0.5–1.0).
Pass it into the split-position calculation in `btree_page.go`. For the
`cell/…` keyspace (always sequential writes), a setting of 0.9 would reduce
leaf page count by ~40% and improve sequential scan throughput proportionally.

This is a single-line change to the split threshold computation — minimal risk.

**Status:** Not applicable. HexxlaDB's `leafSplitIndex` already fills the left page to capacity (stops when adding the next entry would overflow `pageSize`). This is equivalent to bbolt's `FillPercent = 1.0`. No change needed.

---

### 4 — WAL File Recycling ✅ IMPLEMENTED

#### Problem

HexxlaDB uses a WAL-truncate-per-commit model: the WAL file is truncated to
zero (or re-created) after each successful commit. On ext4/XFS, `fallocate`
and inode metadata updates on every commit add measurable latency for
high-frequency write workloads.

#### Source evidence

- **Pebble** (`wal/log_recycler.go`): `LogRecycler` keeps a pool of closed WAL
  files. On the next WAL rotation, a recycled file is reused by overwriting
  from position 0 with a new log record header. This avoids `fallocate` and
  directory-entry updates entirely.
- **Badger**: WAL files are similarly reused via value log GC, avoiding
  file creation overhead.

#### HexxlaDB improvement

After a successful commit, instead of truncating the WAL file to zero, rename
it to a recycle slot (e.g. `wal.recycle`). On the next write transaction
start, if the recycle slot exists, `lseek` to position 0 and reuse it. If not,
create a new WAL file. This requires one additional file rename per commit
cycle but eliminates the `fallocate` cost on the critical path.

Keep at most 1 recycled WAL slot to bound disk space usage (one WAL file's
worth of extra space at most).

**Status:** Implemented. `walSize int64` tracks bytes written to the WAL since the last reset. `groupTruncateWAL` and `CommitWriteTxn` now call `Truncate(walSize)` instead of `Truncate(0)`, leaving the allocated inode blocks intact for the next commit. `openTruncateWAL` (post-replay) still truncates to 0 for a clean start. Both classic and group-WAL paths updated.

---

### 5 — `IsClosed` Atomic Fast-Path ✅ IMPLEMENTED

#### Problem

Every `View`, `Update`, `GetCell`, `AscendRange` etc. checks
`db.activeEng() == nil` inside the mutex:

```go
db.mu.RLock()
defer db.mu.RUnlock()
if db.activeEng() == nil { return ErrDatabaseClosed }
```

For a closed DB every call still acquires the read lock before discovering
the DB is closed.

#### Source evidence

- **Badger** (`db.go`): `db.isClosed.Load() == 1` checked at the top of
  every public method before acquiring any lock.

#### HexxlaDB improvement

Add `closed atomic.Bool` to `DB`. Set it in `Close()` before releasing the
mutex. Check it at the top of `View`/`Update`/`Batch` before acquiring the
lock.

**Impact**: negligible for normal use, measurable under pathological
concurrent-close workloads and useful as a fast-fail guard.

**Status:** Implemented. `closed atomic.Bool` added to `DB`. Set in `Close()` before releasing the mutex. Checked at the top of `View`, `ViewAt`, `ViewAtTime`, and `Update` before acquiring any lock.

---

### 6 — Lazy Rebalance on Delete (Low Priority) — DEFERRED

#### Problem

HexxlaDB currently rebalances (merges underfull pages) eagerly on each
individual delete. For a transaction that deletes N keys, this may trigger
up to N rebalance operations.

#### Source evidence

- **bbolt** (`node.go:365-448`): nodes are marked `unbalanced = true` on
  delete. `rebalance()` is called once per underfull node at commit time.
  Multiple deletes in the same transaction merge once, not N times.

#### HexxlaDB improvement

Defer rebalance to commit time. Mark pages as needing rebalance in a dirty
set; process at end of the write transaction. For HexxlaDB's sparse-delete
workload (agents rarely delete cells), the impact is minimal — but for any
bulk-delete path (MVCC version pruning, cell expiry), this avoids redundant
intermediate merges.

#### Analysis and deferral rationale

Implementing lazy rebalance requires non-trivial structural changes:

1. A `pendingRebalance map[uint64]*leafData` on `BTree` or `Engine` to
   accumulate underfull pages across a transaction.
2. A `BTree.FlushPendingRebalances()` method to process them at commit time.
3. Plumbing from `Engine.CommitWriteTxn` / `applyGroupBatchPipeline` into
   `BTree` before the WAL write, since rebalance generates additional page writes.
4. Careful ordering: rebalance must run _before_ WAL write (not after) so that
   the merged pages are part of the durable commit unit.

HexxlaDB's `BTree` is currently stateless between operations (no transaction
state). Introducing a deferred-rebalance set adds meaningful complexity and
coupling between `BTree` and `Engine` commit paths. For HexxlaDB's primary
workload (sparse deletes — agents append turns far more than they delete),
the benefit is small and the risk of introducing a rebalance bug is real.

**Decision:** Defer. Revisit if bulk-delete workloads (MVCC pruning at scale)
profiling shows rebalance as a measurable cost.

---

### 7 — MVCC Prune: Single-Pass Scan ✅ IMPLEMENTED

#### Problem

`PruneCellVersions` scans the entire `cell/` keyspace **twice**: first to build
`latest map[PackedCoord]uint64`, then again to collect `toDelete`. Both scans
hold `db.mu.Lock()`, blocking all readers for the full duration. With 1M
versioned rows this is 2× O(N) leaf reads under an exclusive lock.

#### Root cause

The two-pass approach was chosen to avoid deleting the latest version of a cell.
But because `cell/<packed>/<seq>` keys are stored in ascending `seq` order per
coordinate, the scan can be collapsed into a **single left-to-right pass**: for
each logical coordinate group, all rows except the last one (highest `seq`) are
candidates if `seq < beforeSeq`. This requires tracking the previous key while
scanning — no map needed.

#### HexxlaDB improvement

Replace the two `AscendRange` calls with one. While scanning, carry a
`prev key + prev seq` pair. When the coordinate changes (or the scan ends),
emit `prev` as a delete candidate if `prevSeq < beforeSeq`. This is O(N) in
one pass with O(1) extra memory.

Also fix `SuggestedPruneBeforeSeq` and `StatsMVCC` to use `db.cachedHdr.Load()`
instead of `db.eng.ReadHeader()` (missed by improvement #2).

**Status:** Implemented. Single `AscendRange` pass buffers `prevKey/prevSeq/prevCoord`; emits `prevKey` as a delete candidate when coord has not changed and `prevSeq < beforeSeq`. Latest version of each group is never emitted. `SuggestedPruneBeforeSeq` and `StatsMVCC` now use `cachedHdr.Load()`.

---

### 8 — MVCC Prune: Batched Commits ✅ IMPLEMENTED

#### Problem

All `maxDelete` deletes in `PruneCellVersions` run in a single
`BeginWriteTxn`/`CommitWriteTxn` cycle. Each `BTree.Delete` may trigger an
eager rebalance, generating additional page writes to the WAL. With
`maxDelete=2048`, up to 2048 rebalance-triggered page writes land in a single
WAL commit — a large WAL record burst that blocks the group-WAL flusher for
all concurrent writers during the sync.

#### HexxlaDB improvement

Split the delete loop into sub-batches of a fixed size (e.g. 64 deletes per
commit). Each sub-batch is a separate `BeginWriteTxn`/`CommitWriteTxn`. This
caps WAL burst size, keeps individual commit latency low, and releases
`db.mu.Lock()` between sub-batches so concurrent readers are not starved.

**Status:** Implemented. `pruneSubBatchSize = 64`. Delete loop processes at most 64 keys per `CommitWriteTxn`. Lock is released and re-acquired between sub-batches with a liveness check. Cached header refreshed after each sub-batch.

---

## B+ Tree Databases to Analyse

These are the canonical B+ tree embedded databases. Each makes different
trade-offs on the same core problem (page-level B+ tree + WAL/MVCC).
Analysing them against HexxlaDB's engine reveals what is already correct
and what can be improved.

### bbolt / etcd/bbolt

#### Architecture (sourced from `/home/anon/Documents/GitHub/bbolt/`)

- **Language**: Go
- **Model**: Single-writer, mmap-based B+ tree with COW spill-on-commit
- **WAL**: None — dirty pages are held in a per-transaction `pages map[Pgid]*Page`
  in memory, written directly to file on `Commit()`, then the meta page is
  written as the atomic visibility barrier
- **Concurrency**: `rwlock sync.Mutex` (one writer), `mmaplock sync.RWMutex`
  (readers hold RLock, remmap holds Lock), `metalock sync.Mutex` (meta page access)
- **Page cache**: The OS mmap is the cache. `db.page(id)` is pure pointer
  arithmetic into the mmap — zero I/O, zero kernel crossing:

```go
func (db *DB) page(id Pgid) *Page {
    pos := id * Pgid(db.pageSize)
    return (*Page)(unsafe.Pointer(&db.data[pos]))
}
```

- **Node cache**: Per-transaction `Bucket.nodes map[Pgid]*node` — deserialized
  in-memory nodes. `pageNode(id)` checks this map first, falls back to mmap
  pointer. Once a node is materialized (touched by a write), it stays in memory
  for the transaction duration
- **Freelist**: Explicit free page tracking (`freelist.Interface`, array or
  hashmap backend). Freed pages accumulate as `pending[txid][]Pgid` and become
  available once no reader transaction with `txid ≤ freed_txid` is open
- **Split / rebalance**: Deferred to commit-time `spill()`. During the
  transaction, writes mutate in-memory nodes only. On commit: `rebalance()` merges
  underfull nodes, then `spill()` recurses children-first, splits overfull nodes
  into new page allocations, writes them to the dirty-page map, then `write()`
  flushes all dirty pages in page-ID order with a single `fdatasync`, then
  `writeMeta()` writes the meta page (the commit point)
- **Batch**: `DB.Batch()` collects multiple `fn` callbacks under `batchMu`,
  fires a `time.AfterFunc` timer, runs all fns in a single `Update()`. If one
  fn fails, it is removed from the batch and retried solo (`trySolo` sentinel)
- **Meta pages**: Two alternating meta pages (page 0 and page 1). `db.meta()`
  returns the one with the higher valid `Txid`. Provides crash recovery without
  a WAL — if the last write failed, the lower-txid meta is still valid
- **FillPercent**: `Bucket.FillPercent` (default 0.5) controls the split
  threshold. `splitIndex()` finds the position where cumulative size exceeds
  `pageSize * FillPercent`. Append-heavy workloads should set this to 1.0

#### What HexxlaDB already does correctly vs bbolt

| Aspect                   | bbolt                          | HexxlaDB                                 | Assessment                                         |
| ------------------------ | ------------------------------ | ---------------------------------------- | -------------------------------------------------- |
| Page read I/O            | Zero (mmap pointer)            | `pread` syscall per page                 | **bbolt wins**                                     |
| Meta read on `View()`    | Zero — `meta` pointer in mmap  | `pread` of page 0                        | **bbolt wins** — see improvement #2                |
| Node cache (write txn)   | `nodes map[Pgid]*node` per txn | `cellOverlay map[PackedCoord]CellRecord` | Equivalent for read-your-writes                    |
| Split deferred to commit | Yes — in-memory during txn     | Yes — WAL buffers all writes             | **Equivalent**                                     |
| `minKeysPerPage = 2`     | `common.MinKeysPerPage = 2`    | `minKeysPerPage = 2` (citing bbolt)      | **Identical**                                      |
| Rebalance on delete      | `node.rebalance()` at commit   | In-place B+ tree merge at write time     | HexxlaDB is eager; bbolt is lazy                   |
| Page reuse               | Freelist (pending → free)      | Monotonic `NextPageID` (no reuse yet)    | **bbolt wins** for long-running DBs                |
| Encryption               | Not supported                  | Per-page encryption                      | **HexxlaDB wins** (mmap + encryption incompatible) |
| Crash recovery           | Dual meta pages, no WAL needed | WAL redo, truncated per commit           | Both correct; different trade-offs                 |

#### New findings from bbolt source — additional HexxlaDB improvements

**Finding A: Eager rebalance vs lazy rebalance**

bbolt marks nodes `unbalanced = true` on delete, but only calls `rebalance()`
at commit time. This means multiple deletes in the same transaction merge once,
not once per delete. HexxlaDB rebalances (merges underfull pages) eagerly at
write time — if a transaction deletes 50 keys, HexxlaDB may rebalance 50 times;
bbolt rebalances once per underfull node at commit.

For HexxlaDB's workloads (sparse deletes, mostly writes), this is unlikely to
matter. For bulk-delete workloads (e.g. pruning old MVCC versions), lazy
rebalance would reduce write amplification during the transaction.

**Finding B: FillPercent / split threshold is hardcoded in HexxlaDB**

bbolt exposes `Bucket.FillPercent` (default 0.5, range 0.1–1.0). For
append-heavy workloads (HexxlaDB's primary pattern — agents appending turns),
setting `FillPercent = 1.0` means pages fill completely before splitting,
halving the number of leaf splits for sequential-key writes.

HexxlaDB's `btree_page.go` computes split position at 50% fill with no
configurable threshold. For the `cell/…` keyspace (always appended in
hex-coordinate order), a higher fill percent would reduce page count and
improve scan locality.

**Finding C: `NoFreelistSync` — skip freelist persistence**

bbolt's `NoFreelistSync` option skips writing the freelist page on every
commit (saves one page write + one `fdatasync`). Recovery reconstructs the
freelist by scanning the DB. For HexxlaDB, which uses monotonic page
allocation (no freelist yet), this is not applicable today — but when a
freelist is added (improvement prerequisite for page reuse), this option
pattern should be adopted from the start.

**Finding D: `db.pagePool sync.Pool` — identical to HexxlaDB**

bbolt uses `db.pagePool = sync.Pool{New: func() interface{} { return
make([]byte, db.pageSize) }}` for single-page allocations during spill.
HexxlaDB uses `pageBufPool` identically. **No action needed.**

**Finding E: `NoStatistics` option for high-concurrency read paths**

bbolt added `Options.NoStatistics` specifically because `statlock.Lock()`
in `removeTx()` was a bottleneck under high-concurrency read-only workloads.
HexxlaDB does not have per-transaction statistics contention today, but if
stats are added later, this pattern (opt-in stats under a lock vs always-on)
should be followed.

### LMDB (Lightning Memory-Mapped Database)

- **Language**: C
- **Model**: MVCC via copy-on-write B+ tree, two alternating root pages
- **WAL**: None — uses two root slots; readers see one, writers update the
  other. Reader snapshot is guaranteed by not overwriting the old root
  until no reader holds it
- **Concurrency**: MVCC — multiple readers never block; one writer
- **Page cache**: mmap — OS manages cache
- **Relevance to HexxlaDB**:
  - LMDB's two-root-slot MVCC is conceptually close to HexxlaDB's
    `ViewAt(readSeq)` pinning mechanism but avoids WAL entirely
  - LMDB copies-on-write every modified page up to the root — for a tree
    of height H, a single key update writes H pages. HexxlaDB's WAL-based
    approach writes only changed pages to the WAL, then applies in-place
  - LMDB's approach: lower write amplification for read-heavy workloads;
    HexxlaDB's approach: lower space amplification (no shadow copies)
  - **Lesson**: LMDB's two-root-slot trick is worth understanding for
    HexxlaDB's `ViewAt` — could allow snapshotted reads without any header
    read (just an atomic pointer load)

### Pebble (CockroachDB's embedded store)

#### Architecture (sourced from `/home/anon/Documents/GitHub/pebble/`)

- **Language**: Go
- **Model**: LSM tree (log-structured merge-tree) — **not** a B+ tree at the
  storage level. Data is written to a memtable (arena-backed skiplist), flushed
  to immutable SSTable files, and compacted in background goroutines. Very
  different from HexxlaDB's B+ tree engine, but Pebble's infrastructure
  components are the best Go examples of patterns HexxlaDB needs
- **Memtable**: `arenaskl.Skiplist` backed by a manually-managed arena
  (`manual.New`/`manual.Free` → `C.malloc`/`C.free` via cgo, or Go allocator
  without cgo). Fixed-size arena per memtable; when full, a new memtable is
  allocated. Concurrent inserts supported; deletes are tombstones only
- **WAL**: Separate `wal/` package. `StandaloneManager` (one log file per WAL)
  and `FailoverManager` (dual primary+secondary for HA). WAL files are
  **recycled** via `LogRecycler` — closed WAL files are kept in a pool and
  reused for the next WAL rotation, avoiding `fallocate` overhead. Max recycled
  log pool is configurable (`Options.MaxNumRecyclableLogs`)
- **Commit pipeline**: `commitPipeline` in `commit.go` — the most advanced
  write batching model of the databases surveyed. Key design points:
  - Serialized only for WAL write (under `commitPipeline.mu`). WAL write is
    a memory copy to a `block.buf[blockSize]byte`, extremely fast
  - Concurrent memtable apply: once a batch is written to WAL, the mutex is
    released and goroutines apply to the skiplist concurrently
  - Lock-free `commitQueue` (SPMC ring buffer keyed on `headTail atomic.Uint64`)
    tracks in-flight batches in WAL order
  - `publish()` ratchets `visibleSeqNum` in WAL order using CAS on an
    `AtomicSeqNum` — no goroutine wakeup needed; later batches that finish
    applying first do the ratchet work for earlier ones
  - Lock-free `syncQueue` (SPSC ring buffer, capacity `SyncConcurrency = 4096`)
    for WAL fsync callbacks — the sync goroutine drains the queue and calls
    `WaitGroup.Done()` on all pending sync waiters at once (group sync)
- **Block cache**: `internal/cache` — `Cache` struct with `4×NumCPU` shards,
  each shard running **CLOCK-Pro** (patent-free approximation of LIRS/ARC).
  Cache key is `(handleID, fileNum, offset)`. Three eviction states per entry:
  `etHot` (recently re-accessed cold page), `etCold` (candidate for eviction),
  `etTest` (ghost entry — tracks recently evicted pages to detect re-access
  and promote to hot). `coldTarget` tracks the target cold fraction dynamically
- **Manual memory management in cache**: `cache.Value` is allocated with
  `C.malloc` (or Go allocator without cgo) outside the GC heap. Each `Value`
  is reference-counted (`refcnt`). The cache holds one ref; callers that receive
  a `*Value` from `Get` must call `value.Release()` when done. Memory is freed
  when refcount drops to zero. This **eliminates GC scan pressure** on the
  entire block cache — critical at CRDB scale (hundreds of GB of cache)
- **Read deduplication (`readShard`)**: Added in 2024. When multiple goroutines
  concurrently miss the same cache key, only one is given a `ReadHandle`
  (permission to do the I/O); others block in `waitForReadPermissionOrHandle()`.
  This prevents cache stampedes and was motivated by memory spikes from
  concurrent identical reads on CockroachDB
- **`manual` package**: Provides `Purpose`-tagged allocations
  (`BlockCacheMap`, `BlockCacheEntry`, `BlockCacheData`, `MemTable`). Each
  purpose has its own `atomic.Int64` counter (cache-line padded). This gives
  precise off-heap memory accounting without GC overhead

#### What HexxlaDB can borrow from Pebble

**Finding P-A: CLOCK-Pro is the right cache eviction algorithm (not LRU)**

Plain LRU is poor for scan workloads (a single range scan can evict the entire
working set). CLOCK-Pro keeps a ghost list (`etTest`) of recently evicted pages
— if a page is re-accessed after eviction, it is promoted directly to `etHot`
and `coldTarget` grows, shifting more budget toward cold-page protection.

For HexxlaDB, `AscendRange` scans over B+ tree leaf pages are the primary read
pattern. A scan will access many cold pages sequentially. Without a ghost list,
each scan pollutes the cache with pages that won't be reused. CLOCK-Pro's
`etTest` entries detect re-scanned ranges and give them hot status without
holding their data in memory between scans.

HexxlaDB's improvement #1 (page cache) should use CLOCK-Pro, not LRU. The
existing `internal/cache` package in this analysis is the Go reference
implementation — it can be ported directly to HexxlaDB's `internal/engine`
with the key adapted to `(pageID uint64)` instead of `(handleID, fileNum, offset)`.

**Finding P-B: Sharded cache mutex — `4×NumCPU` shards**

Pebble comments (`cache.go:106`) document the reasoning: at 2 shards/CPU,
lock contention is measurable in tail latencies. At 4 shards/CPU, contention
is negligible. The birthday-problem math means contention grows superlinearly
with CPU count. HexxlaDB's initial page cache can start with a small number of
shards but should not use a single global cache mutex.

The shard index is computed via Fibonacci hash:
`h = (id * m) ^ (fileNum * m) ^ (offset * m); shard = (h >> 32) * numShards >> 32`
where `m = 11400714819323198485` (Knuth's multiplicative constant). For
HexxlaDB, the key would be `pageID uint64` and the same hash applies.

**Finding P-C: Manual memory management for the cache is not needed for HexxlaDB**

Pebble's `manual.New`→`C.malloc` was motivated by CRDB running with hundreds
of GB of block cache. The GC scan of a standard Go map over millions of entries
caused stop-the-world pauses. HexxlaDB's typical cache size (tens of thousands
of pages) will not hit this threshold. The Go allocator with a standard map is
sufficient. However, the `Purpose`-tagged accounting pattern (`BlockCacheData`,
`MemTable` counters) is worth adopting for observability.

**Finding P-D: Commit pipeline — group WAL sync via lock-free syncQueue**

Pebble's `syncQueue` (SPSC lock-free ring, capacity 4096) collects sync
waiters and the WAL flusher calls `WaitGroup.Done()` on all of them at once
after a single `fdatasync`. HexxlaDB's group WAL batching in
`CommitWriteTxnBeginAsync` / `wait()` uses a `sync.WaitGroup` with a
time-bounded coalesce window — conceptually the same, but HexxlaDB's version
holds `db.mu.Lock()` for the entire window. Pebble releases `commitPipeline.mu`
immediately after the WAL write (memory copy), then applies the memtable
concurrently. HexxlaDB's B+ tree cannot safely do concurrent page writes, but
the WAL coalesce window could be shortened if the WAL write itself (the memory
copy into the WAL buffer) is separated from the B+ tree apply step.

**Finding P-E: WAL recycling — avoid `fallocate` overhead**

Pebble's `LogRecycler` keeps up to `MaxNumRecyclableLogs` (default 1) closed
WAL files for reuse. When a new WAL is needed, the recycler pops a file and
reuses it (overwriting from position 0 with a new log record header), avoiding
the `fallocate`/metadata-update overhead on ext4/XFS. HexxlaDB truncates and
re-creates the WAL file on every commit (WAL-truncate-per-commit model).
For high-frequency writes, the constant WAL file creation adds `fallocate`
cost. Keeping a single recycled WAL slot would amortize this cost at the
expense of a small amount of extra disk space (one WAL file's worth).

#### Pebble vs HexxlaDB comparison table

| Aspect             | Pebble                                      | HexxlaDB                              | Gap                                                                |
| ------------------ | ------------------------------------------- | ------------------------------------- | ------------------------------------------------------------------ |
| Cache algorithm    | CLOCK-Pro (ghost list)                      | CLOCK-Pro (ghost list), 4 MiB default | **Equivalent** — implemented                                       |
| Cache sharding     | 4×NumCPU shards                             | N/A                                   | Adopt when cache is added                                          |
| Cache memory       | Manual (`C.malloc`, off-heap)               | N/A                                   | Go allocator sufficient at HexxlaDB scale                          |
| Write batching     | Lock-free pipeline, parallel memtable apply | Group WAL coalesce under global lock  | Pebble more concurrent; HexxlaDB correct for single B+ tree writer |
| WAL sync           | Group fdatasync via SPSC lock-free queue    | Coalesce window + WaitGroup           | Equivalent semantics, different implementation                     |
| WAL file lifecycle | Recycled (avoid fallocate)                  | Truncate+recreate per commit          | Pebble wins for high write frequency                               |
| Read deduplication | readShard (cache stampede prevention)       | N/A                                   | Add if concurrent reads become a bottleneck                        |

### SQLite (B-tree engine, WAL mode)

- **Language**: C
- **Model**: Single-writer B-tree with WAL mode (Write-Ahead Log) for
  concurrent reads during a write
- **WAL pattern**: WAL is a flat append-only file. Readers use a shared-memory
  index (`WAL-index`) to find the latest version of each page without
  scanning the WAL linearly. Writers checkpoint the WAL back to the main
  file periodically
- **Relevance to HexxlaDB**:
  - HexxlaDB's WAL-truncate-per-commit is simpler than SQLite WAL but
    means readers cannot read during a write (they block on `RLock`)
  - SQLite WAL mode allows readers to read the last committed snapshot
    while a writer is active — by consulting the WAL index for any page
    modified by the active write. This is the only model where true
    concurrent reader+writer access works without MVCC or mmap COW
  - **Lesson**: If HexxlaDB ever needs reader+writer concurrency without
    full MVCC, SQLite WAL-index style is the path — a shared-memory or
    in-process index mapping `pageID → WAL offset` for dirty pages

### BoltDB lineage comparison table

| Database     | Write model         | Read model               | WAL                       | Page cache                              |
| ------------ | ------------------- | ------------------------ | ------------------------- | --------------------------------------- |
| bbolt        | COW, single-writer  | mmap snapshot            | None                      | OS mmap                                 |
| LMDB         | COW, single-writer  | mmap snapshot            | None                      | OS mmap                                 |
| SQLite WAL   | In-place + WAL      | WAL-index lookup         | Append-only, checkpointed | OS + optional page cache                |
| Badger       | LSM memtable        | Read timestamp watermark | Value log                 | Ristretto LRU                           |
| **HexxlaDB** | WAL redo + in-place | RWMutex snapshot         | Truncate-per-commit       | CLOCK-Pro sharded cache (default 4 MiB) |

---

## Consolidated Priority Order

| #   | Improvement                    | Priority | Effort  | Source                |
| --- | ------------------------------ | -------- | ------- | --------------------- | ------------------------------------------------------------ |
| 1   | CLOCK-Pro page cache           | **High** | Medium  | Pebble, bbolt, Badger | ✅ Done                                                      |
| 2   | Cached DB header               | **High** | Low     | Badger, bbolt, Pebble | ✅ Done                                                      |
| 3   | Configurable leaf fill percent | Medium   | Low     | bbolt                 | N/A — already max-fill                                       |
| 4   | WAL file recycling             | Medium   | Low     | Pebble                | ✅ Done                                                      |
| 5   | `IsClosed` atomic fast-path    | Low      | Trivial | Badger                | ✅ Done                                                      |
| 6   | Lazy rebalance on delete       | Low      | Medium  | bbolt                 | Deferred — structural change, low impact on primary workload |
| 7   | MVCC prune: single-pass scan   | Medium   | Low     | internal analysis     | ✅ Done                                                      |
| 8   | MVCC prune: batched commits    | Medium   | Low     | internal analysis     | ✅ Done                                                      |

Improvements 1–4 are confirmed by at least two independent production
codebase references. All are confined to `internal/engine` and `db.go`.
No public API changes are required for any of them.

---

## References

- Badger source: `/home/anon/Documents/GitHub/badger/` — `db.go`, `txn.go`, `batch.go`
- bbolt source: `/home/anon/Documents/GitHub/bbolt/` — `db.go`, `tx.go`, `node.go`, `bucket.go`
- Pebble source: `/home/anon/Documents/GitHub/pebble/` — `internal/cache/clockpro.go`, `commit.go`, `wal/log_recycler.go`
- HexxlaDB engine: `internal/engine/engine.go`, `internal/engine/btree_page.go`, `tx.go`, `group_wal.go`
