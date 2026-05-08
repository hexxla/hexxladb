package hexxladb

import (
	"errors"
	"fmt"
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

// DB is a handle to an embedded HexxlaDB database. Construction is via [Open].
// Concurrent [View] calls are serialized with readers. [Update] and [Batch] hold the DB lock around
// the callback; see [docs/hexxladb/TX.md] for group-WAL wait semantics during engine commit.
type DB struct {
	mu            sync.RWMutex
	eng           *engine.Engine
	btree         *engine.BTree
	changelog     *changelog.Log
	useMVCC       bool             // true when on-disk format is v2+ (MVCC physical keys; see [Options.EnableMVCC]).
	mvccRetention MVCCRetention    // copy of [Options.MVCCRetention] at [Open] for [SuggestedPruneBeforeSeq].
	cellValidator CellValidator    // optional pre-write hook from [Options.CellValidator].
	afterPutCell  AfterPutCellHook // optional post-write hook from [Options.AfterPutCell].
	afterPutSeam  AfterPutSeamHook // optional post-write hook from [Options.AfterPutSeam].
	writeSeqNext  atomic.Uint64
	// cachedHdr is the fast-path header snapshot for readers. Writers store a new pointer
	// after every commit; readers load it without holding any lock.
	cachedHdr atomic.Pointer[dbCachedHeader]
	// closed is set to true by Close before the mutex is released, allowing
	// public methods to fast-fail without acquiring the read lock.
	closed atomic.Bool
}

func (db *DB) storeCachedHeader(commitSeq, btreeRoot uint64) {
	db.cachedHdr.Store(&dbCachedHeader{commitSeq: commitSeq, btreeRoot: btreeRoot})
}

// ErrCorruptDatabase means the database or WAL failed validation on open.
var ErrCorruptDatabase = errors.New("hexxladb: corrupt database")

// Open opens or creates a database at path. On success, any redo WAL is applied.
func Open(path string, opts *Options) (*DB, error) {
	eopts, err := buildEngineOptions(path, opts)
	if err != nil {
		return nil, err
	}
	eopts = mergeEnginePageSize(eopts, opts)
	eopts = mergeEnginePrimaryFdatasync(eopts, opts)
	eopts = mergeEngineGroupWAL(eopts, opts)
	eopts = mergeEngineMaxValueBytes(eopts, opts)
	eopts = mergeEnginePageCache(eopts, opts)
	eopts = mergeEngineEmbedding(eopts, opts)
	eng, err := engine.Open(path, eopts)
	if err != nil {
		if errors.Is(err, engine.ErrCorruptHeader) || errors.Is(err, engine.ErrCorruptWAL) {
			return nil, fmt.Errorf("%w: %w", ErrCorruptDatabase, err)
		}
		if errors.Is(err, engine.ErrBadEncryptionKey) {
			return nil, ErrEncryptionKeyMismatch
		}
		if errors.Is(err, engine.ErrInvalidMaxValueBytes) {
			return nil, fmt.Errorf("%w: MaxValueBytes must be 512, 1024, 2048, 4096, 8192, or 16384", ErrInvalidArgument)
		}
		if errors.Is(err, engine.ErrInvalidPageSize) {
			return nil, fmt.Errorf("%w: PageSize must be 4096, 8192, 16384, or 65536", ErrInvalidArgument)
		}
		if errors.Is(err, engine.ErrInvalidEmbeddingConfig) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
		}
		return nil, err
	}
	hdr, err := eng.ReadHeader()
	if err != nil {
		_ = eng.Close()
		return nil, err
	}
	if err := openValidateEncryption(opts, hdr); err != nil {
		_ = eng.Close()
		return nil, err
	}
	if eopts != nil && eopts.ExpectEncryptionKeyCheck &&
		hdr.Features&engine.FeatureEncryptedDataPages != 0 &&
		hdr.EncryptionKeyCheck == ([engine.HeaderEncryptionKeyCheckLen]byte{}) {
		if err := eng.UpdateHeader(func(h *engine.Header) {
			h.EncryptionKeyCheck = eopts.EncryptionKeyCheck
		}); err != nil {
			_ = eng.Close()
			return nil, err
		}
	}
	bt := engine.OpenBTree(eng)
	db := &DB{eng: eng, btree: bt, useMVCC: hdr.FormatVersion >= 2}
	db.writeSeqNext.Store(hdr.CommitSeq)
	db.storeCachedHeader(hdr.CommitSeq, hdr.BTreeRoot)
	if opts != nil {
		db.mvccRetention = opts.MVCCRetention
		db.cellValidator = opts.CellValidator
		db.afterPutCell = opts.AfterPutCell
		db.afterPutSeam = opts.AfterPutSeam
	}
	if opts != nil && opts.ChangelogEnabled {
		clPath := opts.ChangelogPath
		if clPath == "" {
			clPath = path + "-changelog"
		}
		syncWrites := !opts.ChangelogLazy
		cl, err := changelog.Open(clPath, syncWrites)
		if err != nil {
			_ = eng.Close()
			return nil, err
		}
		db.changelog = cl
	}
	return db, nil
}

// Close releases resources associated with the database.
// It waits for any in-flight [View], [Update], or [Batch] to finish.
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
	var err error
	if db.changelog != nil {
		err = db.changelog.Close()
		db.changelog = nil
	}
	if e := db.eng.Close(); e != nil && err == nil {
		err = e
	}
	db.eng = nil
	db.btree = nil
	return err
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

// GroupWALStats returns group-WAL flusher counters when group commit is enabled: total
// applyGroupBatch invocations, batches that combined two or more user commits, and WAL sync
// operations. It is a thin forwarder over [engine.Engine.GroupWALStats] for operators who should
// not import [internal/engine].
func (db *DB) GroupWALStats() (applyBatches, batchesWith2PlusJobs, walSynces uint64) {
	if db == nil {
		return 0, 0, 0
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return 0, 0, 0
	}
	return db.eng.GroupWALStats()
}
