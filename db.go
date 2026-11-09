package hexxladb

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
)

// dbCachedHeader holds the two header fields needed by readers: the latest committed
// sequence number and the B+ tree root page ID. Updated atomically by the writer after
// every successful commit; read lock-free by View / ViewAt / ViewAtTime.
type dbCachedHeader struct {
	commitSeq uint64
	btreeRoot uint64
}

// DB is a handle to an embedded HexxlaDB database. Use [Open] or [OpenExisting].
// Concurrent [View] calls may run together. [Update] and [Batch] hold the exclusive DB lock through
// their callback, engine commit, and DB-level finalization; see [docs/hexxladb/TX.md].
type DB struct {
	mu            sync.RWMutex
	eng           *engine.Engine
	btree         *engine.BTree
	changelog     *changelog.Log
	mvccRetention MVCCRetention    // copy of [Options.MVCCRetention] at [Open] for [SuggestedPruneBeforeSeq].
	cellValidator CellValidator    // optional pre-write hook from [Options.CellValidator].
	afterPutCell  AfterPutCellHook // optional post-write hook from [Options.AfterPutCell].
	afterPutSeam  AfterPutSeamHook // optional post-write hook from [Options.AfterPutSeam].
	writeSeqNext  atomic.Uint64
	// Snapshot retention state is guarded by mu after Open publishes the handle.
	snapshotFloor          uint64
	snapshotFloorStored    bool
	snapshotFloorUncertain bool
	// changefeedSeqNext allocates durable primary-outbox commit identifiers independently
	// of external changelog record sequences. Its value is persisted under the outbox head key.
	changefeedSeqNext atomic.Uint64
	changelogLazy     bool
	// pendingOutboxEntries is guarded by mu and bounds lazy-mode intents between sync barriers.
	pendingOutboxEntries int
	// commitFaults is test-only deterministic boundary injection; production construction leaves it nil.
	commitFaults *commitFaultHooks
	// embeddingRebuildFaults is test-only deterministic boundary injection.
	embeddingRebuildFaults *embeddingRebuildFaultHooks
	// cachedHdr is the fast-path header snapshot for readers. Writers store a new pointer
	// after every commit; readers load it without holding any lock.
	cachedHdr atomic.Pointer[dbCachedHeader]
	// closed is set to true by Close before the mutex is released, allowing
	// public methods to fast-fail without acquiring the read lock.
	closed atomic.Bool
	// recoveryRequired prevents another writer from building on an ambiguous or incompletely
	// projected finalization outcome. Close and Open perform the supported recovery boundary.
	recoveryRequired atomic.Bool
	writeStats       writeStatsCounters
}

func (db *DB) storeCachedHeader(commitSeq, btreeRoot uint64) {
	db.cachedHdr.Store(&dbCachedHeader{commitSeq: commitSeq, btreeRoot: btreeRoot})
}

// ErrCorruptDatabase means the database or WAL failed validation on open.
var ErrCorruptDatabase = errors.New("hexxladb: corrupt database")

// Open opens or creates a database at path and recovers pending WAL commits
// before returning a handle. It returns [ErrCorruptDatabase] for a WAL that
// fails validation, including pending v2 redo without a recoverable commit header.
func Open(path string, opts *Options) (*DB, error) {
	return openDB(path, opts, false)
}

// OpenExisting opens an existing database and recovers pending WAL commits.
// It never creates the primary file. Like [Open], it takes the exclusive
// database lock and may write recovery changes or create sidecars.
func OpenExisting(path string, opts *Options) (*DB, error) {
	var existing Options
	if opts != nil {
		existing = *opts
	}
	existing.requireExistingPrimary = true
	return openDB(path, &existing, false)
}

func openDB(path string, opts *Options, createExclusive bool) (*DB, error) {
	pathLock, err := engine.AcquirePathLock(path)
	if err != nil {
		return nil, mapEngineOpenError(path, err)
	}
	defer func() { _ = pathLock.Close() }()
	return openDBWithPathLock(path, opts, createExclusive)
}

