package hexxladb

import (
	"bytes"
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

type migrationPair struct {
	key   []byte
	value []byte
}

func copyMigrationRows(ctx context.Context, src, dest *DB, state *migrationState, batchSize int, resumed bool, onProgress func(MigrationProgress)) error {
	return src.View(func(tx *Tx) error {
		batch := make([]migrationPair, 0, batchSize)
		var copyErr error
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			copyErr = writeMigrationBatch(ctx, dest, batch, state)
			if copyErr == nil && onProgress != nil {
				onProgress(MigrationProgress{ProcessedKeys: state.processedKeys, Resumed: resumed})
			}
			batch = batch[:0]
			return copyErr == nil
		}
		err := tx.AscendRange(state.lastKey, nil, func(key, value []byte) bool {
			if len(state.lastKey) > 0 && bytes.Equal(key, state.lastKey) {
				return true
			}
			if err := ctx.Err(); err != nil {
				copyErr = err
				return false
			}
			batch = append(batch, migrationPair{key: bytes.Clone(key), value: bytes.Clone(value)})
			return len(batch) < batchSize || flush()
		})
		if err != nil {
			return fmt.Errorf("migrate v1 to v2: scan source: %w", err)
		}
		if copyErr != nil {
			return fmt.Errorf("migrate v1 to v2: copy batch: %w", copyErr)
		}
		if !flush() {
			return fmt.Errorf("migrate v1 to v2: copy batch: %w", copyErr)
		}
		return nil
	})
}

func writeMigrationBatch(ctx context.Context, dest *DB, batch []migrationPair, state *migrationState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	next := migrationState{
		sourceDigest:  state.sourceDigest,
		processedKeys: state.processedKeys + uint64(len(batch)), //nolint:gosec // batch is non-negative and bounded to 4096.
		lastKey:       bytes.Clone(batch[len(batch)-1].key),
	}
	err := dest.Update(func(tx *Tx) error {
		for i := range batch {
			if err := applyMigrationPair(ctx, tx, batch[i]); err != nil {
				return err
			}
		}
		return replaceMigrationState(tx, *state, next)
	})
	if err == nil {
		*state = next
	}
	return err
}

func applyMigrationPair(ctx context.Context, tx *Tx, pair migrationPair) error {
	switch {
	case bytes.HasPrefix(pair.key, []byte(index.CellPrefix)):
		rec, err := record.DecodeCell(pair.value)
		if err != nil {
			return err
		}
		return tx.PutCell(ctx, rec)
	case bytes.HasPrefix(pair.key, []byte(index.FacetPrefix)):
		rec, err := record.DecodeFacet(pair.value)
		if err != nil {
			return err
		}
		return tx.PutFacet(rec)
	case bytes.HasPrefix(pair.key, []byte(index.SeamPrefix)):
		rec, err := record.DecodeSeam(pair.value)
		if err != nil {
			return err
		}
		return tx.PutSeam(ctx, rec)
	case isMigrationDerivedKey(pair.key), isMigrationChangelogKey(pair.key):
		return nil
	default:
		return tx.Put(pair.key, pair.value)
	}
}

func isMigrationDerivedKey(key []byte) bool {
	prefixes := [...]string{
		index.SourcePrefix,
		index.TimePrefix,
		index.TagPrefix,
		index.SeamByCellsPrefix,
		index.SeamSourcePrefix,
		index.SeamTimePrefix,
	}
	for _, prefix := range prefixes {
		if bytes.HasPrefix(key, []byte(prefix)) {
			return true
		}
	}
	return false
}

func isMigrationChangelogKey(key []byte) bool {
	if bytes.Equal(key, index.ChangelogHeadKey()) || bytes.Equal(key, index.ChangelogProjectionCheckpointKey()) {
		return true
	}
	if _, ok := index.ParseChangelogConsumerKey(key); ok {
		return true
	}
	_, _, _, ok := index.ParseChangelogOutboxKey(key)
	return ok
}

func verifyMigration(ctx context.Context, src, dest *DB, resetChangelog bool) error {
	return src.View(func(srcTx *Tx) error {
		return dest.View(func(destTx *Tx) error {
			var verifyErr error
			err := srcTx.AscendRange(nil, nil, func(key, value []byte) bool {
				if err := ctx.Err(); err != nil {
					verifyErr = err
					return false
				}
				if isMigrationDerivedKey(key) || isMigrationChangelogKey(key) {
					return true
				}
				verifyErr = verifyMigrationPair(destTx, key, value)
				return verifyErr == nil
			})
			if err != nil {
				return err
			}
			if verifyErr != nil {
				return verifyErr
			}
			if !resetChangelog {
				return verifyNoChangelogState(srcTx)
			}
			return nil
		})
	})
}

func verifyMigrationPair(destTx *Tx, key, value []byte) error {
	var got []byte
	var ok bool
	var err error
	switch {
	case bytes.HasPrefix(key, []byte(index.CellPrefix)):
		coord, parseErr := index.ParseCellKey(key)
		if parseErr != nil {
			return parseErr
		}
		got, _, ok, err = destTx.getCellVisibleRaw(coord)
	case bytes.HasPrefix(key, []byte(index.FacetPrefix)):
		coord, facetID, parseErr := index.ParseFacetKey(key)
		if parseErr != nil {
			return parseErr
		}
		got, ok, err = destTx.getFacetVisibleRaw(coord, facetID)
	case bytes.HasPrefix(key, []byte(index.SeamPrefix)):
		id := string(key[len(index.SeamPrefix):])
		got, _, ok, err = destTx.getSeamVisibleRaw(id)
	default:
		got, ok, err = destTx.Get(key)
	}
	if err != nil {
		return err
	}
	if !ok || !bytes.Equal(got, value) {
		return fmt.Errorf("logical row %x does not match source", key)
	}
	return nil
}

func verifyNoChangelogState(srcTx *Tx) error {
	var stateErr error
	err := srcTx.AscendRange(nil, nil, func(key, value []byte) bool {
		if migrationChangelogState(key, value) {
			stateErr = ErrMigrationChangelogState
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	return stateErr
}
