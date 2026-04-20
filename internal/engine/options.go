package engine

// Options configures the engine shell (M3).
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
}
