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
	"github.com/hexxla/hexxladb/internal/record"
)

const (
	migrationBatchSize       = 4096
	migrationStateVersion    = byte(1)
	migrationStateValueBytes = 1 + sha256.Size + 8
)

var migrationStatePrefix = []byte("__meta/migration/v1-to-v2/")

// MigrationOptions configures offline source-preserving format migration.
type MigrationOptions struct {
	// targetAuthenticated is set only by MigrateToAuthenticated.
	targetAuthenticated bool
	// SourceOptions supplies credentials and page transforms needed to open the source.
	// ChangelogEnabled must be false; the source sidecar is never modified or copied.
	SourceOptions *Options
	// DestinationOptions supplies credentials or page transforms for the destination.
	// MigrateToAuthenticated requires official raw-key or passphrase encryption;
	// MigrateV1ToV2 may also create a plaintext or legacy encrypted v2 file.
	// Page size, maximum value size, and embedding configuration are inherited from the source.
	// Cell validators and post-write hooks are ignored to keep retryable migration free of
	// application side effects.
	// ChangelogEnabled must be false during migration and may be enabled after verification.
	DestinationOptions *Options
	// BatchSize is the maximum number of source rows processed per durable destination
	// transaction. Zero uses 4096; values must be between 1 and 4096.
	BatchSize int
	// SnapshotDirectory selects the existing directory for the locked temporary
	// source snapshot. Empty uses the destination's parent directory, keeping the
	// temporary copy off a potentially small system temporary filesystem.
	SnapshotDirectory string
	// ResetChangelog explicitly starts a new logical changefeed history at the destination.
	// It is required when the source contains changelog head, outbox, checkpoint, or consumer state.
	ResetChangelog bool
	// OnPreflight is called after source, destination-resume, changelog, and
	// capacity checks succeed but before the destination is created or advanced.
	OnPreflight func(MigrationPreflight)
	// OnProgress is called after each destination batch and reports an atomic resume checkpoint.
	OnProgress func(MigrationProgress)
}

// MigrationProgress is durable, cumulative progress for [MigrateV1ToV2] or
// [MigrateToAuthenticated].
type MigrationProgress struct {
	ProcessedKeys uint64
	Resumed       bool
}

type migrationState struct {
	sourceDigest  [sha256.Size]byte
	processedKeys uint64
	lastKey       []byte
}

type migrationPair struct {
	key   []byte
	value []byte
}

// MigrateV1ToV2 performs an offline, resumable logical copy from a format-v1 source
// into a format-v2 MVCC destination. The source must be closed by every other handle;
// it is never replaced or deleted. The destination is exclusively created on the first
// call. A later call resumes only when its embedded source digest matches.
//
// Mutable cell, facet, and seam records are rewritten through their public primitives so
// v2 physical keys and secondary indexes are rebuilt. Other application and immutable
// index rows are copied byte-for-byte. Completion includes a full logical verification;
// ordinary [Open] refuses a destination while resumable state remains.
func MigrateV1ToV2(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) (retErr error) {
	return migrateV1ToTarget(ctx, srcPath, destPath, opts)
}

// MigrateToAuthenticated performs an offline, source-preserving migration from
// format v1 or v2 into an exclusively created authenticated format-v3
// destination. DestinationOptions must provide a raw key or passphrase.
func MigrateToAuthenticated(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) error {
	if err := validateAuthenticatedMigrationOptions(opts); err != nil {
		return err
	}
	hdr, err := engine.ReadHeaderFile(srcPath)
	if err != nil {
		return fmt.Errorf("migration: read source header: %w", err)
	}
	cloned := *opts
	cloned.targetAuthenticated = true
	switch hdr.FormatVersion {
	case 1:
		return migrateV1ToTarget(ctx, srcPath, destPath, &cloned)
	case 2:
		return migrateV2ToAuthenticated(ctx, srcPath, destPath, &cloned)
	default:
		return fmt.Errorf("%w: authenticated migration source format is %d, want 1 or 2", ErrInvalidArgument, hdr.FormatVersion)
	}
}

