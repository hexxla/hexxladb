# HexxlaDB v1 — implementation checklist (foundations)

**Audience:** Engineers and coding agents implementing the first embedded-engine wave.
**Normative product spec:** [`docs/hexxladb/HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) (keyspace, objects, primitives, locked architecture). This checklist records **engineering commitments** and **build order**; it does not replace the spec.

**Hexagonal wiring for the service in this repo:** [`docs/context/HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md) — ports in `internal/domain` / `internal/app`, adapters in `internal/adapters/...`, composition in `cmd/...`.

## How to use

- Work **top to bottom** where dependencies exist (lattice before I/O-heavy engine; durability before optional encryption).
- Tick boxes as items land in `main`; keep the spec linked when behavior is ambiguous.
- The **ordered implementation plan** (milestones, phases, folder layout) is **[`docs/hexxladb/DEVELOPMENT_ROADMAP.md`](../hexxladb/DEVELOPMENT_ROADMAP.md)**; it references this file as the checklist source.

---

## Seed selection vs HexxlaDB (product boundary)

Hexxla separates **approximate seed selection** (how you pick a starting `Coord`) from **deterministic lattice work** (everything after the seed). **Embeddings are optional** and only one way to obtain a seed; alternatives include **explicit coordinates**, **lexical / tag / `source_id` discovery**, and **agent-driven navigation**—see **[`HEXXLA.md`](../hexxladb/HEXXLA.md)** (Retrieval and Context Orchestration).

- [ ] **HexxlaDB v1** does **not** depend on embeddings: **no vector columns**, **no ANN indexes** in the engine; all keys and core operations use **`PackedCoord`**, tags, provenance, validity, seams, and related non-vector indexes per spec.
- [ ] The optional **`embed/<partition>/<vector_ref>`** key in [`HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) Storage Layout is **reserved for future hybrid use**—not required to ship or use v1.
- [ ] **Keep seed discovery outside** the stable `hexxladb` surface: orchestration lives in the **service / app layer** (or a higher-level package); the DB API accepts coordinates and runs **`WalkRing`**, **`LoadContext`**, **`FindSeams`**, etc.
- [ ] Do **not** add engine primitives like **`FindSeedsByEmbedding`** on the v1 **public** API unless a milestone explicitly requires it; prefer optional helpers **above** the engine.

---

## 1. Locked constraints (from spec)

- [ ] **Hex-native embedded engine** — custom on-disk format, crash recovery, embeddable in Go; lattice operations are primitives, not translated queries over a generic KV/SQL core ([`HEXXLA_DB.md` § Architecture Position](../hexxladb/HEXXLA_DB.md)).
- [ ] **Morton (Z-order) `PackedCoord` keyspace** — ring walks and neighborhood loads as native prefix/range scans over that keyspace.
- [ ] **No SQLite**; **no third-party ordered-KV or SQL engine as the storage core** (e.g. Pebble, RocksDB, SQLite).
- [ ] **Edge vs Seam** — distinct logical concepts and **distinct storage families**; read aggregates vs primary storage per spec.
- [ ] **MVCC path** — design transactions and snapshots for future `as_of`; **full MVCC not required in v1** (see Concurrency below).

---

## 2. Module and package layout

- [x] **Module:** `github.com/hexxla/hexxladb` (see root `go.mod`).
- [x] **Stable API:** `package hexxladb` at the **module root** (top-level `.go` files next to `go.mod`) so importers use `import "github.com/hexxla/hexxladb"`.
- [x] **Implementation:** `internal/engine`, `internal/record`, `internal/index` (and optional `internal/lattice` if not all lattice types live in the root package).
- [x] **`pkg/`** — not required for v1; add under [`HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md) guidelines only if a second stable surface is needed later.

---

## 3. Public API v1 (freeze list)