// openDBWithPathLock requires the caller to hold the stable path lock until
// this call has acquired the primary lock and finished opening its recovery set.
func openDBWithPathLock(path string, opts *Options, createExclusive bool) (*DB, error) {
	pendingRotation, err := rotationPending(path)
	if err != nil {
		return nil, err
	}
	if pendingRotation {
		return nil, ErrRotationIncomplete
	}
	eopts, master, err := openEngineOptions(path, opts, createExclusive)
	if err != nil {
		return nil, mapEngineOpenError(path, err)
	}
	defer clear(master)
	eng, err := engine.Open(path, eopts)
	if err != nil {
		return nil, mapEngineOpenError(path, err)
	}
	hdr, err := eng.ReadHeader()
	if err != nil {
		_ = eng.Close()
		return nil, err
	}
	if hdr.Features&engine.FeatureIncompleteCompaction != 0 && (opts == nil || !opts.newIncompleteCompaction) {
		_ = eng.Close()
		return nil, ErrCompactionIncomplete
	}
	bt := engine.OpenBTree(eng)
	if err := rejectIncompleteMigration(bt); err != nil {
		_ = eng.Close()
		return nil, mapEngineDataError(err)
	}
	db := &DB{eng: eng, btree: bt}
	db.writeSeqNext.Store(hdr.CommitSeq)
	db.storeCachedHeader(hdr.CommitSeq, hdr.BTreeRoot)
	if err := db.loadSnapshotFloor(hdr.CommitSeq); err != nil {
		_ = eng.Close()
		return nil, err
	}
	if err := db.initializeChangefeedHead(); err != nil {
		_ = eng.Close()
		return nil, mapEngineDataError(err)
	}
	if opts != nil {
		db.mvccRetention = opts.MVCCRetention
		db.cellValidator = opts.CellValidator
		db.afterPutCell = opts.AfterPutCell
		db.afterPutSeam = opts.AfterPutSeam
	}
	if opts != nil && opts.ChangelogEnabled {
		if err := db.openChangelog(path, opts, hdr, master); err != nil {
			_ = eng.Close()
			return nil, err
		}
	}
	return db, nil
}

func openEngineOptions(path string, opts *Options, createExclusive bool) (*engine.Options, []byte, error) {
	eopts, master, err := buildEngineOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}
	eopts = mergeEnginePageSize(eopts, opts)
	eopts = mergeEnginePrimaryFdatasync(eopts, opts)
	eopts = mergeEngineMaxValueBytes(eopts, opts)
	eopts = mergeEnginePageCache(eopts, opts)
	eopts = mergeEngineEmbedding(eopts, opts)
	if eopts == nil {
		eopts = &engine.Options{}
	}
	eopts.CreateExclusive = createExclusive
	if opts != nil {
		eopts.NewIncompleteCompaction = opts.newIncompleteCompaction
		eopts.RequireExistingPrimary = opts.requireExistingPrimary
	}
	return eopts, master, nil
}

func (db *DB) openChangelog(path string, opts *Options, hdr engine.Header, master []byte) error {
	db.changelogLazy = opts.ChangelogLazy
	pendingOutbox, err := db.readPendingChangelogIntents()
	if err != nil {
		return err
	}
	recoverableTail := len(pendingOutbox) > 0
	clPath := cmp.Or(opts.ChangelogPath, path+"-changelog")
	syncWrites := !opts.ChangelogLazy
	var cl *changelog.Log
	switch {
	case len(master) > 0 && recoverableTail:
		changelogKey := deriveChangelogKey(master, hdr.EncryptionSalt)
		cl, err = changelog.OpenEncryptedRecoverable(clPath, syncWrites, changelogKey[:])
		clear(changelogKey[:])
	case len(master) > 0:
		changelogKey := deriveChangelogKey(master, hdr.EncryptionSalt)
		cl, err = changelog.OpenEncrypted(clPath, syncWrites, changelogKey[:])
		clear(changelogKey[:])
	case recoverableTail:
		cl, err = changelog.OpenRecoverable(clPath, syncWrites)
	default:
		cl, err = changelog.Open(clPath, syncWrites)
	}
	if err != nil {
		return mapChangelogOpenError(err)
	}
	db.changelog = cl
	// Validate acknowledged history before recovery can publish a new checkpoint.
	consumers, err := db.readChangelogConsumersLocked()
	if err == nil {
		err = db.validateChangelogConsumerHistoryLocked(consumers)
	}
	if err != nil {
		_ = cl.Close()
		return err
	}
	if err := db.recoverChangelogOutbox(); err != nil {
		_ = cl.Close()
		return err
	}
	return nil
}

