package engine

import "errors"

var (
	// ErrCorruptHeader means the database file header is invalid.
	ErrCorruptHeader = errors.New("engine: corrupt database header")

	// ErrUnsupportedFormatVersion means the file uses a newer engine format.
	ErrUnsupportedFormatVersion = errors.New("engine: unsupported format version")

	// ErrCorruptWAL means the WAL is truncated or fails checksum verification.
	ErrCorruptWAL = errors.New("engine: corrupt WAL")

	// ErrBadPageID means the page id is out of range for the operation.
	ErrBadPageID = errors.New("engine: invalid page id")

	// ErrBadPageSize means the payload length does not match the database page size.
	ErrBadPageSize = errors.New("engine: page payload size mismatch")

	// ErrInvalidPageSize means the requested page size is not a supported value.
	ErrInvalidPageSize = errors.New("engine: invalid page size")

	// ErrBadEncryptionKey means the provided encryption key/passphrase does not match the database verifier.
	ErrBadEncryptionKey = errors.New("engine: encryption key mismatch")

	// ErrPageAuthentication means an authenticated page or header failed verification.
	ErrPageAuthentication = errors.New("engine: page authentication failed")

	// ErrCorruptTree means a B+ tree page failed validation.
	ErrCorruptTree = errors.New("engine: corrupt B+ tree page")

	// ErrKeyTooLarge means a key exceeds the B+ tree page format limit.
	ErrKeyTooLarge = errors.New("engine: key too large for B+ tree")

	// ErrValueTooLarge means a value exceeds the B+ tree page format limit.
	ErrValueTooLarge = errors.New("engine: value too large for B+ tree")

	// ErrWriteTxnActive means [Engine.BeginWriteTxn] was called while a write transaction is open.
	ErrWriteTxnActive = errors.New("engine: write transaction already active")

	// ErrNoWriteTxn means [Engine.CommitWriteTxn] was called with no open write transaction.
	ErrNoWriteTxn = errors.New("engine: no write transaction active")

	// ErrInvalidMaxValueBytes means Options.MaxValueBytes is not one of the accepted values.
	ErrInvalidMaxValueBytes = errors.New("engine: MaxValueBytes must be " + validMaxValueBytesText)

	// ErrInvalidEmbeddingConfig means the embedding configuration is invalid or conflicts with the stored header.
	ErrInvalidEmbeddingConfig = errors.New("engine: invalid embedding configuration")

	// ErrDatabaseLocked means another process or handle owns the database file.
	ErrDatabaseLocked = errors.New("engine: database is locked")

	// ErrFileLockUnsupported means this platform cannot provide the exclusive file lock required for safe writes.
	ErrFileLockUnsupported = errors.New("engine: database file locking is unsupported on this platform")
)
