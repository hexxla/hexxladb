package hexxladb

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
)

// DB is a handle to an embedded HexxlaDB database. Construction is via [Open].
// Concurrent [View] calls are serialized with readers. [Update] and [Batch] hold the DB lock around
// the callback; see [docs/hexxladb/TX.md] for group-WAL wait semantics during engine commit.
type DB struct {
	mu            sync.RWMutex
	eng           *engine.Engine
	btree         *engine.BTree
	changelog     *changelog.Log
	useMVCC       bool          // true when on-disk format is v2+ (MVCC physical keys; see [Options.EnableMVCC]).
	mvccRetention MVCCRetention // copy of [Options.MVCCRetention] at [Open] for [SuggestedPruneBeforeSeq].
	writeSeqNext  atomic.Uint64
}

// ErrCorruptDatabase means the database or WAL failed validation on open.
var ErrCorruptDatabase = errors.New("hexxladb: corrupt database")

// Open opens or creates a database at path. On success, any redo WAL is applied.
func Open(path string, opts *Options) (*DB, error) {
	eopts, err := buildEngineOptions(path, opts)
	if err != nil {
		return nil, err
	}
	eopts = mergeEnginePrimaryFdatasync(eopts, opts)
	eopts = mergeEngineGroupWAL(eopts, opts)
	eng, err := engine.Open(path, eopts)
	if err != nil {
		if errors.Is(err, engine.ErrCorruptHeader) || errors.Is(err, engine.ErrCorruptWAL) {
			return nil, fmt.Errorf("%w: %w", ErrCorruptDatabase, err)
		}
		if errors.Is(err, engine.ErrBadEncryptionKey) {
			return nil, ErrEncryptionKeyMismatch
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
	if opts != nil {
		db.mvccRetention = opts.MVCCRetention
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
