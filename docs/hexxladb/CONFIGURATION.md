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

`Open` creates the file if it doesn't exist, or opens an existing one. On open it replays any pending WAL entries. Passing `nil` for options uses defaults (no MVCC, embedding dimension auto-detected on first write, 4 KiB pages).

---

## Options reference

All fields on `hexxladb.Options`. Fields marked **immutable** are persisted in the file header on creation and cannot be changed on reopen.

### Core

| Field           | Type            | Default | Immutable | Description                                                                                                                                           |
| --------------- | --------------- | ------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `EnableMVCC`    | `bool`          | `false` | yes       | Format v2 with snapshot isolation, time-travel (`ViewAt`), and version history. Required for `SnapshotDiff`, `PruneCellVersions`, `ViewAtTime`.       |
| `MVCCRetention` | `MVCCRetention` | zero    | no        | Policy for `SuggestedPruneBeforeSeq`. `RetainCommitsBehindHead` keeps N commits of history.                                                           |
| `PageSize`      | `uint32`        | `4096`  | yes       | B+ tree page size. Accepted: `4096`, `8192`, `16384`, `65536`. Larger pages = fewer I/O ops for big values; smaller = less waste for small DBs.       |
| `MaxValueBytes` | `uint32`        | `8192`  | no        | Max encoded value size per B+ tree entry. Accepted: 512 to 1048576 (powers of 2). A non-zero value on reopen updates the persisted limit for subsequent writes; existing values are unchanged. |
| `PageCacheSize` | `int64`         | `0`     | no        | In-process B+ tree page-cache budget. `0` selects 4 MiB, positive values set bytes, and `-1` disables the cache.                                   |

### Embeddings

| Field                | Type             | Default          | Immutable | Description                                                                                                                                                                                                                                                                                                                   |
| -------------------- | ---------------- | ---------------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `EmbeddingDimension` | `uint16`         | `0`              | yes       | Optional pre-set vector dimension. When `0` (default), the dimension is **auto-detected from the first `PutEmbedding` call** and persisted in the file header. When non-zero, locked at creation time. Either way, once set all vectors must match. Common values: `384` (all-MiniLM), `768` (BERT), `1536` (OpenAI ada-002). |
| `DistanceMetric`     | `DistanceMetric` | `DistanceCosine` | yes       | Similarity function. Defaults to cosine. Persisted on first `PutEmbedding` if not set at `Open` time.                                                                                                                                                                                                                         |

Embeddings are always available — there is no "disabled" state. A database with no stored embeddings simply has `EmbeddingDimension() == 0` until the first vector is written.

**Distance metrics:**

| Constant             | Behaviour                                                                                       |
| -------------------- | ----------------------------------------------------------------------------------------------- |
| `DistanceCosine`     | Cosine similarity. Range \[-1, 1\], higher = more similar. Best for normalized text embeddings. |
| `DistanceDotProduct` | Raw dot product. Assumes pre-normalized vectors.                                                |
| `DistanceL2`         | Euclidean distance. Lower = more similar (inverted internally for ranking).                     |

### Encryption

| Field           | Type     | Default | Description                                                                                                                                                  |
| --------------- | -------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `EncryptionKey` | `[]byte` | `nil`   | AES-256-XTS at-rest encryption. Stretched with HKDF-SHA256. Use a key with ≥128 bits of entropy. Mutually exclusive with `Passphrase` and custom page hooks. |
| `Passphrase`    | `string` | `""`    | User passphrase. Combined with Argon2id + per-database salt from the file header. Mutually exclusive with `EncryptionKey` and custom page hooks.             |

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
| `AfterPutCell`  | `AfterPutCellHook` | Post-write hook. Called synchronously after each successful `PutCell`. Use for audit logging, metrics, CDC triggers.                              |
| `AfterPutSeam`  | `AfterPutSeamHook` | Post-write hook. Called after `PutSeam`, `MarkConflict`, `MarkSupersedes`. Use for conflict alerting, review workflows.                           |

Convenience adapters: `CellValidatorFunc`, `AfterPutCellHookFunc`, `AfterPutSeamHookFunc` wrap plain functions.

### Durability tuning

| Field                  | Type            | Default | Description                                                                                                                  |
| ---------------------- | --------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `UsePrimaryFdatasync`  | `bool`          | `false` | Use `fdatasync(2)` instead of `fsync(2)` on the primary file (Linux). See `DURABILITY.md`.                                   |
| `GroupWALMaxBatchWait` | `time.Duration` | `0`     | Immediate flush after already-queued jobs are collected. Positive waits are useful only for direct-engine batching; serialized public updates cannot coalesce. |

### Page hooks (advanced)

| Field             | Type                                                | Description                                                               |
| ----------------- | --------------------------------------------------- | ------------------------------------------------------------------------- |
| `BeforeWritePage` | `func(pageID uint64, plain []byte) ([]byte, error)` | Transform before page is logged/written. Cannot be used with encryption.  |
| `AfterReadPage`   | `func(pageID uint64, data []byte) ([]byte, error)`  | Transform after reading a page from disk. Cannot be used with encryption. |

---

## Common configurations

### Minimal (defaults)

```go
db, _ := hexxladb.Open("data.db", nil)
```

No MVCC, embedding dimension auto-detected on first vector write, 4 KiB pages, 8 KiB max value. Good for simple key-value usage.

### LLM memory with embeddings

```go
db, _ := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC:         true,
    EmbeddingDimension: 384,
    DistanceMetric:     hexxladb.DistanceCosine,
    PageSize:           4096,
    PageCacheSize:      64 << 20,
})
```

MVCC for time-travel and snapshot diffing. For the measured 10,000-vector HNSW envelope, 4 KiB pages avoid random graph-maintenance amplification and a 64 MiB cache keeps hot B+ tree pages resident. Benchmark representative data before changing either value; larger pages can still help workloads dominated by large sequential values rather than embeddings.

### Production with full features

```go
db, _ := hexxladb.Open("prod.db", &hexxladb.Options{
    EnableMVCC:         true,
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
    EnableMVCC:    true,
    EncryptionKey: key,
})
```

---

## Persisted and per-open fields

- **Engine format and page size** are fixed when the database is created. `EnableMVCC` and `PageSize` do not convert an existing file; use the documented migration or compaction workflow when a new physical format is required.
- **Embedding dimension and metric** become fixed together when configured at creation or detected on the first `PutEmbedding`. A conflicting non-zero embedding configuration on reopen returns `ErrInvalidArgument`.
- **`MaxValueBytes` is persisted but mutable.** Passing a supported non-zero value on reopen updates the stored limit for subsequent writes. Existing values are not rewritten, and lowering the limit does not remove them.
- **Encryption mode and credentials** cannot be changed through ordinary `Open`; use `RotateEncryption` for an offline source-preserving replacement.
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
