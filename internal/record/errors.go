package record

import "errors"

var (
	// ErrInvalidRecord means data is truncated, malformed, or fails validation.
	ErrInvalidRecord = errors.New("record: invalid record")

	// ErrUnsupportedFormatVersion means format_version is newer than this package supports.
	ErrUnsupportedFormatVersion = errors.New("record: unsupported format version")

	// ErrUnknownFormatVersion means format_version is older or unrecognized.
	ErrUnknownFormatVersion = errors.New("record: unknown format version")

	// ErrWrongMagic means the 4-byte magic does not match the expected family.
	ErrWrongMagic = errors.New("record: wrong magic")

	// ErrInvalidULID means a seam id is not a valid ULID string or binary form.
	ErrInvalidULID = errors.New("record: invalid ULID")
)
