package hexxladb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hexxla/hexxladb/internal/changelog"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/engine/crashtest"
	"github.com/hexxla/hexxladb/internal/index"
)

const lazyChangelogFlushIntentLimit = 256

type pendingChangelogIntent struct {
	outboxKey []byte
	commitID  uint64
	ordinal   uint32
	intent    changelog.Intent
}

type commitFaultHooks struct {
	beforeCommitSeqPublish func() error
	beforeEngineCommit     func() error
	afterEngineCommit      func() error
	beforeChangelogAppend  func() error
	beforeOutboxCleanup    func() error
}

func (db *DB) initializeChangefeedHead() error {
	raw, ok, err := db.btree.Get(index.ChangelogHeadKey())
	if err != nil {
		return err
	}
	if !ok {
		db.changefeedSeqNext.Store(0)
		return nil
	}
	if len(raw) != 8 {
		return fmt.Errorf("%w: invalid changelog outbox head", ErrCorruptDatabase)
	}
	db.changefeedSeqNext.Store(binary.BigEndian.Uint64(raw))
	return nil
}

func (db *DB) stageChangelogOutbox(tx *Tx, wallUnixNs int64) error {
	if db.changelog == nil || len(tx.clog) == 0 {
		return nil
	}
	if uint64(len(tx.clog)) > uint64(^uint32(0))+1 {
		return errors.New("hexxladb: too many changelog mutations in one commit")
	}
	previousHead := db.changefeedSeqNext.Load()
	if previousHead == ^uint64(0) {
		return errors.New("hexxladb: changelog commit sequence exhausted")
	}
	commitID := db.changefeedSeqNext.Add(1)
	tx.changefeedHeadBefore = previousHead
	tx.changefeedHeadAdvanced = true
	tx.changelogIntents = make([]changelog.Intent, 0, len(tx.clog))
	tx.changelogOutboxKeys = make([][]byte, 0, len(tx.clog))
	maxValueBytes := db.eng.MaxValueBytes()
	for i := range tx.clog {
		intent, err := changelog.PrepareIntent(wallUnixNs, tx.clog[i], maxValueBytes)
		if err != nil {
			return err
		}
		// #nosec G115 -- len(tx.clog) is rejected above when it exceeds uint32 ordinal capacity.
		ordinal := uint32(i) //nolint:gosec // bounded by the transaction mutation-count check
		outboxKey := index.ChangelogOutboxKey(commitID, ordinal, intent.Key)
		value, err := changelog.EncodeIntentValue(intent)
		if err != nil {
			return err
		}
		if err := db.btree.Put(outboxKey, value); err != nil {
			return err
		}
		tx.changelogIntents = append(tx.changelogIntents, intent)
		tx.changelogOutboxKeys = append(tx.changelogOutboxKeys, outboxKey)
	}
	var head [8]byte
	binary.BigEndian.PutUint64(head[:], commitID)
	return db.btree.Put(index.ChangelogHeadKey(), head[:])
}

func (db *DB) resetChangefeedHead(tx *Tx) {
	if tx != nil && tx.changefeedHeadAdvanced {
		db.changefeedSeqNext.Store(tx.changefeedHeadBefore)
	}
}

