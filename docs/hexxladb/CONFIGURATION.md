# Database Configuration

How to create and configure a HexxlaDB database.

---

## Opening a database

```go
db, err := hexxladb.Open("memory.db", &hexxladb.Options{
    // ... options
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

`Open` creates the file if it doesn't exist, or opens an existing one. On open it replays any pending WAL entries. Passing `nil` for options uses defaults (plaintext MVCC, embedding dimension auto-detected on first write, 4 KiB pages).

---

## Options reference

All fields on `hexxladb.Options`. Fields marked **immutable** are persisted in the file header on creation and cannot be changed on reopen.

### Core

| Field           | Type            | Default | Immutable | Description                                                                                                                                           |
| --------------- | --------------- | ------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MVCCRetention` | `MVCCRetention` | zero    | no        | Policy for `SuggestedPruneBeforeSeq`. `RetainCommitsBehindHead` sets the suggested cutoff relative to head. A caller or scheduler applies pruning.                                                           |
| `PageSize`      | `uint32`        | `4096`  | yes       | B+ tree page size. Accepted: `4096`, `8192`, `16384`, `65536`. Larger pages = fewer I/O ops for big values; smaller = less waste for small DBs.       |
| `MaxValueBytes` | `uint32`        | `8192`  | no        | Max encoded value size per B+ tree entry. Accepted: 512 to 1048576 (powers of 2). A non-zero value on reopen updates the persisted limit for subsequent writes; existing values are unchanged. |
| `PageCacheSize` | `int64`         | `0`     | no        | In-process B+ tree page-cache budget. `0` selects 4 MiB, positive values set bytes, and `-1` disables the cache.                                   |

### Embeddings

| Field                | Type             | Default          | Immutable | Description                                                                                                                                                                                                                                                                                                                   |
| -------------------- | ---------------- | ---------------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `EmbeddingDimension` | `uint16`         | `0`              | yes       | Optional pre-set vector dimension. When `0` (default), the dimension is **auto-detected from the first `PutEmbedding` call** and persisted in the file header. When non-zero, locked at creation time. Either way, once set all vectors must match. Common values: `384` (all-MiniLM), `768` (BERT), `1536` (OpenAI ada-002). |
| `DistanceMetric`     | `DistanceMetric` | `DistanceCosine` | yes       | Similarity function. Set it with a nonzero `EmbeddingDimension` at creation. If the first vector establishes the dimension, the metric defaults to cosine.                                                                                                                                                                                                                         |

A zero `EmbeddingDimension()` means the dimension is unset. An empty database can have a nonzero dimension configured at creation.

Ordinary `PutEmbedding` maintains a current HNSW graph synchronously. Detected
disconnection switches retrieval to exact scoring until a successful rebuild.
For bounded bulk loads,
`PutEmbeddingWithOptions` can store authoritative vectors with
`DeferIndexMaintenance`; exact flat search remains available until
`RebuildEmbeddingIndex` publishes a complete graph. Rebuild defaults to 10,000
vectors and a conservative 2 GiB memory/transient-WAL budget, refuses more than
20,000 vectors, and checks available filesystem capacity before invalidating
the current graph. Override `EmbeddingIndexRebuildOptions.MaxVectors` and
`MaxMemoryBytes` only within the documented hard bound and after representative
evidence.

**Distance metrics:**

| Constant             | Behaviour                                                                                       |
| -------------------- | ----------------------------------------------------------------------------------------------- |
| `DistanceCosine`     | Cosine similarity. Range \[-1, 1\], higher = more similar. Best for normalized text embeddings. |
| `DistanceDotProduct` | Raw dot product. Assumes pre-normalized vectors.                                                |
| `DistanceL2`         | Euclidean distance. Lower = more similar (inverted internally for ranking).                     |

### Encryption

| Field           | Type     | Default | Description                                                                                                                                                  |
| --------------- | -------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `EncryptionKey` | `[]byte` | `nil`   | Creates new databases with authenticated XChaCha20-Poly1305 format v3. Use ≥128 bits of entropy. Mutually exclusive with `Passphrase`. |
| `Passphrase`    | `string` | `""`    | Argon2id plus the per-database salt feeds the same authenticated v3 key hierarchy. Mutually exclusive with `EncryptionKey`. |

