package hexxladb

import (
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
)

// ChangelogConsumerCursor is one durable at-least-once consumer position.
type ChangelogConsumerCursor struct {
	ConsumerID string
	Seq        uint64
}

// GetChangelogConsumerCursor returns the durable cursor for consumerID. The
// boolean is false when the identity has not been registered. When the
// changelog is enabled, the cursor is also validated against its logical
// history; disabled handles expose the primary metadata for administration.
func (db *DB) GetChangelogConsumerCursor(consumerID string) (uint64, bool, error) {
	if err := validateChangelogConsumerID(consumerID); err != nil {
		return 0, false, err
	}
	if db == nil {
		return 0, false, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return 0, false, ErrDatabaseClosed
	}
	consumers, err := db.readChangelogConsumersLocked()
	if err != nil {
		return 0, false, err
	}
	if db.changelog != nil {
		err = db.validateChangelogConsumerHistoryLocked(consumers)
	}
	if err != nil {
		return 0, false, err
	}
	for _, consumer := range consumers {
		if consumer.ConsumerID == consumerID {
			return consumer.Seq, true, nil
		}
	}
	return 0, false, nil
}

// ListChangelogConsumers returns every durable cursor ordered by consumer ID.
// Enabled handles validate the cursors against their logical history; disabled
// handles expose the primary metadata for administration.
func (db *DB) ListChangelogConsumers() ([]ChangelogConsumerCursor, error) {
	if db == nil {
		return nil, ErrDatabaseClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return nil, ErrDatabaseClosed
	}
	consumers, err := db.readChangelogConsumersLocked()
	if err != nil {
		return nil, err
	}
	if db.changelog != nil {
		err = db.validateChangelogConsumerHistoryLocked(consumers)
	}
	if err != nil {
		return nil, err
	}
	return consumers, nil
}

// ChangelogRetentionFloor returns the minimum acknowledged sequence across all
// registered consumers. Records at or below the returned floor have been
// acknowledged by every registered consumer. The boolean is false when no
// consumers are registered.
func (db *DB) ChangelogRetentionFloor() (uint64, bool, error) {
	consumers, err := db.ListChangelogConsumers()
	if err != nil {
		return 0, false, err
	}
	if len(consumers) == 0 {
		return 0, false, nil
	}
	floor := consumers[0].Seq
	for i := 1; i < len(consumers); i++ {
		floor = min(floor, consumers[i].Seq)
	}
	return floor, true, nil
}