func (db *DB) readPendingChangelogIntents() ([]pendingChangelogIntent, error) {
	from, to := index.ChangelogOutboxBounds()
	pending := make([]pendingChangelogIntent, 0)
	err := db.btree.AscendRange(from, to, func(key, value []byte) bool {
		commitID, ordinal, logicalKey, ok := index.ParseChangelogOutboxKey(key)
		if !ok || commitID == 0 {
			pending = append(pending, pendingChangelogIntent{})
			return false
		}
		intent, decodeErr := changelog.DecodeIntentValue(logicalKey, value)
		if decodeErr != nil {
			pending = append(pending, pendingChangelogIntent{})
			return false
		}
		pending = append(pending, pendingChangelogIntent{
			outboxKey: bytes.Clone(key),
			commitID:  commitID,
			ordinal:   ordinal,
			intent:    intent,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	for i := range pending {
		if pending[i].commitID == 0 {
			return nil, fmt.Errorf("%w: corrupt changelog outbox", ErrCorruptDatabase)
		}
		if i == 0 || pending[i].commitID != pending[i-1].commitID {
			if pending[i].ordinal != 0 {
				return nil, fmt.Errorf("%w: changelog outbox commit starts at ordinal %d", ErrCorruptDatabase, pending[i].ordinal)
			}
			continue
		}
		if pending[i].ordinal != pending[i-1].ordinal+1 {
			return nil, fmt.Errorf("%w: non-contiguous changelog outbox", ErrCorruptDatabase)
		}
	}
	return pending, nil
}

func (db *DB) recoverChangelogOutbox() error {
	pending, err := db.readPendingChangelogIntents()
	if err != nil || len(pending) == 0 {
		return err
	}
	intents := make([]changelog.Intent, len(pending))
	keys := make([][]byte, len(pending))
	for i := range pending {
		intents[i] = pending[i].intent
		keys[i] = pending[i].outboxKey
	}
	if err := db.changelog.AppendIntents(intents); err != nil {
		return durableCommitError("recover changelog append", err)
	}
	if err := db.changelog.Sync(); err != nil {
		return durableCommitError("recover changelog sync", err)
	}
	if err := db.cleanupChangelogOutbox(keys); err != nil {
		return durableCommitError("recover changelog outbox cleanup", err)
	}
	return nil
}

func (db *DB) finalizeChangelogProjection(tx *Tx) error {
	if len(tx.changelogIntents) == 0 {
		return nil
	}
	if hook := db.commitFaults; hook != nil && hook.beforeChangelogAppend != nil {
		if err := hook.beforeChangelogAppend(); err != nil {
			return durableCommitError("changelog append", err)
		}
	}
	if err := db.changelog.AppendIntents(tx.changelogIntents); err != nil {
		return durableCommitError("changelog append", err)
	}
	crashtest.At("db_changelog_appended")
	if db.changelogLazy {
		db.pendingOutboxEntries += len(tx.changelogOutboxKeys)
		if db.pendingOutboxEntries < lazyChangelogFlushIntentLimit {
			return nil
		}
		return db.syncAndCleanupPendingChangelog()
	}
	if err := db.cleanupChangelogOutbox(tx.changelogOutboxKeys); err != nil {
		return durableCommitError("changelog outbox cleanup", err)
	}
	return nil
}

func (db *DB) syncAndCleanupPendingChangelog() error {
	pending, err := db.readPendingChangelogIntents()
	if err != nil || len(pending) == 0 {
		return err
	}
	if err := db.changelog.Sync(); err != nil {
		return durableCommitError("changelog sync", err)
	}
	keys := make([][]byte, len(pending))
	for i := range pending {
		keys[i] = pending[i].outboxKey
	}
	if err := db.cleanupChangelogOutbox(keys); err != nil {
		return durableCommitError("changelog outbox cleanup", err)
	}
	db.pendingOutboxEntries = 0
	return nil
}

func (db *DB) cleanupChangelogOutbox(keys [][]byte) error {
	if len(keys) == 0 {
		return nil
	}
	if hook := db.commitFaults; hook != nil && hook.beforeOutboxCleanup != nil {
		if err := hook.beforeOutboxCleanup(); err != nil {
			return err
		}
	}
	if err := db.eng.BeginWriteTxn(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			db.eng.AbortWriteTxn()
		}
	}()
	for i := range keys {
		if err := db.btree.Delete(keys[i]); err != nil {
			return err
		}
	}
	finalHeader, err := db.eng.UpdateHeaderGet(func(*engine.Header) {})
	if err != nil {
		return err
	}
	wait, err := db.eng.CommitWriteTxnBeginAsync()
	if err != nil {
		return err
	}
	if err := wait(); err != nil {
		return err
	}
	committed = true
	db.storeCachedHeader(finalHeader.CommitSeq, finalHeader.BTreeRoot)
	return nil
}

func durableCommitError(stage string, err error) error {
	return fmt.Errorf("%w: %w: %s: %w", ErrCommitFinalization, ErrCommitDurable, stage, err)
}
