# Performance audit — throughput, allocations, parallelism, bottlenecks

**Audience:** Engineers optimizing latency or throughput for HexxlaDB, or agents proposing performance work.

**Date:** 2026-04-23

**Companion:** [`CODEBASE_LEAN_AUDIT.md`](./CODEBASE_LEAN_AUDIT.md) (deduplication / structure). This document focuses on **runtime behavior**, **measured hotspots**, and **where parallelism does or does not apply**.

---

## 1. Product & execution model (why “faster” looks like this)

HexxlaDB is an **embedded** engine: **one primary file**, **redo WAL**, **64 KiB pages**, **B+tree** ordered store (`internal/engine`). The public **`DB`** (`db.go`) wraps **`sync.RWMutex`**:

- **`View` / `ViewAt` / `ViewAtTime`**: `RLock` for the lifetime of the callback (`tx.go`). Multiple goroutines **may** run **`View`** concurrently **when no writer holds the lock**.
- **`Update` / `Batch`**: exclusive `Lock` — **no** concurrent `View` or other writers.

So **parallel readers** are allowed at the mutex level; they still contend on **`RWMutex`** (cheap for many readers, one rare writer). The deeper limits are **disk / OS page cache**, **per-page encryption**, and ** btree page walks**.

The engine is **single-writer by design** (Bolt-style transactional shell). **Internal parallel workers** mutating one shared **`BTree`** safely would require a redesign (not incremental).

---

## 2. Dominant bottlenecks (deepest first)

### 2.1 Durability path — `WritePage` (`internal/engine/engine.go`)

Every btree page mutation that persists goes through **`WritePage`**, which roughly:

1. Applies optional encryption hook on the plaintext page.
2. Encodes WAL record, **`wal.Write`**, **`wal.Sync()`**.
3. **`db.WriteAt`** to the primary file.
4. **`db.Sync()`**.
5. **`readHeaderAt`**, mutate **`LastWALSeq` / `NextPageID`**, **`writeHeaderAt`**, **`db.Sync()`**.

This is **intentionally crash-safe** and inherently **serialized**. **Throughput ceiling** for writes is dominated by **fsync latency** × number of **page touches** per logical operation.

**Implications:**

