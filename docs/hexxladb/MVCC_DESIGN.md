# MVCC and `as_of` — design gate (Phase E)

**Status:** Design + implementation reference. **E2+ MVP is shipped** (format v2, `commit_seq`, `ViewAt(read_seq)`, version-suffixed cell/source/time/facet keys). Remaining follow-ons include version GC policy and wall-clock `time.Time -> read_seq` mapping.

**Normative product context:** [HEXXLA_DB.md](./HEXXLA_DB.md) (snapshots / `as_of`), [HEXXLA.md](./HEXXLA.md) (validity and retrieval). **Current engine:** [ENGINE_FORMAT.md](../../internal/engine/ENGINE_FORMAT.md), [ORDERED_STORE.md](../../internal/engine/ORDERED_STORE.md).

**Related:** Phase C single-version filters — [`record.ValidAt`](../../internal/record/validity.go), [`Tx.WalkRingAt`](../../primitives.go), [`Tx.LoadContextAt`](../../primitives.go) — interpret **stored** `ValidityWire` on one committed cell; MVCC adds **multiple committed versions per logical key** and snapshot-pinned reads.

---

## 1. Goals

- **`as_of` reads:** A read transaction sees a **consistent snapshot** of the database at a chosen **logical time** (see §2 for mapping to storage).
- **Snapshot isolation:** Readers do not observe partial writes from other transactions; writers do not block readers indefinitely (typical MVCC tradeoff: **version chains** + **GC**).
- **Durability:** Compatible with the existing **WAL + page** model or an explicitly evolved WAL (§5).
- **Migration:** Opening existing **v1 single-version** files must be a defined **`format_version`** bump (see [ENGINE_FORMAT.md](../../internal/engine/ENGINE_FORMAT.md) header).

---

## 2. Snapshot identity and `as_of` mapping

Two concepts must be distinguished:

- **Commit sequence:** monotonic `uint64` (or wider) assigned **in commit order** on `Update`. Unambiguous total order for visibility.
- **Wall-clock `as_of`:** user-facing instant (e.g. `time.Time` UTC). May align with transaction timestamps or validity semantics.

**Recommended v1 MVCC shape:**

- Store **`commit_seq`** on each write (or derive from WAL position).
- Maintain a **small mapping** (table or btree of segments) from **`commit_seq` → max wall-clock** observed at commit, **or** require callers to use **`commit_seq`** directly for `ViewAt` in the first ship. Pure wall-clock `ViewAt(asOf time.Time)` needs a defined resolution when no commit exists exactly at `as_of` (**largest `commit_seq` with commit_time ≤ as_of**).

**Open product choice:** Whether **`as_of`** is **ingestion time**, **validity**, or **commit time** for the snapshot — document in API when implementing Phase **E3**.

---

## 3. Where versions live (candidates)

The engine today stores **one value per btree key** ([`internal/engine/btree.go`](../../internal/engine/btree.go)). MVCC requires one of:

### Option A — Version suffix on logical keys

- Physical key = `logical_key || encode_uint64(commit_seq_desc)` so **latest** sorts first for prefix scans, or ascending with **visibility filter** on scan.
- **Pros:** Reuses one btree; range scans need careful design for “visible at seq.”
- **Cons:** Key length growth; tombstones / delete markers for MVCC deletes.

### Option B — Side chain per logical key

- Primary btree maps **logical key → head page id** of a **version chain** (linked list or small embedded list in page).
- **Pros:** Stable user-facing key size in primary index.
- **Cons:** More random I/O; chain traversal for visibility.

### Option C — Page-level multi-version

- Store multiple versions in **overflow** pages attached to a base page (engine-level, not btree-keyed per version).
- **Pros:** Can batch GC at page granularity.
- **Cons:** Highest implementation complexity; heavy coupling to allocator.

**Phase E1** prototyped **Option A** for **`cell/`** keys (see §9); Option B/C were not built. Measurements and rationale are in §8–§9.

---

## 4. Visibility rules (read path)

For a snapshot pinned at **`read_seq`**:

- A version written at **`commit_seq`** is visible iff **`commit_seq ≤ read_seq`** and it is the **latest** such version for that logical key that is not superseded by a delete tombstone **visible** at `read_seq`.

**Secondary indexes (`source/`, `time/`):** Product decision required:

- **Index-as-of snapshot:** Secondary entries must be versioned consistently with cells (same `commit_seq` semantics), **or**
- **Latest index, historical cell:** Allowed only if documented; generally **not** snapshot-isolated.

Phase **E4** must state the rule before wiring [`cell_secondary.go`](../../cell_secondary.go) to MVCC.

---

## 5. WAL and redo

Current WAL stores **full page images** ([ENGINE_FORMAT.md](../../internal/engine/ENGINE_FORMAT.md)). Options:

1. **Unchanged redo:** Each commit still writes **new page versions**; MVCC versions may live in **new pages** allocated per commit (copy-on-write style at page granularity).
2. **Logical WAL records (future):** Append **record-level** redo for smaller writes; larger format change.

