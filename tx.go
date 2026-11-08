package hexxladb

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/engine/crashtest"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// Tx is a database transaction or snapshot view. Obtain it only from [DB.View], [DB.ViewAt], [DB.Update], or [DB.Batch].
// A Tx is valid only for the duration of the callback; do not store it past the callback return.
type Tx struct {
	db                     *DB
	writable               bool
	clog                   []changelog.Entry
	changelogIntents       []changelog.Intent
	changelogOutboxKeys    [][]byte
	changefeedHeadBefore   uint64
	changefeedHeadAdvanced bool
	// readSeq is the MVCC snapshot (largest visible commit_seq). Ignored when the DB is format v1.
	readSeq uint64
	// writeSeq is the commit_seq assigned to writes in this Update (hdr.CommitSeq+1). Zero in read-only txs.
	writeSeq uint64
	// cachedBTreeRoot is hdr.BTreeRoot captured when a read-only tx opens (View paths); unused for writers.
	cachedBTreeRoot uint64
	// cellOverlay holds owned encoded writes for read-your-writes.
	cellOverlay map[lattice.PackedCoord][]byte
	// cellDeleted tracks coordinates deleted via [Tx.DeleteCell] in the current Update.
	// Checked before cellOverlay and btree scan so same-tx delete is visible.
	cellDeleted  map[lattice.PackedCoord]bool
	cellWriteErr error // storage or hook failure requires abort even if the callback ignores it
}

// View runs fn inside a read-only transaction. Many concurrent View calls are allowed;
// they exclude only an active [Update] or [Batch]. See docs/hexxladb/TX.md.
func (db *DB) View(fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	ch := db.cachedHdr.Load()
	tx := &Tx{db: db, writable: false, readSeq: ch.commitSeq, cachedBTreeRoot: ch.btreeRoot}
	return fn(tx)
}

// ViewAt runs fn inside a read-only transaction pinned to read_seq (commit sequence).
// read_seq must not exceed the database header's CommitSeq or [ErrReadSeqFuture] is returned.
// On MVCC databases, typed edge and embedding reads on older sequences return
// [ErrHistoricalDataUnavailable].
func (db *DB) ViewAt(readSeq uint64, fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	ch := db.cachedHdr.Load()
	if readSeq > ch.commitSeq {
		return ErrReadSeqFuture
	}
	tx := &Tx{db: db, writable: false, readSeq: readSeq, cachedBTreeRoot: ch.btreeRoot}
	return fn(tx)
}

// ViewAtTime runs fn inside a read-only transaction pinned to the most recent commit at or before asOf.
// For format-v1 databases this resolves to the latest committed snapshot.
func (db *DB) ViewAtTime(asOf time.Time, fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	ch := db.cachedHdr.Load()
	readSeq := ch.commitSeq
	if db.useMVCC {
		var err error
		readSeq, err = db.resolveReadSeqAtOrBefore(asOf)
		if err != nil {
			return err
		}
	}
	tx := &Tx{db: db, writable: false, readSeq: readSeq, cachedBTreeRoot: ch.btreeRoot}
	return fn(tx)
}

func (tx *Tx) checkCurrentData() error {
	if !tx.writable && tx.db.useMVCC && tx.readSeq != tx.db.cachedHdr.Load().commitSeq {
		return ErrHistoricalDataUnavailable
	}
	return nil
}

