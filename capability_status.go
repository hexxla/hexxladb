package hexxladb

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
)

// HasRetainedChangefeed reports whether consumer cursors, a logical-history
// checkpoint, or pending durable intents require the matching changefeed sidecar.
// It works on a changelog-disabled handle and does not mutate or reset history.
// An empty newly enabled feed need not have retained state yet.
func (db *DB) HasRetainedChangefeed() (bool, error) {
	if db == nil {
		return false, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return false, ErrDatabaseClosed
	}
	consumers, err := db.readChangelogConsumersLocked()
	if err != nil {
		return false, err
	}
	if len(consumers) > 0 {
		return true, nil
	}
	_, found, err := db.btree.Get(index.ChangelogProjectionCheckpointKey())
	if err != nil || found {
		return found, mapEngineDataError(err)
	}
	from, to := index.ChangelogOutboxBounds()
	retained := false
	err = db.btree.AscendRange(from, to, func(key, _ []byte) bool { retained = bytes.HasPrefix(key, from); return false })
	return retained, mapEngineDataError(err)
}

// CountEmbeddings counts authoritative current vector rows up to limit without
// decoding vectors or exposing physical keys. A positive limit is required;
// reaching it means more vectors may exist. Historical views return
// ErrHistoricalDataUnavailable. Cancellation is checked between rows.
func (tx *Tx) CountEmbeddings(ctx context.Context, limit int) (int, error) {
	if ctx == nil || limit <= 0 {
		return 0, fmt.Errorf("%w: nonnil context and positive embedding count limit required", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if tx == nil || tx.db == nil {
		return 0, ErrClosed
	}
	if err := tx.checkCurrentData(); err != nil {
		return 0, err
	}
	count := 0
	var canceled error
	err := tx.AscendRange([]byte(index.EmbedPrefix), index.EmbedKeyEnd(), func(key, _ []byte) bool {
		if canceled = ctx.Err(); canceled != nil {
			return false
		}
		if !bytes.HasPrefix(key, []byte(index.EmbedPrefix)) {
			return false
		}
		count++
		return count < limit
	})
	if err != nil {
		return 0, err
	}
	if canceled != nil {
		return 0, canceled
	}
	return count, ctx.Err()
}

// ReadSequence returns the base MVCC snapshot selected for this transaction.
// For updates it excludes writes staged by the callback. Like Tx itself, this
// accessor is valid only during the owning View/Update callback.
func (tx *Tx) ReadSequence() uint64 {
	if tx == nil {
		return 0
	}
	return tx.readSeq
}
