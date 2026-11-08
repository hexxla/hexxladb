package hexxladb

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
)

const (
	migrationStateVersion    = byte(1)
	migrationStateValueBytes = 1 + sha256.Size + 8
)

var migrationStatePrefix = []byte("__meta/migration/v1-to-v2/")

type migrationState struct {
	sourceDigest  [sha256.Size]byte
	processedKeys uint64
	lastKey       []byte
}

func openMigrationDestination(path string, srcHeader engine.Header, digest [sha256.Size]byte, opts *MigrationOptions) (*DB, migrationState, bool, error) {
	destOpts := migrationDestinationOptions(srcHeader, opts)
	dest, err := openDBWithMigration(path, destOpts, true, true)
	if err == nil {
		if err := validateMigrationDestinationFormat(dest, opts); err != nil {
			_ = dest.Close()
			removeDBFiles(path)
			return nil, migrationState{}, false, err
		}
		state := migrationState{sourceDigest: digest}
		if err := storeMigrationState(dest, migrationState{}, state); err != nil {
			_ = dest.Close()
			removeDBFiles(path)
			return nil, migrationState{}, false, fmt.Errorf("migrate v1 to v2: initialize destination: %w", err)
		}
		return dest, state, false, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, migrationState{}, false, fmt.Errorf("migrate v1 to v2: create destination: %w", err)
	}
	dest, err = openDBWithMigration(path, destOpts, false, true)
	if err != nil {
		return nil, migrationState{}, false, fmt.Errorf("migrate v1 to v2: resume destination: %w", err)
	}
	if err := validateMigrationDestinationFormat(dest, opts); err != nil {
		_ = dest.Close()
		return nil, migrationState{}, false, err
	}
	state, found, err := loadMigrationState(dest.btree)
	if err != nil {
		_ = dest.Close()
		return nil, migrationState{}, false, fmt.Errorf("migrate v1 to v2: read resume state: %w", err)
	}
	if !found || state.sourceDigest != digest {
		_ = dest.Close()
		return nil, migrationState{}, false, fmt.Errorf("%w: destination is not a matching resumable migration", ErrInvalidArgument)
	}
	return dest, state, true, nil
}

func validateMigrationDestinationFormat(dest *DB, opts *MigrationOptions) error {
	hdr, err := dest.eng.ReadHeader()
	if err != nil {
		return err
	}
	want := uint32(2)
	if opts != nil && opts.targetAuthenticated {
		want = engine.AuthenticatedFormatVersion
	}
	if hdr.FormatVersion != want {
		return fmt.Errorf("%w: migration destination format is %d, want %d", ErrInvalidArgument, hdr.FormatVersion, want)
	}
	return nil
}

func migrationDestinationOptions(srcHeader engine.Header, opts *MigrationOptions) *Options {
	var destOpts Options
	if opts != nil && opts.DestinationOptions != nil {
		destOpts = *opts.DestinationOptions
	}
	destOpts.EnableMVCC = true
	destOpts.newLegacyEncryption = opts == nil || !opts.targetAuthenticated
	destOpts.ChangelogEnabled = false
	destOpts.ChangelogPath = ""
	destOpts.ChangelogLazy = false
	destOpts.CellValidator = nil
	destOpts.AfterPutCell = nil
	destOpts.AfterPutSeam = nil
	destOpts.PageSize = srcHeader.PageSize
	destOpts.MaxValueBytes = srcHeader.MaxValueBytes
	destOpts.EmbeddingDimension = srcHeader.EmbeddingDim
	destOpts.DistanceMetric = srcHeader.EmbeddingMetric
	return &destOpts
}

func hasIncompleteMigration(bt *engine.BTree) (bool, error) {
	_, found, err := loadMigrationState(bt)
	return found, err
}

func loadMigrationState(bt *engine.BTree) (migrationState, bool, error) {
	upper := bytes.Clone(migrationStatePrefix)
	upper[len(upper)-1]++
	var state migrationState
	found := false
	var stateErr error
	err := bt.AscendRange(migrationStatePrefix, upper, func(key, value []byte) bool {
		if found {
			stateErr = fmt.Errorf("%w: multiple migration checkpoints", ErrCorruptDatabase)
			return false
		}
		decoded, err := decodeMigrationState(key, value)
		if err != nil {
			stateErr = err
			return false
		}
		state = decoded
		found = true
		return true
	})
	if err != nil {
		return migrationState{}, false, err
	}
	if stateErr != nil {
		return migrationState{}, false, stateErr
	}
	return state, found, nil
}

func decodeMigrationState(key, value []byte) (migrationState, error) {
	if !bytes.HasPrefix(key, migrationStatePrefix) || len(value) != migrationStateValueBytes || value[0] != migrationStateVersion {
		return migrationState{}, fmt.Errorf("%w: invalid migration checkpoint", ErrCorruptDatabase)
	}
	var state migrationState
	copy(state.sourceDigest[:], value[1:1+sha256.Size])
	state.processedKeys = binary.BigEndian.Uint64(value[1+sha256.Size:])
	state.lastKey = bytes.Clone(key[len(migrationStatePrefix):])
	return state, nil
}

func encodeMigrationState(state migrationState) []byte {
	value := make([]byte, migrationStateValueBytes)
	value[0] = migrationStateVersion
	copy(value[1:1+sha256.Size], state.sourceDigest[:])
	binary.BigEndian.PutUint64(value[1+sha256.Size:], state.processedKeys)
	return value
}

func migrationStateKey(state migrationState) []byte {
	return append(bytes.Clone(migrationStatePrefix), state.lastKey...)
}

func storeMigrationState(dest *DB, oldState, newState migrationState) error {
	return dest.Update(func(tx *Tx) error {
		return replaceMigrationState(tx, oldState, newState)
	})
}

func replaceMigrationState(tx *Tx, oldState, newState migrationState) error {
	if oldState.sourceDigest != ([sha256.Size]byte{}) || oldState.processedKeys != 0 || len(oldState.lastKey) != 0 {
		if err := tx.deleteDirect(migrationStateKey(oldState)); err != nil {
			return err
		}
	}
	return tx.putDirect(migrationStateKey(newState), encodeMigrationState(newState))
}

func clearMigrationState(dest *DB, state migrationState) error {
	return dest.Update(func(tx *Tx) error {
		return tx.deleteDirect(migrationStateKey(state))
	})
}