func mapEngineOpenError(path string, err error) error {
	switch {
	case errors.Is(err, engine.ErrUnsupportedFormatVersion):
		return fmt.Errorf("%w: %w", ErrUnsupportedFormatVersion, err)
	case errors.Is(err, engine.ErrCorruptHeader), errors.Is(err, engine.ErrCorruptWAL), errors.Is(err, engine.ErrPageAuthentication):
		return fmt.Errorf("%w: %w", ErrCorruptDatabase, err)
	case errors.Is(err, engine.ErrBadEncryptionKey):
		return ErrEncryptionKeyMismatch
	case errors.Is(err, engine.ErrInvalidMaxValueBytes), errors.Is(err, engine.ErrInvalidEmbeddingConfig):
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	case errors.Is(err, engine.ErrInvalidPageSize):
		return fmt.Errorf("%w: PageSize must be 4096, 8192, 16384, or 65536", ErrInvalidArgument)
	case errors.Is(err, engine.ErrDatabaseLocked):
		return fmt.Errorf("%w: %s", ErrDatabaseLocked, path)
	default:
		return err
	}
}

// Refuse unpublished candidates left by the offline pre-v1 converter.
func rejectIncompleteMigration(bt *engine.BTree) error {
	prefix := []byte("__meta/migration/v1-to-v2/")
	incomplete := false
	err := bt.AscendRange(prefix, []byte("__meta/migration/v1-to-v20"), func(key, _ []byte) bool {
		incomplete = bytes.HasPrefix(key, prefix)
		return false
	})
	if err != nil {
		return err
	}
	if incomplete {
		return ErrMigrationIncomplete
	}
	return nil
}

func mapChangelogOpenError(err error) error {
	switch {
	case errors.Is(err, changelog.ErrPlaintext):
		return fmt.Errorf("%w: archive or remove the legacy changelog before re-enabling it", ErrChangelogPlaintext)
	case errors.Is(err, changelog.ErrEncryptionRequired):
		return ErrChangelogEncryptionKeyRequired
	case errors.Is(err, changelog.ErrEncryptionKeyMismatch):
		return ErrChangelogEncryptionKeyMismatch
	default:
		return err
	}
}

// Close releases resources associated with the database.
// It waits for any in-flight [View], [Update], or [Batch] to finish.
// Multiple cleanup failures are joined and remain inspectable with errors.Is
// and errors.As. A single failure is returned directly.
func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.eng == nil {
		return nil
	}
	db.closed.Store(true)
	var errs []error
	if db.changelog != nil && db.changelogLazy && db.pendingOutboxEntries > 0 && !db.recoveryRequired.Load() {
		errs = append(errs, db.syncAndCleanupPendingChangelog())
	}
	if db.changelog != nil {
		errs = append(errs, db.changelog.Close())
		db.changelog = nil
	}
	errs = append(errs, db.eng.Close())
	db.eng = nil
	db.btree = nil
	errs = slices.DeleteFunc(errs, func(err error) bool { return err == nil })
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

func (db *DB) activeEng() *engine.Engine {
	if db == nil || db.eng == nil {
		return nil
	}
	return db.eng
}

// PageSize returns the page size of the database in bytes.
// For new databases, this is the value from [Options.PageSize] (or the default 4096).
// For existing databases, it is read from the file header.
// Returns 0 if the database is closed.
func (db *DB) PageSize() uint32 {
	if db == nil {
		return 0
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return 0
	}
	return uint32(db.eng.PageSizeInt()) //nolint:gosec // PageSizeInt always returns a valid page size
}

// MaxValueBytes returns the effective per-database maximum B+ tree value size in bytes.
// This is the limit persisted in the file header; the default is 8192 (8 KB).
// Returns 0 if the database is closed.
func (db *DB) MaxValueBytes() uint32 {
	if db == nil {
		return 0
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return 0
	}
	return db.eng.MaxValueBytes()
}

// WALStats reports the number of WAL commit attempts and successful redo
// syncs.
// Redo syncs exclude the sync used to reset the WAL after a durable commit.
func (db *DB) WALStats() (commitAttempts, redoSyncs uint64) {
	if db == nil {
		return 0, 0
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return 0, 0
	}
	return db.eng.WALStats()
}
