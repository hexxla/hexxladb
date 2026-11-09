package engine

// Options configures the embedded storage engine.
type Options struct {
	// disablePageReuse is a test-only control for allocator evidence baselines.
	disablePageReuse bool
	// CreateExclusive requires path not to exist. It is used by maintenance operations
	// that must never merge into or overwrite an existing database.
	CreateExclusive bool
	// RequireExistingPrimary prevents opening a missing or empty primary as a
	// new database. A missing WAL may still be created for an existing primary.
	RequireExistingPrimary bool
	// Hooks optional page transforms (e.g. encryption).
	Hooks *PageHooks
	// NewAuthenticatedDB creates a format-v3 database with authenticated pages.
	NewAuthenticatedDB bool
	// NewIncompleteCompaction marks the first durable header as an unpublished
	// compaction destination. The public database boundary owns this lifecycle.
	NewIncompleteCompaction bool
	// EncryptionSalt is the database identity used by authenticated encryption.
	EncryptionSalt [16]byte
	// EncryptionKeyCheck is persisted for new encrypted DBs and used to verify provided keys.
	EncryptionKeyCheck [HeaderEncryptionKeyCheckLen]byte
	// WALMACKey is used when EnableWALMAC is true to sign/verify WAL records.
	WALMACKey [32]byte
	// EnableWALMAC enables keyed MAC verification for WAL records.
	EnableWALMAC bool
	// HeaderMACKey authenticates format-v3 header metadata.
	HeaderMACKey [32]byte
	// EnableHeaderMAC enables format-v3 header authentication.
	EnableHeaderMAC bool
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