- [x] `Open(path string, opts *Options) (*DB, error)`
- [x] `(*DB) Close() error`
- [x] **Bolt-style** `View(func(*Tx) error) error` and `Update(func(*Tx) error) error` (not `Batch`-primary for v1); **`Batch`** aliases **`Update`** — see [`docs/hexxladb/TX.md`](../hexxladb/TX.md).
- [x] **Minimum primitives:** `PutCell`, `WalkRing`, `FindSeams` (aligned with [`HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) Native Query Primitives). M7: **`seam-by-cells`** secondary maintained on [`PutSeam`](../../primitives.go); [`FindSeams`](../../primitives.go) uses indexed lookups (ball × secondary ranges).
- [x] **Early (memory model):** `LoadContext`, `ResolveSeam` — M6 ships minimal `LoadContext` (concentric rings, `maxCells` cap) and `ResolveSeam` on primary seam keys; token-budget policy refinements can follow without breaking the API shape.
- [ ] **Defer** heavy **Edge** linking APIs if they threaten v1 scope.
- [x] Exported **`Coord`**, **`PackedCoord`**, and lattice helpers (`Distance`, neighbors, ring iteration — see Resolved decisions).
- [x] **Stable errors:** sentinel values (e.g. invalid coord, seam/facet conflicts) with **`errors.Is` / `errors.As`**; no opaque strings for stable cases — M6–M7 add [`ErrSeamNotFound`](../../errors.go), [`ErrInvalidArgument`](../../errors.go), [`ErrSeamEndpointMismatch`](../../errors.go) alongside existing M5 sentinels.
- [ ] Read-only view types as needed (e.g. `CellView`, `ContextPack` — names TBD to match spec).

---

## 4. Lattice (do first — no I/O)

- [x] **Stdlib only** — no disk, no drivers in lattice code.
- [x] **`PackedCoord`** — fixed 128-bit representation: **`[2]uint64`** or `struct { Hi, Lo uint64 }`; **not** `math/big.Int` on hot paths.
- [x] Zigzag + Morton interleaving per [`HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) Coordinate Packing Scheme (see [`internal/lattice/PACKED_COORD.md`](../../internal/lattice/PACKED_COORD.md)).
- [x] Hex distance and neighbor rules — align with **[`HEXXLA.md`](../hexxladb/HEXXLA.md)** (Geometric Model).
- [x] **Tests:** distance symmetry; **shell** at ring \(k\): **\(6k\)** cells; **ball** up to radius \(R\): **\(1+3R(R+1)\)** cells; boundary behavior; **canonical ring iteration** (documented + golden tests; see Resolved decisions).

---

## 5. Records (`internal/record`)

- [x] **Versioned envelope** per record family: magic + `format_version` + length + payload (encoding/binary or fixed layouts on hot paths).
- [x] **Forward-only** migrations within a major version; **reject** unknown **newer** `format_version` in v1 until migration tooling exists.
- [x] Separate serialization for **Cell, Facet, Edge, Seam**; **Seam** IDs as **ULID** per spec.
- [x] **Facet updates** — enforce **derivation hash** (e.g. SHA-256 of raw content) per product rules.

---

## 6. Engine skeleton (`internal/engine`)

- [x] **Page size:** **64 KiB**, **compile-time constant** for v1 (see [`internal/engine/ENGINE_FORMAT.md`](../../internal/engine/ENGINE_FORMAT.md)).
- [x] **Files:** **single primary database file** + **separate WAL file** (`{primary}-wal`).
- [x] **Header page:** magic, format version, `last_wal_seq`, `next_page_id` (freelist/bitmap deferred to M4+); see **ENGINE_FORMAT.md**.
- [x] **WAL:** append-only **redo** entries; **replay on `Open`**; format in **ENGINE_FORMAT.md**.
- [x] **Page I/O abstraction** — [`PageHooks`](../../internal/engine/hooks.go) (optional `BeforeWrite` / `AfterRead`); default plaintext.
- [x] **Ordered store (M4)** — B+ tree on engine pages; **`cell/`** byte keys in [`internal/index`](../../internal/index); spec in [`ORDERED_STORE.md`](../../internal/engine/ORDERED_STORE.md).

---

## 7. Concurrency

- [x] **Single writer, multiple readers** for v1 — [`DB`](../../db.go) uses **`sync.RWMutex`** ([`docs/hexxladb/TX.md`](../hexxladb/TX.md)).
- [x] **Writer path isolated** (`RWMutex`: `Update` exclusive, `View` shared) — future **`as_of`** can add snapshot pinning without breaking the API shape.
- [x] **No full MVCC in v1** — **`View`/`Update`/`Tx`** present; versioned snapshots later.

---

## 8. Encryption (designed seam; optional in v1)

- [x] Hooks at **page read/write** boundary — [`PageHooks`](../../internal/engine/hooks.go) (`pageID`, plaintext in / out).
- [x] **Default: plaintext** on disk unless [`Options`](../../options.go) enables encryption ([`docs/hexxladb/ENCRYPTION.md`](../hexxladb/ENCRYPTION.md)).
- [x] When enabled: **AES-256-XTS** (tweak = page id) — length-preserving, **pure Go** (`golang.org/x/crypto/xts`).
- [x] **Key input:** [`Options.EncryptionKey`](../../options.go); passphrase → **Argon2id** via [`Options.Passphrase`](../../options.go) / [`DeriveKeyFromPassphrase`](../../encryption.go).
- [x] **Threat model (v1):** documented in [`ENCRYPTION.md`](../hexxladb/ENCRYPTION.md) — at-rest ciphertext; **runtime memory** deferred.
- [x] **WAL policy:** WAL records store **ciphertext** (same bytes as primary **after** hooks); documented in [`ENCRYPTION.md`](../hexxladb/ENCRYPTION.md).

---

## 9. Hexagonal service integration (this repository)

- [x] Outbound adapter [`internal/adapters/out/hexxladb`](../../internal/adapters/out/hexxladb) implements [`domain.Storage`](../../internal/domain/storage.go) via **`hexxladb` public API** only.
- [x] **`cmd/hexxladb`** opens **`hexxladb.DB`**, constructs the adapter, **`app.NewWithStorage`**, and runs a PutCell/GetCell smoke when **`HEXXLA_DB_PATH`** is set ([`HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md)).

---

## 10. Testing

- [x] **Fast unit tests:** lattice, record encode/decode, **engine B+ tree + index keys**; bounded temp-file tests for WAL/btree (default `go test ./...` stays fast).
- [x] **Integration tests:** real temp files, WAL replay on **`Open`** ([`db_durability_test.go`](../../db_durability_test.go)); optional stress (`make integration`, [`durability_integration_test.go`](../../durability_integration_test.go)). **Chaos** (kill during write) still deferred.
- [x] Use **build tags** (`//go:build integration`) if integration tests must stay out of default `go test ./...` — see **`make integration`** in [CONTRIBUTING.md](../CONTRIBUTING.md).
- [x] **Benchmarks** (`make bench`) and **fuzz** decoders (`make fuzz` / `go test -fuzz=...`) — see [CONTRIBUTING.md](../CONTRIBUTING.md); milestone **M10** in [DEVELOPMENT_ROADMAP.md](../hexxladb/DEVELOPMENT_ROADMAP.md).

---

## 11. Explicit deferrals (v1 non-goals)

- [ ] Federation, replication, consensus — **out of v1**; one keyspace per process is acceptable for years.
- [ ] Directory-of-SSTables / leveled compaction — **later**, unless benchmarks force a minimal step.
- [ ] **Vector storage, ANN indexes, and embedding-backed seed APIs inside the engine** — **out of v1**; optional `embed/` keyspace is for **future** hybrid indexing only (see [Seed selection vs HexxlaDB](#seed-selection-vs-hexxladb-product-boundary)).
- [ ] **Secondary index primitives** (`tag`, `source_id`, etc.) **not** on the stable v1 **public** API unless a milestone explicitly requires them — keep in **`internal/index`** keyed by [`HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) Storage Layout.

---

## 12. Optional / TBD (implementation plan may decide)

- [ ] **`Changefeed` / event stream** — add only if **zero overhead when disabled**; otherwise defer and document extension point.
- [ ] **WAL encryption vs plaintext WAL** — final policy if encrypt-everything proves too costly for recovery (must be documented).

---

## Resolved decisions (owner / co-design)

| Topic | Decision |
| --- | --- |
| Embeddings / seed selection | **Optional** product concern; **outside** v1 `hexxladb` API by default. Engine is spatial + relational (seams/edges) + temporal—no embedding dependency. |
| Transaction API | **Bolt-style `View` / `Update` + `*Tx`** for v1; not `Batch`-primary. |
| `PackedCoord` | **`[2]uint64` or `{Hi, Lo uint64}`**; not `math/big` on hot paths. |
| Ring order | **One canonical iteration** — rings outward from center; within-ring order fixed and **tested** (align with [`HEXXLA.md`](../hexxladb/HEXXLA.md) `load_context` ordering). |
| Index API | **Spatial + seam flows** on public API; **tag/source** and similar **internal** until a milestone demands. |
| On-disk files | **Single main file + separate WAL file** for v1. |
| Page size | **64 KiB**, compile-time constant for v1. |
| Format compatibility | **Forward-only** within major; **major bump** = new magic and/or migration tool / new directory layout. |

---

## Cross-reference summary

| Document | Role |
| --- | --- |
| [`docs/hexxladb/HEXXLA_DB.md`](../hexxladb/HEXXLA_DB.md) | Product and storage specification (normative). |
| [`docs/hexxladb/HEXXLA.md`](../hexxladb/HEXXLA.md) | Hexxla memory model, geometry, retrieval (product the DB supports). |
| [`docs/hexxladb/DEVELOPMENT_ROADMAP.md`](../hexxladb/DEVELOPMENT_ROADMAP.md) | Phases, milestones, folder layout, implementation sequencing. |
| [`docs/context/HEXAGONAL_ARCHITECTURE.md`](../context/HEXAGONAL_ARCHITECTURE.md) | Ports, adapters, `cmd` composition for this repo’s service. |