func validateAuthenticatedMigrationOptions(opts *MigrationOptions) error {
	if opts == nil || opts.DestinationOptions == nil {
		return fmt.Errorf("%w: authenticated migration requires destination encryption credentials", ErrEncryptionOptions)
	}
	destination := opts.DestinationOptions
	hasRawKey := len(destination.EncryptionKey) > 0
	hasPassphrase := destination.Passphrase != ""
	if hasRawKey == hasPassphrase || destination.BeforeWritePage != nil || destination.AfterReadPage != nil {
		return ErrEncryptionOptions
	}
	return nil
}

func migrateV2ToAuthenticated(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) error {
	srcTx, srcHeader, preflight, cleanup, err := prepareV2AuthenticatedMigration(ctx, srcPath, destPath, opts)
	if err != nil {
		return err
	}
	defer cleanup()
	if opts.OnPreflight != nil {
		opts.OnPreflight(preflight)
	}

	destOpts := migrationDestinationOptions(srcHeader, opts)
	destOpts.newIncompleteCompaction = true
	batchSize, onProgress := migrationOptions(opts)
	var compactProgress func(CompactProgress)
	if onProgress != nil {
		compactProgress = func(progress CompactProgress) {
			onProgress(MigrationProgress{ProcessedKeys: progress.CopiedKeys})
		}
	}
	var skipKey func([]byte) bool
	if opts.ResetChangelog {
		skipKey = isMigrationChangelogKey
	}
	return compactFromTx(ctx, srcTx, destPath, destOpts, srcHeader, batchSize, compactProgress, true, skipKey)
}

func prepareV2AuthenticatedMigration(
	ctx context.Context,
	srcPath, destPath string,
	opts *MigrationOptions,
) (*Tx, engine.Header, MigrationPreflight, func(), error) {
	if err := validateMigrationInputs(ctx, srcPath, destPath, opts); err != nil {
		return nil, engine.Header{}, MigrationPreflight{}, nil, err
	}
	destinationDirectory, err := validateMaintenancePaths(srcPath, destPath, false)
	if err != nil {
		return nil, engine.Header{}, MigrationPreflight{}, nil, fmt.Errorf("authenticated migration preflight: %w", err)
	}
	src, err := Open(srcPath, maintenanceSourceOptions(opts.SourceOptions))
	if err != nil {
		return nil, engine.Header{}, MigrationPreflight{}, nil, fmt.Errorf("authenticated migration: open source: %w", err)
	}
	src.mu.RLock()
	cleanup := func() {
		src.mu.RUnlock()
		_ = src.Close()
	}
	fail := func(err error) (*Tx, engine.Header, MigrationPreflight, func(), error) {
		cleanup()
		return nil, engine.Header{}, MigrationPreflight{}, nil, err
	}
	srcHeader, err := src.eng.ReadHeader()
	if err != nil {
		return fail(fmt.Errorf("authenticated migration: read source header: %w", err))
	}
	if srcHeader.FormatVersion != 2 {
		return fail(fmt.Errorf(
			"%w: authenticated migration source format is %d, want 2",
			ErrInvalidArgument,
			srcHeader.FormatVersion,
		))
	}
	ch := src.cachedHdr.Load()
	srcTx := &Tx{db: src, readSeq: ch.commitSeq, cachedBTreeRoot: ch.btreeRoot}
	if _, err := migrationSourceDigestFromTx(ctx, srcTx, opts.ResetChangelog); err != nil {
		return fail(err)
	}
	storage, err := src.btree.StorageStats()
	if err != nil {
		return fail(fmt.Errorf("authenticated migration: source storage: %w", err))
	}
	dataPages := storage.AllocatedPages - 1
	authenticatedPrimary := saturatingAdd(
		storage.PageSize,
		saturatingMultiply(dataPages, storage.PageSize+engine.AuthenticatedPageOverhead),
	)
	required := saturatingAdd(saturatingMultiply(authenticatedPrimary, 2), storage.WALBytes)
	space, spaceErr := inspectMaintenanceSpace([]maintenanceSpacePart{{
		directory: destinationDirectory,
		purpose:   "authenticated migration destination",
		required:  required,
	}})
	preflight := MigrationPreflight{
		SourceStorage:      storageStatsFromEngine(storage),
		SourcePrimaryBytes: storage.PrimaryBytes,
		SourceWALBytes:     storage.WALBytes,
		Space:              space,
	}
	if spaceErr != nil {
		return fail(fmt.Errorf("authenticated migration preflight: %w", spaceErr))
	}
	return srcTx, srcHeader, preflight, cleanup, nil
}