// Update runs fn inside an exclusive read-write transaction. The database lock remains held
// through engine commit and finalization; see [docs/hexxladb/TX.md].
func (db *DB) Update(fn func(*Tx) error) error { //nolint:gocyclo // durability and recovery ordering is intentionally one state machine.
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.writeStats.calls.Add(1)
	lockStarted := time.Now()
	db.mu.Lock()
	db.writeStats.lockWait.Add(int64(time.Since(lockStarted)))
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if db.recoveryRequired.Load() {
		return fmt.Errorf("%w: close and reopen required", ErrCommitFinalization)
	}
	if err := db.eng.BeginWriteTxn(); err != nil {
		return err
	}
	committed := false
	var writeSeqBefore uint64
	var tx *Tx
	defer func() {
		if !committed {
			db.eng.AbortWriteTxn()
			db.resetChangefeedHead(tx)
			if !db.recoveryRequired.Load() {
				db.writeSeqNext.Store(writeSeqBefore)
			}
		}
	}()

	ch := db.cachedHdr.Load()
	writeSeqBefore = ch.commitSeq
	var metaTimelineKey []byte
	commitWall := time.Now().UTC().UnixNano()
	if db.useMVCC {
		wseq := db.writeSeqNext.Add(1)
		tx = &Tx{
			db:          db,
			writable:    true,
			readSeq:     ch.commitSeq,
			writeSeq:    wseq,
			cellOverlay: make(map[lattice.PackedCoord][]byte),
		}
		// Insert commit-time row before MVCC physical cells so btree key order matches sorted key
		// order (__meta/commit-time/… sorts before cell/…). Writing cells first then meta produced a
		// shape where leaf delete/rebalance could corrupt pages (see internal/engine/btree_test.go).
		metaTimelineKey = index.CommitTimeKey(commitWall, tx.writeSeq)
		if err := db.btree.Put(metaTimelineKey, emptySecondaryVal); err != nil {
			return fmt.Errorf("%w: commit timeline: %w", ErrCommitFinalization, err)
		}
	} else {
		tx = &Tx{db: db, writable: true}
	}
	callbackStarted := time.Now()
	fnErr := fn(tx)
	db.writeStats.callback.Add(int64(time.Since(callbackStarted)))
	if err := cmp.Or(fnErr, tx.cellWriteErr); err != nil {
		if metaTimelineKey != nil {
			_ = db.btree.Delete(metaTimelineKey)
		}
		return err
	}
	if err := db.stageChangelogOutbox(tx, commitWall); err != nil {
		return fmt.Errorf("%w: durable changelog intent: %w", ErrCommitFinalization, err)
	}
	if hook := db.commitFaults; hook != nil && hook.beforeCommitSeqPublish != nil {
		if err := hook.beforeCommitSeqPublish(); err != nil {
			return fmt.Errorf("%w: header publication: %w", ErrCommitFinalization, err)
		}
	}
	finalHeader, err := db.eng.UpdateHeaderGet(func(h *engine.Header) {
		if db.useMVCC {
			h.CommitSeq = tx.writeSeq
		}
	})
	if err != nil {
		return fmt.Errorf("%w: header publication: %w", ErrCommitFinalization, err)
	}
	if hook := db.commitFaults; hook != nil && hook.beforeEngineCommit != nil {
		if err := hook.beforeEngineCommit(); err != nil {
			return fmt.Errorf("%w: engine commit: %w", ErrCommitFinalization, err)
		}
	}
	crashtest.At("db_header_staged")
	// Keep db.mu held while the group-WAL job is applied. A later writer must not begin from
	// staged B+ tree pages or a header that has not completed DB-level finalization.
	durabilityStarted := time.Now()
	wait, wErr := db.eng.CommitWriteTxnBeginAsync()
	if wErr != nil {
		db.writeStats.durability.Add(int64(time.Since(durabilityStarted)))
		return fmt.Errorf("%w: engine commit: %w", ErrCommitFinalization, wErr)
	}
	cErr := wait()
	db.writeStats.durability.Add(int64(time.Since(durabilityStarted)))
	if cErr != nil {
		db.recoveryRequired.Store(true)
		return fmt.Errorf("%w: engine commit: %w", ErrCommitFinalization, cErr)
	}
	committed = true
	db.writeStats.commits.Add(1)
	db.storeCachedHeader(finalHeader.CommitSeq, finalHeader.BTreeRoot)
	crashtest.At("db_engine_committed")
	finalizationStarted := time.Now()
	defer func() { db.writeStats.finalization.Add(int64(time.Since(finalizationStarted))) }()
	if hook := db.commitFaults; hook != nil && hook.afterEngineCommit != nil {
		if err := hook.afterEngineCommit(); err != nil {
			db.recoveryRequired.Store(true)
			return durableCommitError("after engine commit", err)
		}
	}
	if err := db.finalizeChangelogProjection(tx); err != nil {
		db.recoveryRequired.Store(true)
		return err
	}
	return nil
}

// Batch runs fn inside a read-write transaction. It is equivalent to [DB.Update]; the name
// matches spec and Bolt-style expectations for a batched write entrypoint. Semantics and
// locking are identical to Update (exclusive writer).
func (db *DB) Batch(fn func(*Tx) error) error {
	return db.Update(fn)
}

// Get returns a physical key from the ordered store, or (nil, false, nil) if missing.
// Use typed getters for MVCC visibility.
func (tx *Tx) Get(key []byte) (val []byte, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	e := tx.db.activeEng()
	if e == nil {
		return nil, false, ErrDatabaseClosed
	}
	if !tx.writable {
		val, ok, err = tx.db.btree.GetUsingRoot(tx.cachedBTreeRoot, key)
		return val, ok, mapEngineDataError(err)
	}
	val, ok, err = tx.db.btree.Get(key)
	return val, ok, mapEngineDataError(err)
}

// Put inserts or replaces an application-owned key/value pair inside [DB.Update].
// Use a namespace such as app/. Database-owned namespaces return [ErrInvalidArgument].
// Application values do not participate in typed record validation, MVCC, or changefeed.
func (tx *Tx) Put(key, val []byte) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if index.IsReservedKey(key) {
		return fmt.Errorf("%w: database-owned key; use the typed write API", ErrInvalidArgument)
	}
	return tx.putDirect(key, val)
}

