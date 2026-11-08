package hexxladb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
)

func openMigrationSource(
	ctx context.Context,
	path string,
	opts *MigrationOptions,
	snapshotDirectory string,
) (*DB, engine.Header, [sha256.Size]byte, func(), error) {
	var sourceOpts *Options
	resetChangelog := false
	if opts != nil {
		sourceOpts = opts.SourceOptions
		resetChangelog = opts.ResetChangelog
	}
	if sourceOpts == nil {
		sourceOpts = &Options{PageCacheSize: -1}
	} else {
		cloned := *sourceOpts
		cloned.PageCacheSize = -1
		sourceOpts = &cloned
	}
	snapshotPath, cleanupSnapshot, err := createMigrationSourceSnapshot(ctx, path, snapshotDirectory)
	if err != nil {
		return nil, engine.Header{}, [sha256.Size]byte{}, nil, err
	}
	src, err := Open(snapshotPath, sourceOpts)
	if err != nil {
		cleanupSnapshot()
		return nil, engine.Header{}, [sha256.Size]byte{}, nil, fmt.Errorf("migrate v1 to v2: open source snapshot: %w", err)
	}
	hdr, err := src.eng.ReadHeader()
	if err != nil {
		_ = src.Close()
		cleanupSnapshot()
		return nil, engine.Header{}, [sha256.Size]byte{}, nil, fmt.Errorf("migrate v1 to v2: read source header: %w", err)
	}
	if hdr.FormatVersion != 1 {
		_ = src.Close()
		cleanupSnapshot()
		return nil, engine.Header{}, [sha256.Size]byte{}, nil, fmt.Errorf("%w: migration source format is %d, want 1", ErrInvalidArgument, hdr.FormatVersion)
	}
	digest, err := migrationSourceDigest(ctx, src, resetChangelog)
	if err != nil {
		_ = src.Close()
		cleanupSnapshot()
		return nil, engine.Header{}, [sha256.Size]byte{}, nil, err
	}
	return src, hdr, digest, cleanupSnapshot, nil
}

func createMigrationSourceSnapshot(ctx context.Context, sourcePath, snapshotDirectory string) (string, func(), error) {
	lockedSource, err := engine.OpenLockedDatabaseFile(sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("migrate v1 to v2: lock source: %w", mapEngineOpenError(sourcePath, err))
	}
	lockedInfo, err := lockedSource.Stat()
	if err != nil {
		_ = lockedSource.Close()
		return "", nil, fmt.Errorf("migrate v1 to v2: stat locked source: %w", err)
	}
	if lockedInfo.Size() < 0 {
		_ = lockedSource.Close()
		return "", nil, fmt.Errorf("%w: locked migration source has a negative size", ErrInvalidArgument)
	}
	walBytes, err := optionalMaintenanceFileBytes(engine.WalPath(sourcePath))
	if err != nil {
		_ = lockedSource.Close()
		return "", nil, fmt.Errorf("migrate v1 to v2: stat locked source WAL: %w", err)
	}
	lockedBytes := uint64(lockedInfo.Size()) //nolint:gosec // size is checked non-negative above.
	if _, err := inspectMaintenanceSpace([]maintenanceSpacePart{{
		directory: snapshotDirectory,
		purpose:   "migration source snapshot",
		required:  saturatingAdd(lockedBytes, walBytes),
	}}); err != nil {
		_ = lockedSource.Close()
		return "", nil, fmt.Errorf("migrate v1 to v2: %w", err)
	}
	dir, err := os.MkdirTemp(snapshotDirectory, ".hexxladb-migrate-v1-")
	if err != nil {
		_ = lockedSource.Close()
		return "", nil, fmt.Errorf("migrate v1 to v2: create source snapshot directory: %w", err)
	}
	cleanup := func() {
		_ = lockedSource.Close()
		_ = os.RemoveAll(dir)
	}
	snapshotPath := filepath.Join(dir, "source.db")
	err = copyMigrationSnapshotFile(ctx, lockedSource, snapshotPath)
	if err == nil {
		// #nosec G304 -- WAL path is derived from the locked caller path.
		wal, openErr := os.Open(engine.WalPath(sourcePath))
		switch {
		case errors.Is(openErr, os.ErrNotExist):
		case openErr != nil:
			err = openErr
		default:
			err = copyMigrationSnapshotFile(ctx, wal, engine.WalPath(snapshotPath))
			if closeErr := wal.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("migrate v1 to v2: snapshot source: %w", err)
	}
	return snapshotPath, cleanup, nil
}

func copyMigrationSnapshotFile(ctx context.Context, source *os.File, destinationPath string) (retErr error) {
	// #nosec G304 -- destination is inside a private temporary directory.
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := destination.Close(); retErr == nil && closeErr != nil {
			retErr = closeErr
		}
	}()
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	buffer := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if _, err := destination.Write(buffer[:read]); err != nil {
				return err
			}
		}
		switch {
		case errors.Is(readErr, io.EOF):
			return destination.Sync()
		case readErr != nil:
			return readErr
		}
	}
}

func migrationSourceDigest(ctx context.Context, src *DB, resetChangelog bool) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	err := src.View(func(tx *Tx) error {
		var err error
		digest, err = migrationSourceDigestFromTx(ctx, tx, resetChangelog)
		return err
	})
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("migrate v1 to v2: inspect source: %w", err)
	}
	return digest, nil
}

func migrationSourceDigestFromTx(ctx context.Context, tx *Tx, resetChangelog bool) ([sha256.Size]byte, error) {
	h := sha256.New()
	var scanErr error
	err := tx.AscendRange(nil, nil, func(key, value []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		if migrationChangelogState(key, value) && !resetChangelog {
			scanErr = ErrMigrationChangelogState
			return false
		}
		if bytes.HasPrefix(key, migrationStatePrefix) {
			scanErr = fmt.Errorf("%w: source uses reserved migration keyspace", ErrInvalidArgument)
			return false
		}
		writeMigrationDigestRow(h, key, value)
		return true
	})
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	if scanErr != nil {
		return [sha256.Size]byte{}, scanErr
	}
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

func writeMigrationDigestRow(h hash.Hash, key, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(key)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(key)
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write(value)
}

func migrationChangelogState(key, value []byte) bool {
	if bytes.Equal(key, index.ChangelogHeadKey()) {
		return len(value) != 8 || binary.BigEndian.Uint64(value) != 0
	}
	if bytes.Equal(key, index.ChangelogProjectionCheckpointKey()) {
		return true
	}
	if _, ok := index.ParseChangelogConsumerKey(key); ok {
		return true
	}
	_, _, _, ok := index.ParseChangelogOutboxKey(key)
	return ok
}
