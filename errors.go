package hexxladb

import (
	"errors"

	"github.com/hexxla/hexxladb/internal/changelog"
)

// Stable sentinel errors for the public API. Use [errors.Is] and [errors.As].
var (
	// ErrNotImplemented indicates API surface that is not yet implemented.
	ErrNotImplemented = errors.New("hexxladb: not implemented")

	// ErrSeamNotFound means a seam ULID had no primary key seam/<ulid>.
	ErrSeamNotFound = errors.New("hexxladb: seam not found")

	// ErrSeamEndpointMismatch means [Tx.PutSeam] would change CellA/CellB for an existing ULID;
	// seam endpoints are immutable for a given ULID (M7+).
	ErrSeamEndpointMismatch = errors.New("hexxladb: seam endpoint mismatch for ULID")

	// ErrInvalidArgument means a parameter was out of range or invalid for the API.
	ErrInvalidArgument = errors.New("hexxladb: invalid argument")

	// ErrClosed indicates use of a closed transaction or handle where one was required.
	ErrClosed = errors.New("hexxladb: closed")

	// ErrDatabaseClosed indicates the database was closed ([DB.Close]) and is no longer usable.
	ErrDatabaseClosed = errors.New("hexxladb: database closed")

	// ErrTxReadOnly means a write was attempted inside [DB.View] (read-only transaction).
	ErrTxReadOnly = errors.New("hexxladb: transaction is read-only")

	// ErrNilCallback means View or Update was called with a nil function.
	ErrNilCallback = errors.New("hexxladb: nil callback")

	// ErrEncryptionKeyRequired means the database file is marked encrypted but no key or passphrase was provided.
	ErrEncryptionKeyRequired = errors.New("hexxladb: encryption key required")

	// ErrDatabaseNotEncrypted means encryption options were provided but the database is plaintext.
	ErrDatabaseNotEncrypted = errors.New("hexxladb: database is not encrypted")

	// ErrEncryptionOptions means encryption options conflict with custom page hooks.
	ErrEncryptionOptions = errors.New("hexxladb: encryption options conflict with custom page hooks")

	// ErrEncryptionKeyMismatch means provided key/passphrase does not match the encrypted database.
	ErrEncryptionKeyMismatch = errors.New("hexxladb: encryption key mismatch")

	// ErrCellNotFound means no cell exists at the key required for the operation (e.g. [Tx.UpdateFacet]).
	ErrCellNotFound = errors.New("hexxladb: cell not found")

	// ErrFacetDerivationMismatch means [Tx.UpdateFacet] was rejected: facet DerivationHash does not match SHA-256 of the cell RawContent.
	ErrFacetDerivationMismatch = errors.New("hexxladb: facet derivation hash mismatch")

	// ErrChangelogDisabled means [DB.ReadChangelogSince] was called but changelog was not enabled in [Options].
	ErrChangelogDisabled = errors.New("hexxladb: changelog not enabled")

	// ErrReadSeqFuture means [DB.ViewAt] was called with a read_seq greater than the database's committed [engine.Header.CommitSeq].
	ErrReadSeqFuture = errors.New("hexxladb: read_seq beyond last commit")

	// ErrCommitFinalization means callback writes may have reached storage but post-callback finalization failed.
	ErrCommitFinalization = errors.New("hexxladb: commit finalization failed")
)

// ErrChangelogCorrupt means the logical changelog file failed validation (docs/hexxladb/CHANGEFEED.md).
var ErrChangelogCorrupt = changelog.ErrCorrupt