// getDirect reads a value by key without public API guards.
// Used by internal subsystems (HNSW storage adapter, embeddings).
func (tx *Tx) getDirect(key []byte) (val []byte, ok bool, err error) {
	if !tx.writable {
		val, ok, err = tx.db.btree.GetUsingRoot(tx.cachedBTreeRoot, key)
		return val, ok, mapEngineDataError(err)
	}
	val, ok, err = tx.db.btree.Get(key)
	return val, ok, mapEngineDataError(err)
}

// putDirect writes a physical row for typed primitives and trusted maintenance.
func (tx *Tx) putDirect(key, val []byte) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	return mapEngineDataError(tx.db.btree.Put(key, val))
}

// deleteDirect removes a key without going through the public Tx API.
// Internal secondary-index maintenance (cell_secondary, seam_secondary) uses this
// instead of reaching directly into tx.db.btree.Delete.
func (tx *Tx) deleteDirect(key []byte) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	return mapEngineDataError(tx.db.btree.Delete(key))
}

// AscendRange calls fn for keys in [from, to] inclusive (byte order). If from is nil, starts at the smallest key.
func (tx *Tx) AscendRange(from, to []byte, fn func(k, v []byte) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	e := tx.db.activeEng()
	if e == nil {
		return ErrDatabaseClosed
	}
	if !tx.writable && tx.cachedBTreeRoot != 0 {
		return mapEngineDataError(tx.db.btree.AscendRangeFromRoot(tx.cachedBTreeRoot, from, to, fn))
	}
	return mapEngineDataError(tx.db.btree.AscendRange(from, to, fn))
}

func mapEngineDataError(err error) error {
	if errors.Is(err, engine.ErrPageAuthentication) || errors.Is(err, engine.ErrCorruptTree) ||
		errors.Is(err, engine.ErrBadPageSize) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", ErrCorruptDatabase, err)
	}
	return err
}

// descendRange calls fn for keys in [from, to] inclusive in reverse byte order.
// Read-only transactions use their pinned B+ tree root; writers use staged tree state.
func (tx *Tx) descendRange(from, to []byte, fn func(k, v []byte) bool) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if !tx.writable {
		return mapEngineDataError(tx.db.btree.DescendRangeFromRoot(tx.cachedBTreeRoot, from, to, fn))
	}
	return mapEngineDataError(tx.db.btree.DescendRange(from, to, fn))
}

// Writable reports whether this transaction was started with [DB.Update].
func (tx *Tx) Writable() bool {
	return tx != nil && tx.writable
}

func (tx *Tx) noteChangelog(op byte, key, encoded []byte) {
	if tx == nil || !tx.writable || tx.db == nil || tx.db.changelog == nil {
		return
	}
	tx.clog = append(tx.clog, changelog.Entry{
		Op:      op,
		Key:     append([]byte(nil), key...),
		Encoded: append([]byte(nil), encoded...),
	})
}

func (db *DB) resolveReadSeqAtOrBefore(asOf time.Time) (uint64, error) {
	if asOf.Before(time.Unix(0, math.MinInt64)) {
		return 0, nil
	}
	unixNano := int64(math.MaxInt64)
	if !asOf.After(time.Unix(0, math.MaxInt64)) {
		unixNano = asOf.UnixNano()
	}
	readSeq, err := db.resolveCommitTimePartition(unixNano)
	if err != nil || readSeq != 0 || unixNano < 0 {
		return readSeq, err
	}
	// No nonnegative commit qualifies. The most recent negative commit,
	// if any, predates every nonnegative as-of timestamp.
	return db.resolveCommitTimePartition(-1)
}

func (db *DB) resolveCommitTimePartition(unixNano int64) (uint64, error) {
	from, to := index.CommitTimeScanBounds(unixNano)
	ch := db.cachedHdr.Load()
	// Descend from the upper bound so the first hit is the largest commit ≤ asOf.
	// This is O(log N) instead of O(commits before asOf) with a forward scan.
	var readSeq uint64
	err := db.btree.DescendRangeFromRoot(ch.btreeRoot, from, to, func(k, _ []byte) bool {
		_, seq, ok := index.ParseCommitTimeKey(k)
		if !ok {
			return true
		}
		readSeq = seq
		return false // stop at the first (largest) match
	})
	if err != nil {
		return 0, mapEngineDataError(err)
	}
	return readSeq, nil
}

func (tx *Tx) requireWritable() error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if !tx.writable {
		return ErrTxReadOnly
	}
	if tx.db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	return nil
}