- **`PutCell`** with MVCC + secondaries triggers **many** btree **`Put`**/**`Delete`** operations → many durable steps per commit (see **`primitives.go`**, **`cell_secondary.go`**).
- **`Batch`** is documented as **`Update`** — there is **no separate group-commit** pipeline that amortizes WAL sync across unrelated transactions today.

**Optimization vectors (large effort):**

- Group commit / deferred WAL flush (risky; must preserve crash semantics).
- Fewer header re-reads during commit if batched (micro gains vs fsync).

---

### 2.2 Page reads — pooled engine buffers (`internal/engine/engine.go`)

Previously, each read did **`make([]byte, PageSize)`** (64 KiB). The btree now uses **`Engine.readPagePooled`**: a **`sync.Pool`** backs the ciphertext read buffer, **`transformRead`** returns it or a hook-allocated slice, and callers **`release`** when finished so nested/recursive walks keep distinct physical pages. Exported **`ReadPage`** returns an owned copy (`slices.Clone`) for callers outside the btree; **`internal/engine`** btree/delete paths call **`readPagePooled`** directly.

Microbenchmark **`B/op`** drops materially after pooling ( **`docs/hexxladb/BENCHMARKS.md`** sample refreshed 2026-04-22 ); **`allocs/op`** on btree microbenches is still dominated by parsing / leaf bookkeeping.

**Remaining:** **`AfterRead`** may still allocate a distinct plaintext slice; when it does, the ciphertext slab is returned to the pool immediately.

---

### 2.3 Repeated header reads on btree operations (`internal/engine/btree.go`)

**`BTree.Get`** still reads **`ReadHeader()`** when invoked directly (writers and tools). Read-only **`Tx`** paths (**`Tx.Get`**, **`getCellVisibleRaw`** v1) call **`GetUsingRoot`** with **`cachedBTreeRoot`** captured when **`View`** / **`ViewAt`** / **`ViewAtTime`** opens, skipping a per-lookup header read. **`Put`** continues to **`ReadHeader()`** up front; **`AscendRange`** reads the header once.

---

### 2.4 Encryption (`internal/engine/hooks.go`, options in `options.go` / open path)

At-rest encryption uses **`AfterRead` / `BeforeWrite`** at the page boundary. Reads pay **CPU** for decrypt; writes pay **encrypt +** full durability chain.

Existing benchmarks: **`BenchmarkAPI_GetCell_Encrypted`**, **`BenchmarkAPI_GetCell_MVCC_Encrypted`** (`api_bench_test.go`). Tune with **hardware AES** / Go crypto in mind.

---

### 2.5 MVCC and secondaries

- **MVCC** adds version-suffixed keys and **`__meta/commit-time`** rows (`tx.go` **`Update`** path) — **more btree work** per commit than v1.
- **Secondary indexes** (`cell_secondary.go`, `seam_secondary.go`) add **additional** **`Put`/`Delete`** per logical write.
- **Scans** (`AscendCellsBySource`, etc.) walk a range then **`GetCell`** / seam decode — **O(result)** btree/CPU work, sequential in one **`View`**.

**Pruning** (`mvcc_lifecycle.go`, etc.) is **maintenance** cost; tune retention to avoid pathological prune volume (product/ops concern).

---

## 3. Parallelization — what actually helps

| Idea | Fits this codebase? | Notes |
|------|---------------------|-------|
| **Parallel `View` goroutines** (many readers) | **Yes** | `RWMutex` permits concurrent views; **`BenchmarkAPI_ViewUpdateContention`** exercises mixed read/write. Scale is still limited by **lock**, **disk cache**, and **CPU** (decrypt). |
| **Parallel writes to one `DB`** | **No** (current design) | Single writer mutex + btree + WAL serializes mutations. |
| **Shard by multiple DB files / paths** | **Yes** (application layer) | Scale-out throughput by partitioning keys across **separate `Open` instances** — each has its own WAL and lock. |
| **Parallel batch load into one DB** | **Only if serialized** | Use **one `Update`** with many **`PutCell`** calls (single commit), not concurrent **`Update`** on same handle. |
| **Parallel changelog consumers** | **Yes** (outside engine) | Logical log is **ordered**; workers can process ** disjoint key ranges** only if semantics allow and **idempotency** holds (`docs/hexxladb/CHANGEFEED.md`). |
| **Parallel tree scan inside one `Tx`** | **Generally no** | Range scan callback is single-threaded; splitting ranges across goroutines would need **immutable snapshots** + careful API — not present. |

**Takeaway:** “Blazingly fast” single-process **writes** usually means **fewer btree touches**, **fewer fsyncs per logical op**, or **sharding**. “Blazingly fast” **reads** means **fewer allocations** on **`ReadPage`**, cheaper decrypt, OS cache warmth, and **not** blocking readers behind writers except during **`Update`**.

---

## 4. API-layer hotspots (callers pay these costs)

- **`WalkRing` / `LoadContext`**: many coordinate steps → repeated **`GetCell`** / btree **`Get`**.
- **`AscendCellsBySource` / `AscendCellsByTag`**: range scan + parse + **`GetCell`** + dedupe maps (`cell_secondary.go`) — **CPU + heap** for `seen` / `processed` maps on large scans.
- **`context.Context` checks** in hot loops: small but non-zero; keep **`ctx`** cancelable for long scans (good for **latency** under load, not raw ns/op).

---

## 5. Measurement & regression (already in-repo)

Authoritative inventory: **`docs/hexxladb/BENCHMARKS.md`**.

**Run:**

- `make bench` or targeted `go test -bench=. -benchmem ./internal/engine ./.` (see doc for **`HEXXLA_BENCH_PRELOAD`**).
- **`make bench-stress`** for longer API sub-benches.
- **`make integration`** / stress for MVCC churn, not microbench.

**Gaps called out in BENCHMARKS:** combined write-heavy MVCC+encrypt scenarios; contention bench uses raw **`Tx.Put`** not **`PutCell`** — interpret as **mutex + WAL**, not full MVCC secondary churn.

**Suggested additions if chasing wins:**

- Bench **`PutCell`** vs **`PutCell`** MVCC with **same** preload size side-by-side on CI artifacts only (optional).
- Bench **`AscendCellsBySource`** with **`cells_10000`** after **`ReadPage`** pooling to validate alloc regression.

---

## 6. Prioritized opportunity list

| Priority | Area | Kind | Expected gain |
|----------|------|------|----------------|
| P0 | `Engine.ReadPage` | **Allocation** (`sync.Pool` / scratch) | Large drop in **`B/op`**, better cache behavior |
| P1 | `BTree` + `Tx` | **Avoid redundant `ReadHeader`** inside same snapshot | Moderate CPU / syscall reduction on **`Get`**-heavy workloads |
| P2 | Record encode/decode (`internal/record`) | **Reuse buffers** where safe | Moderate on write-heavy **`PutCell`** |
| P3 | Durability | **Group commit / WAL batching** | High throughput potential, **high engineering + correctness risk** |
| Ops | Deployments | **Sharding** multiple DBs | Linear throughput scaling with partitioning |
| — | Single-DB parallel mutation | — | **Not applicable** without new concurrency model |

**Status (2026-04-22):** **P0** btree paths use **`readPagePooled`** (exported **`ReadPage`** clones for safety). **P1** **`Tx.cachedBTreeRoot`** + **`BTree.GetUsingRoot`** on read-only **`Tx.Get`**. **P2** **`EncodeCell`** uses **`cellPayloadScratch`** for payload growth. **P3** unchanged (deferred).

---

## 7. Non-goals (avoid spending time here for “speed”)

- Turning **`View`** into lock-free arbitrary concurrent btree readers without **read consistency** guarantees — current model is intentional.
- Adding **`GOMAXPROCS`**-style parallelism **inside** one **`Update`** callback — **`Tx`** is not safe for concurrent use.
- Micro-optimizing **`internal/lattice`** before engine I/O — lattice is already **nanoseconds / zero alloc** per **`BENCHMARKS.md`** sample; **prove I/O bound** first.

---

## 8. Key files for implementers

| Concern | Path |
|--------|------|
| DB locking, `View`/`Update` | `tx.go`, `db.go` |
| Page I/O, WAL, fsync | `internal/engine/engine.go`, `internal/engine/wal.go` |
| B+tree walks | `internal/engine/btree.go` |
| Public hot API | `primitives.go`, `cell_secondary.go`, `seam_secondary.go` |
| Benchmarks | `api_bench_test.go`, `internal/engine/btree_bench_test.go` |
| Methodology doc | `docs/hexxladb/BENCHMARKS.md` |

---

## 9. Living document

After major engine or API changes, re-run **`make bench`** (same machine / `TMPDIR`) and refresh §2 baseline narrative if allocation profiles shift materially.

**2026-04-22:** Re-ran btree microbenchmarks after **`readPagePooled`** + **`GetUsingRoot`** / **`EncodeCell`** scratch pooling; **`docs/hexxladb/BENCHMARKS.md`** sample row for engine **`B/op`** updated on this machine (`linux/amd64`).