**E0 recommendation:** Start with (1) unless record-level WAL is required for size; revisit after E1 measurements.

---

## 6. Garbage collection (GC)

Old versions are reclaimable when **no snapshot** can reference them:

- Track **oldest active read snapshot** (`min_read_seq`) across `View` / `ViewAt` handles.
- **GC job** (inline on commit, background goroutine, or periodic): delete version chain nodes / pages with **`commit_seq < min_read_seq`**, subject to retention policy.

**Retention:** Optional **time-based** minimum history (e.g. keep 24h of commits) even if no readers — product policy.

---

## 7. Migration from v1 (single version)

- Bump **`format_version`** in the file header when MVCC layout is introduced.
- **Tooling:** Offline migration (dump + reload) vs in-place upgrade — choose before E2.
- **Compatibility:** Older **`hexxladb`** versions must **refuse** unknown `format_version` (existing policy).

---

## 8. Decision log

- **Storage model (§3 A vs B vs C):** **E1 Option A prototyped**. Version suffix on `cell/` keys implemented in [`internal/mvccspike`](../../internal/mvccspike). B/C not prototyped in E1.
- **`ViewAt` by `commit_seq` vs `time.Time`:** **Resolved** — both are supported (`ViewAt` + `ViewAtTime`).
- **Secondary index versioning (§4):** **Resolved for current families** — source/time/seam secondaries are snapshot-visible.
- **WAL strategy (§5):** page-level redo remains default; E1 measured approximate 2x overhead for two physical writes vs one.

---

## 9. Phase E1 — Spike (recommended next step)

**Delivered (this repo):** a **two-version Option A** experiment — not wired to [`PutCell`](../../primitives.go) or public `View`.

- **Code:** [`internal/mvccspike/version_suffix_cell_key.go`](../../internal/mvccspike/version_suffix_cell_key.go) — `CellPhysicalKeyWithVersionSuffix`, `CellVersionSuffixScanBounds`, `SelectVisible`, tests that write two physical rows via raw [`Tx.Put`](../../tx.go) and resolve visibility by `read_seq` over an `AscendRange` scan.
- **Rationale:** Option A reuses the existing btree; prefix `cell/<packed>` is unchanged for logical identity, with an 8-byte suffix so version order matches byte order (ascending `commit_seq`). Range scans for “all versions of this cell” are a single bounded `AscendRange`. No separate overflow chains (Option B) or page-level MVCC (Option C) were built in E1.
- **Measurements (example, `go test -bench` on linux/amd64, will vary by machine):** `BenchmarkCellPhysicalKeyWithVersionSuffix_putTwoPhysicalRows` ~**411µs/op** vs `BenchmarkCellPhysicalKeyWithVersionSuffix_putOnePhysicalRow` ~**199µs/op** (~**2.1×**); bytes/op ~**935k** vs ~**467k** (~**2×**). Interprets as: **two versioned physical puts per logical commit** track ~2× the work of one put at this layer — consistent with expecting **higher WAL traffic** under full MVCC until logical WAL (§5) exists.

**Not done in E1:** plumbing `read_seq` through [`DB.View`](../../db.go) (optional alternative spike), Option B/C prototypes, `format_version` bump, secondary indexes — still **E2+**.

---

## 10. Phase E2+ — Implementation status

**Shipped in this repository:** format-v2 `commit_seq`, `ViewAt(read_seq)`, `ViewAtTime(time.Time)` wall-clock mapping, versioned primaries for cells/facets/seams, and snapshot-visible reads for source/time/seam secondary scans.

**Remaining hardening focus:** retention policy automation, long-run churn validation, and operator runbooks (see **[`HEXXLA_READINESS_ROADMAP.md`](./HEXXLA_READINESS_ROADMAP.md)**).

**Locked milestone sequence (order matters):**

- **E2a:** Format/header (`format_version`, persisted `commit_seq`, migration path). Exit: open/version tests.
- **E2b:** Engine version rows + WAL behavior + GC hooks/deferred sweep. Exit: crash/reopen and stale-version bounds.
- **E3:** Public API snapshots (`ViewAt`, `ViewAtTime`) with atomic `commit_seq` advance. Exit: race tests + docs.
- **E4:** Primitives + secondary index snapshot semantics. Exit: integration tests and roadmap sync.

**Out of scope for E2+ until re-scoped:** Phase **H** `embed/` keys; full **logical WAL** for MVCC (§5 optional follow-on).

- **E2:** Engine + format (multi-version storage, recovery, GC).
- **E3:** Public API (snapshot reads + commit sequencing).
- **E4:** Primitives + secondary indexes under snapshot rules.

---

## References

- Consolidated plan and remaining gaps: [HEXXLA_READINESS_ROADMAP.md](./HEXXLA_READINESS_ROADMAP.md).
- Checklist: [HEXXLA_DB_V1.md](../checklist/HEXXLA_DB_V1.md).
