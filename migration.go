package hexxladb

import (
	"context"
	"fmt"

	"github.com/hexxla/hexxladb/internal/engine"
)

const migrationBatchSize = 4096

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
	copyOpts := CompactOptions{BatchSize: batchSize, OnProgress: compactProgress, VerifyDestination: true}
	return compactFromTx(ctx, srcTx, destPath, destOpts, srcHeader, copyOpts, skipKey, false)
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
