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

`Open` creates the file if it doesn't exist, or opens an existing one. On open it replays any pending WAL entries. Passing `nil` for options uses defaults (no MVCC, no embeddings, 4 KiB pages).

---

## Options reference

All fields on `hexxladb.Options`. Fields marked **immutable** are persisted in the file header on creation and cannot be changed on reopen.

### Core

| Field           | Type            | Default | Immutable | Description                                                                                                                                           |
| --------------- | --------------- | ------- | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `EnableMVCC`    | `bool`          | `false` | yes       | Format v2 with snapshot isolation, time-travel (`ViewAt`), and version history. Required for `SnapshotDiff`, `PruneCellVersions`, `ViewAtTime`.       |
| `MVCCRetention` | `MVCCRetention` | zero    | no        | Policy for `SuggestedPruneBeforeSeq`. `RetainCommitsBehindHead` keeps N commits of history.                                                           |
| `PageSize`      | `uint32`        | `4096`  | yes       | B+ tree page size. Accepted: `4096`, `8192`, `16384`, `65536`. Larger pages = fewer I/O ops for big values; smaller = less waste for small DBs.       |
| `MaxValueBytes` | `uint32`        | `8192`  | yes       | Max encoded value size per B+ tree entry. Accepted: 512 to 1048576 (powers of 2). Values above the inline threshold use overflow pages automatically. |

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

| Field              | Type     | Default | Description                                                                 |
| ------------------ | -------- | ------- | --------------------------------------------------------------------------- |
| `ChangelogEnabled` | `bool`   | `false` | Maintain an append-only logical changefeed file alongside the DB.           |
| `ChangelogPath`    | `string` | `""`    | Override path. Empty = `<dbpath>-changelog`.                                |
| `ChangelogLazy`    | `bool`   | `false` | Skip fsync after each commit batch. Faster; may lose tail records on crash. |

### Hooks

| Field           | Type               | Description                                                                                                                                       |
| --------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CellValidator` | `CellValidator`    | Pre-write validation. Return non-nil error from `ValidateCell(rec)` to reject a `PutCell`. Use for content limits, required tags, business rules. |
| `AfterPutCell`  | `AfterPutCellHook` | Post-write hook. Called synchronously after each successful `PutCell`. Use for audit logging, metrics, CDC triggers.                              |
| `AfterPutSeam`  | `AfterPutSeamHook` | Post-write hook. Called after `PutSeam`, `MarkConflict`, `MarkSupersedes`. Use for conflict alerting, review workflows.                           |

Convenience adapters: `CellValidatorFunc`, `AfterPutCellHookFunc`, `AfterPutSeamHookFunc` wrap plain functions.

### Durability tuning

| Field                  | Type            | Default | Description                                                                                |
| ---------------------- | --------------- | ------- | ------------------------------------------------------------------------------------------ |
| `UsePrimaryFdatasync`  | `bool`          | `false` | Use `fdatasync(2)` instead of `fsync(2)` on the primary file (Linux). See `DURABILITY.md`. |
| `GroupWALMaxBatchWait` | `time.Duration` | `2ms`   | WAL flusher coalescing window. Lower = less latency; higher = more batching.               |

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

No MVCC, no embeddings, 4 KiB pages, 8 KiB max value. Good for simple key-value usage.

### LLM memory with embeddings

```go
db, _ := hexxladb.Open("memory.db", &hexxladb.Options{
    EnableMVCC:         true,
    EmbeddingDimension: 384,
    DistanceMetric:     hexxladb.DistanceCosine,
    PageSize:           65536,
})
```

MVCC for time-travel and snapshot diffing. 384-dim vectors (all-MiniLM-L6-v2). Large pages for embedding storage efficiency.

### Production with full features

```go
db, _ := hexxladb.Open("prod.db", &hexxladb.Options{
    EnableMVCC:         true,
    EmbeddingDimension: 384,
    DistanceMetric:     hexxladb.DistanceCosine,
    PageSize:           65536,
    MaxValueBytes:      65536,
    ChangelogEnabled:   true,
    Passphrase:         os.Getenv("HEXXLA_PASSPHRASE"),
    MVCCRetention:      hexxladb.MVCCRetention{RetainCommitsBehindHead: 1000},
    CellValidator: hexxladb.CellValidatorFunc(func(rec record.CellRecord) error {
        if len(rec.RawContent) > 100_000 {
            return fmt.Errorf("content too large: %d bytes", len(rec.RawContent))
        }
        return nil
    }),
    AfterPutCell: hexxladb.AfterPutCellHookFunc(func(_ context.Context, rec record.CellRecord) error {
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

## Immutable vs mutable fields

Fields marked **immutable** are written to the file header when the database is first created (or on first use for embedding dimension). On subsequent opens:

- **Immutable fields are read from the header**, not from `Options`. If you pass a conflicting value (e.g. different `EmbeddingDimension`), `Open` returns an error.
- **Mutable fields** (hooks, changelog, MVCC retention, durability tuning) take effect on each open and can change between runs.
- **Embedding dimension** is special: if not set at `Open` time, it's auto-detected and persisted on the first `PutEmbedding` call.

To change an immutable field, create a new database and migrate data.

---

## Runtime accessors

After opening, read back configuration:

```go
db.PageSize()           // uint32 — active page size
db.MaxValueBytes()      // uint32 — max value size
db.EmbeddingDimension() // uint16 — vector dimension (0 = disabled)
db.EmbeddingMetric()    // DistanceMetric — distance function
```

---

## See also

- [`API_REFERENCE.md`](API_REFERENCE.md) — complete API surface
- [`OPERATIONS.md`](OPERATIONS.md) — production operations, backup, compaction, benchmarks
- [`HEXXLA_DB.md`](HEXXLA_DB.md) — storage layout, key encoding