func storageStatsFromEngine(stats engine.StorageStats) StorageStats {
	return StorageStats{
		PageSize:         stats.PageSize,
		PhysicalPageSize: stats.PhysicalPageSize,
		PrimaryBytes:     stats.PrimaryBytes,
		WALBytes:         stats.WALBytes,
		AllocatedPages:   stats.AllocatedPages,
		ReachablePages:   stats.ReachablePages,
		AllocatorPages:   stats.AllocatorPages,
		ReusablePages:    stats.ReusablePages,
		LiveBytes:        stats.LiveBytes,
		ReclaimableBytes: stats.ReclaimableBytes,
	}
}

func migrateV1ToTarget(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) (retErr error) {
	if err := validateMigrationInputs(ctx, srcPath, destPath, opts); err != nil {
		return err
	}
	snapshotDirectory, err := migrationSnapshotDirectory(destPath, opts)
	if err != nil {
		return err
	}
	preflight, err := migrationCapacityPreflight(srcPath, destPath, snapshotDirectory)
	if err != nil {
		return err
	}
	batchSize, onProgress := migrationOptions(opts)
	src, srcHeader, sourceDigest, cleanupSnapshot, err := openMigrationSource(ctx, srcPath, opts, snapshotDirectory)
	if err != nil {
		return err
	}
	defer cleanupSnapshot()
	defer func() {
		if closeErr := src.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("migrate v1 to v2: close source: %w", closeErr)
		}
	}()
	storage, err := src.StorageStats()
	if err != nil {
		return fmt.Errorf("migrate v1 to v2: source storage: %w", err)
	}
	preflight.SourceStorage = storage
	preflight.Resumable, preflight.ProcessedKeys, err = inspectMigrationDestination(
		destPath,
		srcHeader,
		sourceDigest,
		opts,
	)
	if err != nil {
		return err
	}
	if opts != nil && opts.OnPreflight != nil {
		opts.OnPreflight(preflight)
	}

	dest, state, resumed, err := openMigrationDestination(destPath, srcHeader, sourceDigest, opts)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dest.Close(); retErr == nil && closeErr != nil {
			retErr = fmt.Errorf("migrate v1 to v2: close destination: %w", closeErr)
		}
	}()
	if resumed && onProgress != nil {
		onProgress(MigrationProgress{ProcessedKeys: state.processedKeys, Resumed: true})
	}
	if err := copyMigrationRows(ctx, src, dest, &state, batchSize, resumed, onProgress); err != nil {
		return err
	}
	if err := verifyMigration(ctx, src, dest, opts != nil && opts.ResetChangelog); err != nil {
		return fmt.Errorf("migrate v1 to v2: verify destination: %w", err)
	}
	if err := clearMigrationState(dest, state); err != nil {
		return fmt.Errorf("migrate v1 to v2: finalize destination: %w", err)
	}
	return nil
}

func validateMigrationInputs(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if srcPath == "" || destPath == "" || srcPath == destPath {
		return fmt.Errorf("%w: migration requires distinct non-empty source and destination paths", ErrInvalidArgument)
	}
	if opts != nil {
		if opts.BatchSize < 0 || opts.BatchSize > migrationBatchSize {
			return fmt.Errorf("%w: migration BatchSize must be zero or between 1 and %d", ErrInvalidArgument, migrationBatchSize)
		}
		if opts.SourceOptions != nil && opts.SourceOptions.ChangelogEnabled {
			return fmt.Errorf("%w: migration source changelog must be disabled", ErrInvalidArgument)
		}
		if opts.DestinationOptions != nil && opts.DestinationOptions.ChangelogEnabled {
			return fmt.Errorf("%w: migration destination changelog must be disabled", ErrInvalidArgument)
		}
	}
	return nil
}

func migrationOptions(opts *MigrationOptions) (int, func(MigrationProgress)) {
	if opts == nil {
		return migrationBatchSize, nil
	}
	batchSize := opts.BatchSize
	if batchSize == 0 {
		batchSize = migrationBatchSize
	}
	return batchSize, opts.OnProgress
}

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