Authenticated v3 adds 48 physical bytes per logical data page. `PageSize`
remains the logical B+ tree capacity; inspect `StorageStats.PhysicalPageSize`
for the on-disk stride. Engine v1 and AES-XTS files require [offline conversion](./OPERATIONS.md#offline-conversion-of-retired-files).

### Changelog

| Field              | Type     | Default | Description                                                                                                                                       |
| ------------------ | -------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ChangelogEnabled` | `bool`   | `false` | Maintain a recoverable logical changefeed backed by durable primary outbox intents. Official database encryption selects authenticated format v2. |
| `ChangelogPath`    | `string` | `""`    | Override path. Empty = `<dbpath>-changelog`. The file must match the database's encryption mode and key binding.                                  |
| `ChangelogLazy`    | `bool`   | `false` | Defer sidecar fsync; durable primary intents are acknowledged after 256 pending records or clean close. Crash recovery may redeliver them.        |

### Hooks

| Field           | Type               | Description                                                                                                                                       |
| --------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CellValidator` | `CellValidator`    | Pre-write validation. Return non-nil error from `ValidateCell(rec)` to reject a `PutCell`. Use for content limits, required tags, business rules. |
| `AfterPutCell`  | `AfterPutCellHook` | Runs synchronously after a cell is staged and before commit. A hook error aborts the update.                              |
| `AfterPutSeam`  | `AfterPutSeamHook` | Runs synchronously after staging a seam. Return its error from the update callback to abort.                           |

Convenience adapters: `CellValidatorFunc`, `AfterPutCellHookFunc`, `AfterPutSeamHookFunc` wrap plain functions.

### Durability tuning

| Field                  | Type            | Default | Description                                                                                                                  |
| ---------------------- | --------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `UsePrimaryFdatasync`  | `bool`          | `false` | Use `fdatasync(2)` instead of `fsync(2)` on the primary file (Linux). See `DURABILITY.md`.                                   |

## Common configurations

### Minimal (defaults)

```go
db, _ := hexxladb.Open("data.db", nil)
```

No MVCC, embedding dimension auto-detected on first vector write, 4 KiB pages, 8 KiB max value. Good for simple key-value usage.

### LLM memory with embeddings

```go
db, _ := hexxladb.Open("memory.db", &hexxladb.Options{
    EmbeddingDimension: 384,
    DistanceMetric:     hexxladb.DistanceCosine,
    PageSize:           4096,
    PageCacheSize:      64 << 20,
})
```

MVCC for time-travel and snapshot diffing. For the measured deferred HNSW envelope (20,000×32d and 10,000×384d), 4 KiB pages avoid random graph-maintenance amplification and a 64 MiB cache keeps hot B+ tree pages resident. Benchmark representative data before changing either value; larger pages can still help workloads dominated by large sequential values rather than embeddings.

### Production with full features

```go
db, _ := hexxladb.Open("prod.db", &hexxladb.Options{
    EmbeddingDimension: 384,
    DistanceMetric:     hexxladb.DistanceCosine,
    PageSize:           4096,
    PageCacheSize:      64 << 20,
    MaxValueBytes:      65536,
    ChangelogEnabled:   true,
    Passphrase:         os.Getenv("HEXXLA_PASSPHRASE"),
    MVCCRetention:      hexxladb.MVCCRetention{RetainCommitsBehindHead: 1000},
    CellValidator: hexxladb.CellValidatorFunc(func(rec hexxladb.CellRecord) error {
        if len(rec.RawContent) > 100_000 {
            return fmt.Errorf("content too large: %d bytes", len(rec.RawContent))
        }
        return nil
    }),
    AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, rec hexxladb.CellRecord) error {
        slog.Info("cell written", "key", rec.Key, "tags", rec.Tags)
        return nil
    }),
})
```

### Encrypted (raw key)

```go
key := make([]byte, 32)
if _, err := rand.Read(key); err != nil {
    log.Fatal(err)
}
db, _ := hexxladb.Open("encrypted.db", &hexxladb.Options{
    EncryptionKey: key,
})
```

---

## Persisted and per-open fields

- **Engine format and page size** are fixed when the database is created. Use [offline conversion](./OPERATIONS.md#offline-conversion-of-retired-files) for retired formats. Compaction preserves the source format.
- **Embedding dimension and metric** become fixed together when configured at creation or detected on the first `PutEmbedding`. A conflicting non-zero embedding configuration on reopen returns `ErrInvalidArgument`.
- **`MaxValueBytes` is persisted but mutable.** Passing a supported non-zero value on reopen updates the stored limit for subsequent writes. Existing values are not rewritten, and lowering the limit does not remove them.
- **Encryption mode and credentials** cannot be changed through ordinary `Open`; use `RotateEncryption` for an offline replacement.
- **Per-open fields** such as hooks, changelog selection, MVCC retention, page-cache budget, and durability tuning take effect for that handle and can change between opens subject to their documented compatibility checks.

---

## Runtime accessors

After opening, read back configuration:

```go
db.PageSize()           // uint32 — active page size
db.MaxValueBytes()      // uint32 — max value size
db.EmbeddingDimension() // uint16 — vector dimension (0 = no embeddings stored yet)
db.EmbeddingMetric()    // DistanceMetric — distance function
```

---

## See also

- [`API_REFERENCE.md`](API_REFERENCE.md) — task-oriented public API guide
- [`OPERATIONS.md`](OPERATIONS.md) — production operations, backup, compaction, benchmarks
- [`HEXXLA_DB.md`](HEXXLA_DB.md) — storage layout, key encoding
