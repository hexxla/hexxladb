package engine

import "time"

// GroupWAL configures optional batched WAL apply (group commit).
type GroupWAL struct {
	// Enabled starts the background flusher that coalesces logical commits before WAL sync.
	Enabled bool
	// MaxBatchWait is the window after the first queued commit to batch additional commits.
	// Zero means a small default (2ms) in [Engine.startGroupWALFlusher].
	MaxBatchWait time.Duration
}

// Options configures the embedded storage engine.
type Options struct {
	// Hooks optional page transforms (e.g. encryption).
	Hooks *PageHooks
	// NewEncryptedDB sets FeatureEncryptedDataPages on newly created database files.
	NewEncryptedDB bool
	// EncryptionSalt is stored in the header for passphrase KDF (16 bytes). Ignored unless NewEncryptedDB.
	// If NewEncryptedDB and EncryptionSalt is zero, [Open] fills it with crypto/rand.
	EncryptionSalt [16]byte
	// UseFormatV2, when true on a new empty database file, writes format_version 2 with CommitSeq support (MVCC).
	// Ignored when opening an existing non-empty file (format is taken from the header).
	UseFormatV2 bool
	// EncryptionKeyCheck is persisted for new encrypted DBs and used to verify provided keys.
	EncryptionKeyCheck [HeaderEncryptionKeyCheckLen]byte
	// ExpectEncryptionKeyCheck requests deterministic wrong-key detection on open.
	ExpectEncryptionKeyCheck bool
	// WALMACKey is used when EnableWALMAC is true to sign/verify WAL records.
	WALMACKey [32]byte
	// EnableWALMAC enables keyed MAC verification for WAL records.
	EnableWALMAC bool
	// GroupWAL enables batched redo apply; see package writetxn and group_wal.go.
	GroupWAL GroupWAL
	// UsePrimaryFdatasync, when true, uses a data-only flush (e.g. fdatasync on Linux) on the
	// primary file instead of fsync for durability barriers. Default false. See [Engine.syncPrimary].
	UsePrimaryFdatasync bool
	// MaxValueBytes sets the per-database maximum B+ tree value size and is persisted in the file
	// header. Zero means [DefaultMaxValueBytes] (8192). Accepted values: 512..1048576.
	// Values exceeding the inline threshold spill to overflow pages. Invalid values are rejected at [Open] time.
	MaxValueBytes uint32
	// PageSize sets the page size for newly created databases. Accepted values: 4096, 8192,
	// 16384, 65536. Zero means [DefaultPageSize] (4096). Ignored when opening an existing
	// database — the page size is read from the file header.
	PageSize uint32
	// EmbeddingDim sets the fixed vector dimension for new databases. Zero means embeddings
	// disabled. Persisted in the file header; immutable after creation.
	EmbeddingDim uint16
	// EmbeddingMetric sets the distance function for embedding search. Only valid when
	// EmbeddingDim > 0. Persisted in the file header; immutable after creation.
	EmbeddingMetric DistanceMetric
	// PageCacheSize is the total byte budget for the in-process CLOCK-Pro page cache.
	// Zero disables the cache at the engine level. The public [Open] API resolves the
	// user-facing 0-means-default / -1-means-disabled convention before passing a value here.
	// The cache stores decrypted page bytes keyed by pageID and eliminates
	// pread syscalls for hot B+ tree pages (root, internal nodes, hot leaves).
	PageCacheSize int64
}
