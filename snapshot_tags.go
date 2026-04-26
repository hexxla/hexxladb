package hexxladb

import (
	"bytes"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
)

// SnapshotTag is a named reference to an MVCC commit sequence.
type SnapshotTag struct {
	// Label is the human-friendly name supplied to [DB.TagSnapshot].
	Label string
	// CommitSeq is the commit sequence number the tag points to.
	CommitSeq uint64
}

// TagSnapshot creates or updates a named reference to the current head commit sequence.
// label must be non-empty and at most [index.SnapTagMaxLabelBytes] bytes.
// Subsequent calls with the same label overwrite the previous mapping.
//
// When MVCC is disabled the tag is still stored (commit_seq 0) and [DB.ViewAtTag]
// will open a zero-seq snapshot (same as [DB.ViewAt](0, fn)).
func (db *DB) TagSnapshot(label string) error {
	if db == nil {
		return ErrDatabaseClosed
	}
	if label == "" {
		return fmt.Errorf("%w: label must not be empty", ErrInvalidArgument)
	}
	if len(label) > index.SnapTagMaxLabelBytes {
		return ErrSnapshotTagLabelTooLong
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
	key := index.SnapTagKey(label)
	val := index.EncodeSnapTagValue(hdr.CommitSeq)
	if err := db.eng.BeginWriteTxn(); err != nil {
		return err
	}
	if err := db.btree.Put(key, val); err != nil {
		db.eng.AbortWriteTxn()
		return fmt.Errorf("hexxladb: TagSnapshot put: %w", err)
	}
	if err := db.eng.CommitWriteTxn(); err != nil {
		db.eng.AbortWriteTxn()
		return fmt.Errorf("hexxladb: TagSnapshot commit: %w", err)
	}
	return nil
}

// ViewAtTag opens a read-only transaction pinned to the commit sequence recorded by [DB.TagSnapshot]
// for label. Returns [ErrSnapshotTagNotFound] if label has not been tagged.
func (db *DB) ViewAtTag(label string, fn func(*Tx) error) error {
	if fn == nil {
		return ErrNilCallback
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	seq, err := db.resolveSnapTag(label)
	if err != nil {
		return err
	}
	return db.ViewAt(seq, fn)
}

// ListSnapshotTags returns all named snapshot tags sorted by label.
func (db *DB) ListSnapshotTags() ([]SnapshotTag, error) {
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	from := []byte(index.SnapTagPrefix)
	to := snapTagPrefixEnd()
	var tags []SnapshotTag
	if err := db.btree.AscendRange(from, to, func(k, v []byte) bool {
		label, ok := index.ParseSnapTagLabel(k)
		if !ok {
			return true
		}
		seq, ok := index.DecodeSnapTagValue(v)
		if !ok {
			return true
		}
		tags = append(tags, SnapshotTag{Label: label, CommitSeq: seq})
		return true
	}); err != nil {
		return nil, fmt.Errorf("hexxladb: ListSnapshotTags: %w", err)
	}
	return tags, nil
}

// DeleteSnapshotTag removes the named snapshot tag. Returns [ErrSnapshotTagNotFound] if the
// label does not exist. The underlying commit sequence and its data are not affected.
func (db *DB) DeleteSnapshotTag(label string) error {
	if db == nil {
		return ErrDatabaseClosed
	}
	if label == "" {
		return fmt.Errorf("%w: label must not be empty", ErrInvalidArgument)
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	key := index.SnapTagKey(label)
	val, ok, err := db.btree.Get(key)
	if err != nil {
		return fmt.Errorf("hexxladb: DeleteSnapshotTag get: %w", err)
	}
	if !ok || val == nil {
		return ErrSnapshotTagNotFound
	}
	if err := db.eng.BeginWriteTxn(); err != nil {
		return err
	}
	if err := db.btree.Delete(key); err != nil {
		db.eng.AbortWriteTxn()
		return fmt.Errorf("hexxladb: DeleteSnapshotTag delete: %w", err)
	}
	if err := db.eng.CommitWriteTxn(); err != nil {
		db.eng.AbortWriteTxn()
		return fmt.Errorf("hexxladb: DeleteSnapshotTag commit: %w", err)
	}
	return nil
}

// resolveSnapTag reads the commit_seq for label under RLock.
func (db *DB) resolveSnapTag(label string) (uint64, error) {
	if label == "" {
		return 0, fmt.Errorf("%w: label must not be empty", ErrInvalidArgument)
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return 0, ErrDatabaseClosed
	}
	key := index.SnapTagKey(label)
	val, ok, err := db.btree.Get(key)
	if err != nil {
		return 0, fmt.Errorf("hexxladb: ViewAtTag get: %w", err)
	}
	if !ok || val == nil {
		return 0, ErrSnapshotTagNotFound
	}
	seq, ok := index.DecodeSnapTagValue(val)
	if !ok {
		return 0, fmt.Errorf("hexxladb: ViewAtTag malformed tag value for %q", label)
	}
	return seq, nil
}

// snapTagPrefixEnd returns the exclusive upper bound for the snap-tag prefix scan.
func snapTagPrefixEnd() []byte {
	prefix := []byte(index.SnapTagPrefix)
	end := bytes.Clone(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return nil
}
