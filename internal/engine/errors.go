package engine

import "errors"

var (
	// ErrCorruptHeader means the database file header is invalid or unsupported.
	ErrCorruptHeader = errors.New("engine: corrupt or unsupported database header")

	// ErrCorruptWAL means the WAL is truncated or fails checksum verification.
	ErrCorruptWAL = errors.New("engine: corrupt WAL")

	// ErrBadPageID means the page id is out of range for the operation.
	ErrBadPageID = errors.New("engine: invalid page id")

	// ErrBadPageSize means the payload length is not exactly PageSize.
	ErrBadPageSize = errors.New("engine: page payload must be PageSize bytes")

	// ErrCorruptTree means a B+ tree page failed validation.
	ErrCorruptTree = errors.New("engine: corrupt B+ tree page")

	// ErrKeyTooLarge means a key exceeds the B+ tree page format limit.
	ErrKeyTooLarge = errors.New("engine: key too large for B+ tree")

	// ErrValueTooLarge means a value exceeds the B+ tree page format limit.
	ErrValueTooLarge = errors.New("engine: value too large for B+ tree")
)
