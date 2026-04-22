package hexxladb

import (
	"bytes"
	"fmt"
	"time"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// Tx is a database transaction or snapshot view. Obtain it only from [DB.View], [DB.ViewAt], [DB.Update], or [DB.Batch].
// A Tx is valid only for the duration of the callback; do not store it past the callback return.
type Tx struct {
	db       *DB
	writable bool
	clog     []changelog.Entry
	// readSeq is the MVCC snapshot (largest visible commit_seq). Ignored when the DB is format v1.
	readSeq uint64
	// writeSeq is the commit_seq assigned to writes in this Update (hdr.CommitSeq+1). Zero in read-only txs.
	writeSeq uint64
	// cellOverlay holds uncommitted cell writes in the current Update (read-your-writes).
	cellOverlay map[lattice.PackedCoord]record.CellRecord
}

// View runs fn inside a read-only transaction. Many concurrent View calls are allowed;
// they exclude only an active [Update] or [Batch]. See docs/hexxladb/TX.md.
func (db *DB) View(fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return err
	}
	tx := &Tx{db: db, writable: false, readSeq: hdr.CommitSeq}
	return fn(tx)
}

// ViewAt runs fn inside a read-only transaction pinned to read_seq (commit sequence).
// read_seq must not exceed the database header's CommitSeq or [ErrReadSeqFuture] is returned.
func (db *DB) ViewAt(readSeq uint64, fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return err
	}
	if readSeq > hdr.CommitSeq {
		return ErrReadSeqFuture
	}
	tx := &Tx{db: db, writable: false, readSeq: readSeq}
	return fn(tx)
}

// ViewAtTime runs fn inside a read-only transaction pinned to the most recent commit at or before asOf.
// For format-v1 databases this resolves to the latest committed snapshot.
func (db *DB) ViewAtTime(asOf time.Time, fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return err
	}
	readSeq := hdr.CommitSeq
	if db.useMVCC {
		readSeq, err = db.resolveReadSeqAtOrBeforeUnixNano(asOf.UTC().UnixNano())
		if err != nil {
			return err
		}
	}
	tx := &Tx{db: db, writable: false, readSeq: readSeq}
	return fn(tx)
}

// Update runs fn inside a read-write transaction. Exclusive: no concurrent View, Update, or Batch.
func (db *DB) Update(fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		return err
	}
	var tx *Tx
	var metaTimelineKey []byte
	if db.useMVCC {
		tx = &Tx{
			db:          db,
			writable:    true,
			readSeq:     hdr.CommitSeq,
			writeSeq:    hdr.CommitSeq + 1,
			cellOverlay: make(map[lattice.PackedCoord]record.CellRecord),
		}
		// Insert commit-time row before MVCC physical cells so btree key order matches sorted key
		// order (__meta/commit-time/… sorts before cell/…). Writing cells first then meta produced a
		// shape where leaf delete/rebalance could corrupt pages (see internal/engine/btree_test.go).
		wall := time.Now().UTC().UnixNano()
		metaTimelineKey = index.CommitTimeKey(wall, tx.writeSeq)
		if err := db.btree.Put(metaTimelineKey, emptySecondaryVal); err != nil {
			return fmt.Errorf("%w: commit timeline: %w", ErrCommitFinalization, err)
		}
	} else {
		tx = &Tx{db: db, writable: true}
	}
	fnErr := fn(tx)
	if fnErr != nil {
		if metaTimelineKey != nil {
			_ = db.btree.Delete(metaTimelineKey)
		}
		return fnErr
	}
	if db.changelog != nil && len(tx.clog) > 0 {
		wall := time.Now().UnixNano()
		if err := db.changelog.AppendBatch(wall, tx.clog); err != nil {
			return fmt.Errorf("%w: changelog append: %w", ErrCommitFinalization, err)
		}
	}
	if db.useMVCC {
		if err := db.eng.UpdateHeader(func(h *engine.Header) {
			h.CommitSeq = tx.writeSeq
		}); err != nil {
			return fmt.Errorf("%w: header update: %w", ErrCommitFinalization, err)
		}
	}
	return nil
}

// Batch runs fn inside a read-write transaction. It is equivalent to [DB.Update]; the name
// matches spec and Bolt-style expectations for a batched write entrypoint. Semantics and
// locking are identical to Update (exclusive writer).
func (db *DB) Batch(fn func(*Tx) error) error {
	return db.Update(fn)
}

// Get returns the value for key in the ordered store, or (nil, false, nil) if missing.
func (tx *Tx) Get(key []byte) (val []byte, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	e := tx.db.activeEng()
	if e == nil {
		return nil, false, ErrDatabaseClosed
	}
	return tx.db.btree.Get(key)
}

// Put inserts or replaces a key/value pair. Only allowed inside [DB.Update].
//
// MVCC (format v2): low-level insertion order matters for internal-tree invariants.
// Prefer [Tx.PutCell], [Tx.PutFacet], and other primitives over raw Put for application data.
// See docs/hexxladb/MVCC_DESIGN.md (engineering debt: cell/ before __meta/ ordering).
func (tx *Tx) Put(key, val []byte) error {
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if !tx.writable {
		return ErrTxReadOnly
	}
	e := tx.db.activeEng()
	if e == nil {
		return ErrDatabaseClosed
	}
	if tx.db.useMVCC && bytes.HasPrefix(key, []byte(index.CellPrefix)) {
		if _, _, err := index.ParseCellVersionKey(key); err != nil {
			return fmt.Errorf("%w: MVCC databases require version-suffixed cell/ keys — use Tx.PutCell: %w", ErrInvalidArgument, err)
		}
	}
	return tx.db.btree.Put(key, val)
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
	return tx.db.btree.AscendRange(from, to, fn)
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

func (db *DB) resolveReadSeqAtOrBeforeUnixNano(unixNano int64) (uint64, error) {
	from, to := index.CommitTimeScanBounds(unixNano)
	var readSeq uint64
	err := db.btree.AscendRange(from, to, func(k, _ []byte) bool {
		_, seq, ok := index.ParseCommitTimeKey(k)
		if !ok {
			return true
		}
		if seq > readSeq {
			readSeq = seq
		}
		return true
	})
	if err != nil {
		return 0, err
	}
	return readSeq, nil
}
