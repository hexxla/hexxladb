package hexxladb

import (
	"time"
)

// MVCCRetention configures optional defaults for [DB.SuggestedPruneBeforeSeq] and [DB.MVCCPrunePlan].
// Zero values mean automatic prune suggestions are disabled (operators pass explicit beforeSeq to [DB.PruneCellVersions]).
type MVCCRetention struct {
	// RetainCommitsBehindHead keeps at least this much commit history: only cell versions with
	// commit_seq strictly less than (current CommitSeq - RetainCommitsBehindHead) are eligible
	// for pruning (the latest version per logical cell is always kept). For example, with
	// RetainCommitsBehindHead=1000 and CommitSeq=5000, [SuggestedPruneBeforeSeq] returns 4000,
	// so [PruneCellVersions](4000, ...) may delete stale rows with seq < 4000.
	RetainCommitsBehindHead uint64
}

// Options configures opening a database (see [Open]).
type Options struct {
	// EnableMVCC, when true, creates new databases at engine format v2 with MVCC versioned keys
	// (see [docs/hexxladb/TX.md] for snapshot semantics and [docs/hexxladb/OPERATIONS.md] for retention/pruning).
	// Existing v1 files are never auto-upgraded; they keep single-version behavior until migrated.
	EnableMVCC bool
	// MVCCRetention is optional policy for [DB.SuggestedPruneBeforeSeq] / [DB.MVCCPrunePlan] (ignored for v1 files).
	MVCCRetention MVCCRetention
	// ChangelogEnabled, when true, maintains an append-only logical changefeed file (see [docs/hexxladb/CHANGEFEED.md]).
	ChangelogEnabled bool
	// ChangelogPath overrides the default path (<dbpath>-changelog). Empty means default.
	ChangelogPath string
	// ChangelogLazy, when true, avoids fsync after each commit batch (faster; may lose tail records on crash).
	ChangelogLazy bool
	// BeforeWritePage optional transform before a data page is logged and written.
	// If [EncryptionKey] or [Passphrase] is set, custom hooks must not be used.
	BeforeWritePage func(pageID uint64, plain []byte) (out []byte, err error)
	// AfterReadPage optional transform after reading a data page from disk.
	AfterReadPage func(pageID uint64, data []byte) (out []byte, err error)

	// EncryptionKey optional secret for AES-XTS at-rest encryption of data pages (see [docs/hexxladb/ENCRYPTION.md]).
	// Stretched with HKDF-SHA256; use a key with at least 128 bits of entropy.
	// Mutually exclusive with [Passphrase] and with custom page hooks.
	EncryptionKey []byte
	// Passphrase optional user passphrase; combined with Argon2id and the per-database salt in the file header.
	// Mutually exclusive with [EncryptionKey] and custom page hooks.
	Passphrase string

	// CellValidator optional pre-write validation hook. When set, [Tx.PutCell] calls
	// ValidateCell before encoding; a non-nil error aborts the write.
	// Use for enforcing content limits, required tags, or custom business rules.
	CellValidator CellValidator

	// AfterPutCell optional post-write hook. When set, called synchronously after each
	// successful [Tx.PutCell]. A non-nil error is returned from [Tx.PutCell] to the caller.
	AfterPutCell AfterPutCellHook

	// AfterPutSeam optional post-write hook. When set, called synchronously after each
	// successful [Tx.PutSeam], [Tx.MarkConflict], or [Tx.MarkSupersedes].
	// A non-nil error is returned from the triggering write method.
	AfterPutSeam AfterPutSeamHook

	// PageSize sets the page size for newly created databases. Accepted values: 4096, 8192,
	// 16384, 65536. Zero means the default (4096 bytes = 4 KiB). Ignored when opening an
	// existing database — the page size is read from the file header.
	PageSize uint32

	// MaxValueBytes sets the per-database maximum encoded value size stored in the B+ tree.
	// The limit is persisted in the file header and enforced on every write.
	// Zero means the default (8192 bytes = 8 KB). Accepted non-zero values: 512, 1024, 2048,
	// 4096, 8192, 16384, 32768, 65536, 131072, 262144, 524288, 1048576.
	// Values exceeding the inline leaf threshold are stored in overflow pages automatically.
	// Invalid values cause [Open] to return [ErrInvalidArgument].
	MaxValueBytes uint32

	// PageCacheSize controls the in-process CLOCK-Pro B+ tree page cache.
	// 0 (default) enables the cache with a 4 MiB budget, which is appropriate for most workloads.
	// Positive values set a custom byte budget (e.g. 16 << 20 for 16 MiB).
	// -1 disables the cache entirely (useful for memory-constrained environments or benchmarking).
	PageCacheSize int64

	// UsePrimaryFdatasync, when true, uses fdatasync(2) on the primary data file on supported
	// platforms (e.g. Linux) instead of fsync(2) for engine durability barriers. Default false; see
	// [docs/hexxladb/DURABILITY.md] before enabling in production.
	UsePrimaryFdatasync bool
	// GroupWALMaxBatchWait is passed to the engine group-WAL flusher as the coalescing window after
	// the first job in a batch. Zero means a 2ms default in the engine. See [docs/hexxladb/DURABILITY.md].
	GroupWALMaxBatchWait time.Duration

	// EmbeddingDimension optionally pre-sets the fixed vector dimension for new databases.
	// When 0 (default), the dimension is auto-detected from the first [Tx.PutEmbedding] call
	// and persisted in the file header. When non-zero, the dimension is set at creation time.
	// Either way, once set it is immutable — all subsequent vectors must match.
	EmbeddingDimension uint16
	// DistanceMetric sets the similarity function for embedding search. Default is [DistanceCosine].
	// Persisted in the file header; immutable after creation.
	DistanceMetric DistanceMetric
}