// AdvanceChangelogConsumer atomically compares the durable cursor with
// expectedSeq and advances it to nextSeq. An unregistered consumer has an
// expected sequence of zero; advancing zero to zero registers it at the start.
// nextSeq must not regress or exceed the current changelog head.
func (db *DB) AdvanceChangelogConsumer(consumerID string, expectedSeq, nextSeq uint64) error {
	if err := validateChangelogConsumerID(consumerID); err != nil {
		return err
	}
	if nextSeq < expectedSeq {
		return ErrChangelogCursorRegression
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if db.recoveryRequired.Load() {
		return fmt.Errorf("%w: close and reopen required", ErrCommitFinalization)
	}
	if db.changelog == nil {
		return ErrChangelogDisabled
	}
	consumers, err := db.readChangelogConsumersLocked()
	if err != nil {
		return err
	}
	if err := db.validateChangelogConsumerHistoryLocked(consumers); err != nil {
		return err
	}
	currentSeq, exists := findChangelogConsumer(consumers, consumerID)
	if (!exists && expectedSeq != 0) || (exists && currentSeq != expectedSeq) {
		return fmt.Errorf("%w: consumer %q expected %d, current %d", ErrChangelogCursorConflict, consumerID, expectedSeq, currentSeq)
	}
	if nextSeq < currentSeq {
		return ErrChangelogCursorRegression
	}
	if nextSeq > db.changelog.MaxSeq() {
		return ErrChangelogCursorBeyondHead
	}
	if exists && nextSeq == currentSeq {
		return nil
	}
	checkpointSeq, checkpointDigest, err := db.changelog.LogicalCheckpoint()
	if err != nil {
		return mapChangelogConsumerHistoryError(err)
	}
	return db.commitChangelogConsumerMetadataLocked("advance", func() error {
		if err := db.btree.Put(index.ChangelogConsumerKey(consumerID), index.EncodeChangelogConsumerCursor(nextSeq)); err != nil {
			return err
		}
		return db.btree.Put(
			index.ChangelogProjectionCheckpointKey(),
			index.EncodeChangelogProjectionCheckpoint(checkpointSeq, checkpointDigest),
		)
	})
}

// DeleteChangelogConsumer removes a durable cursor only when expectedSeq still
// matches. Deleting the final consumer removes retention protection; it does
// not delete changelog records. Deletion is available on changelog-disabled
// handles so an operator can explicitly re-bootstrap invalidated consumers.
func (db *DB) DeleteChangelogConsumer(consumerID string, expectedSeq uint64) error {
	if err := validateChangelogConsumerID(consumerID); err != nil {
		return err
	}
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if db.recoveryRequired.Load() {
		return fmt.Errorf("%w: close and reopen required", ErrCommitFinalization)
	}
	consumers, err := db.readChangelogConsumersLocked()
	if err != nil {
		return err
	}
	if db.changelog != nil {
		err = db.validateChangelogConsumerHistoryLocked(consumers)
	}
	if err != nil {
		return err
	}
	currentSeq, exists := findChangelogConsumer(consumers, consumerID)
	if !exists {
		return ErrChangelogConsumerNotFound
	}
	if currentSeq != expectedSeq {
		return fmt.Errorf("%w: consumer %q expected %d, current %d", ErrChangelogCursorConflict, consumerID, expectedSeq, currentSeq)
	}
	return db.commitChangelogConsumerMetadataLocked("delete", func() error {
		return db.btree.Delete(index.ChangelogConsumerKey(consumerID))
	})
}

func validateChangelogConsumerID(consumerID string) error {
	if !index.ValidChangelogConsumerID(consumerID) {
		return fmt.Errorf("%w: consumer ID must be 1-%d bytes and match [A-Za-z0-9][A-Za-z0-9._:-]*", ErrInvalidArgument, index.ChangelogConsumerMaxIDBytes)
	}
	return nil
}

func (db *DB) readChangelogConsumersLocked() ([]ChangelogConsumerCursor, error) {
	from, to := index.ChangelogConsumerBounds()
	consumers := make([]ChangelogConsumerCursor, 0)
	var decodeErr error
	err := db.btree.AscendRange(from, to, func(key, value []byte) bool {
		consumerID, ok := index.ParseChangelogConsumerKey(key)
		if !ok {
			decodeErr = fmt.Errorf("%w: invalid changelog consumer key", ErrCorruptDatabase)
			return false
		}
		seq, ok := index.DecodeChangelogConsumerCursor(value)
		if !ok {
			decodeErr = fmt.Errorf("%w: invalid changelog cursor for %q", ErrCorruptDatabase, consumerID)
			return false
		}
		consumers = append(consumers, ChangelogConsumerCursor{ConsumerID: consumerID, Seq: seq})
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("hexxladb: read changelog consumers: %w", err)
	}
	return consumers, decodeErr
}

func findChangelogConsumer(consumers []ChangelogConsumerCursor, consumerID string) (uint64, bool) {
	for _, consumer := range consumers {
		if consumer.ConsumerID == consumerID {
			return consumer.Seq, true
		}
	}
	return 0, false
}

func (db *DB) validateChangelogConsumerHistoryLocked(consumers []ChangelogConsumerCursor) error {
	if len(consumers) == 0 {
		return nil
	}
	value, ok, err := db.btree.Get(index.ChangelogProjectionCheckpointKey())
	if err != nil {
		return fmt.Errorf("hexxladb: read changelog projection checkpoint: %w", err)
	}
	if !ok {
		return fmt.Errorf("%w: registered consumers have no projection checkpoint", ErrCorruptDatabase)
	}
	checkpointSeq, wantDigest, ok := index.DecodeChangelogProjectionCheckpoint(value)
	if !ok {
		return fmt.Errorf("%w: invalid changelog projection checkpoint", ErrCorruptDatabase)
	}
	actualHead := db.changelog.MaxSeq()
	if checkpointSeq > actualHead {
		return fmt.Errorf("%w: projected sequence %d exceeds changelog head %d", ErrChangelogConsumerInvalidated, checkpointSeq, actualHead)
	}
	for _, consumer := range consumers {
		if consumer.Seq > actualHead {
			return fmt.Errorf("%w: consumer %q sequence %d exceeds changelog head %d", ErrChangelogConsumerInvalidated, consumer.ConsumerID, consumer.Seq, actualHead)
		}
	}
	gotDigest, err := db.changelog.LogicalDigestAt(checkpointSeq)
	if err != nil {
		return mapChangelogConsumerHistoryError(err)
	}
	if gotDigest != wantDigest {
		return fmt.Errorf("%w: retained changelog history does not match the authoritative checkpoint", ErrChangelogConsumerInvalidated)
	}
	return nil
}

func mapChangelogConsumerHistoryError(err error) error {
	return fmt.Errorf("%w: validate durable consumer history: %w", ErrChangelogCorrupt, err)
}

func (db *DB) commitChangelogConsumerMetadataLocked(action string, mutate func() error) error {
	if err := db.eng.BeginWriteTxn(); err != nil {
		return err
	}
	if err := mutate(); err != nil {
		db.eng.AbortWriteTxn()
		return fmt.Errorf("hexxladb: changelog consumer %s: %w", action, err)
	}
	if err := db.eng.CommitWriteTxn(); err != nil {
		db.recoveryRequired.Store(true)
		return fmt.Errorf("%w: changelog consumer %s commit: %w", ErrCommitFinalization, action, err)
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		db.recoveryRequired.Store(true)
		return durableCommitError("changelog consumer header refresh", err)
	}
	db.storeCachedHeader(hdr.CommitSeq, hdr.BTreeRoot)
	return nil
}
