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
}
