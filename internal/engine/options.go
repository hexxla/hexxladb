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
}
