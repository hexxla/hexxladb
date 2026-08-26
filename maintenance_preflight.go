package hexxladb

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/fsutil"
)

// MaintenanceSpaceRequirement is a conservative capacity check for one
// filesystem used by a copy-style maintenance operation.
type MaintenanceSpaceRequirement struct {
	Directory      string
	Purpose        string
	RequiredBytes  uint64
	AvailableBytes uint64
	AvailableKnown bool
}

// CompactPreflight reports the source state and destination capacity checked by
// [PreflightCompactTo].
type CompactPreflight struct {
	SourceStorage StorageStats
	Space         []MaintenanceSpaceRequirement
}

// MigrationPreflight reports the source state, resume state, and capacity
// checked by [PreflightMigrateV1ToV2] or [PreflightMigrateToAuthenticated].
type MigrationPreflight struct {
	SourceStorage      StorageStats
	SourcePrimaryBytes uint64
	SourceWALBytes     uint64
	Resumable          bool
	ProcessedKeys      uint64
	Space              []MaintenanceSpaceRequirement
}

type maintenanceSpacePart struct {
	directory string
	purpose   string
	required  uint64
}

// PreflightCompactTo opens and inspects a closed source database without
// creating a destination. It verifies distinct paths and absent destination
// components, reports source storage, and checks conservative destination
// capacity. Opening may perform normal WAL recovery but makes no logical write.
func PreflightCompactTo(ctx context.Context, srcPath, destPath string, opts *Options) (CompactPreflight, error) {
	if err := ctx.Err(); err != nil {
		return CompactPreflight{}, err
	}
	destDirectory, err := validateMaintenancePaths(srcPath, destPath, false)
	if err != nil {
		return CompactPreflight{}, fmt.Errorf("compact preflight: %w", err)
	}

	srcOpts := maintenanceSourceOptions(opts)
	src, err := Open(srcPath, srcOpts)
	if err != nil {
		return CompactPreflight{}, fmt.Errorf("compact preflight: open source: %w", err)
	}
	storage, statsErr := src.StorageStats()
	closeErr := src.Close()
	if statsErr != nil {
		return CompactPreflight{}, fmt.Errorf("compact preflight: source storage: %w", statsErr)
	}
	if closeErr != nil {
		return CompactPreflight{}, fmt.Errorf("compact preflight: close source: %w", closeErr)
	}

	workingBytes := max(saturatingMultiply(storage.PageSize, 2), storage.WALBytes)
	required := saturatingAdd(storage.PrimaryBytes, workingBytes)
	space, spaceErr := inspectMaintenanceSpace([]maintenanceSpacePart{{
		directory: destDirectory,
		purpose:   "compaction destination",
		required:  required,
	}})
	plan := CompactPreflight{SourceStorage: storage, Space: space}
	if spaceErr != nil {
		return plan, fmt.Errorf("compact preflight: %w", spaceErr)
	}
	return plan, nil
}

// PreflightMigrateV1ToV2 performs the same source-format, credential,
// changelog-state, resume-identity, path, and conservative capacity checks as
// [MigrateV1ToV2] without creating a destination or copying a new migration
// batch. Inspecting an existing partial destination may perform normal WAL
// recovery. The exact source check creates and removes a temporary locked
// snapshot, so its cost is proportional to the source recovery set.
func PreflightMigrateV1ToV2(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) (MigrationPreflight, error) {
	return preflightMigrateV1ToTarget(ctx, srcPath, destPath, opts)
}

// PreflightMigrateToAuthenticated performs the same source-format, credential,
// changelog-state, path, and capacity checks as [MigrateToAuthenticated]
// without creating or advancing the destination.
func PreflightMigrateToAuthenticated(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) (MigrationPreflight, error) {
	if err := validateAuthenticatedMigrationOptions(opts); err != nil {
		return MigrationPreflight{}, err
	}
	hdr, err := engine.ReadHeaderFile(srcPath)
	if err != nil {
		return MigrationPreflight{}, fmt.Errorf("authenticated migration preflight: read source header: %w", err)
	}
	cloned := *opts
	cloned.targetAuthenticated = true
	switch hdr.FormatVersion {
	case 1:
		return preflightMigrateV1ToTarget(ctx, srcPath, destPath, &cloned)
	case 2:
		_, _, plan, cleanup, err := prepareV2AuthenticatedMigration(ctx, srcPath, destPath, &cloned)
		if cleanup != nil {
			cleanup()
		}
		return plan, err
	default:
		return MigrationPreflight{}, fmt.Errorf(
			"%w: authenticated migration source format is %d, want 1 or 2",
			ErrInvalidArgument,
			hdr.FormatVersion,
		)
	}
}

func preflightMigrateV1ToTarget(ctx context.Context, srcPath, destPath string, opts *MigrationOptions) (MigrationPreflight, error) {
	if err := validateMigrationInputs(ctx, srcPath, destPath, opts); err != nil {
		return MigrationPreflight{}, err
	}
	snapshotDirectory, err := migrationSnapshotDirectory(destPath, opts)
	if err != nil {
		return MigrationPreflight{}, err
	}
	plan, err := migrationCapacityPreflight(srcPath, destPath, snapshotDirectory)
	if err != nil {
		return plan, err
	}

	src, header, digest, cleanup, err := openMigrationSource(ctx, srcPath, opts, snapshotDirectory)
	if err != nil {
		return plan, err
	}
	defer cleanup()
	storage, statsErr := src.StorageStats()
	closeErr := src.Close()
	if statsErr != nil {
		return plan, fmt.Errorf("migrate v1 to v2 preflight: source storage: %w", statsErr)
	}
	if closeErr != nil {
		return plan, fmt.Errorf("migrate v1 to v2 preflight: close source: %w", closeErr)
	}
	plan.SourceStorage = storage

	resumable, processed, err := inspectMigrationDestination(destPath, header, digest, opts)
	if err != nil {
		return plan, err
	}
	plan.Resumable = resumable
	plan.ProcessedKeys = processed
	return plan, nil
}

func migrationCapacityPreflight(srcPath, destPath, snapshotDirectory string) (MigrationPreflight, error) {
	destDirectory, err := validateMaintenancePaths(srcPath, destPath, true)
	if err != nil {
		return MigrationPreflight{}, fmt.Errorf("migrate v1 to v2 preflight: %w", err)
	}
	primaryBytes, err := maintenanceFileBytes(srcPath)
	if err != nil {
		return MigrationPreflight{}, fmt.Errorf("migrate v1 to v2 preflight: source: %w", err)
	}
	walBytes, err := optionalMaintenanceFileBytes(engine.WalPath(srcPath))
	if err != nil {
		return MigrationPreflight{}, fmt.Errorf("migrate v1 to v2 preflight: source WAL: %w", err)
	}
	snapshotBytes := saturatingAdd(primaryBytes, walBytes)
	destinationBytes := saturatingAdd(saturatingMultiply(primaryBytes, 2), walBytes)
	space, spaceErr := inspectMaintenanceSpace([]maintenanceSpacePart{
		{directory: snapshotDirectory, purpose: "migration source snapshot", required: snapshotBytes},
		{directory: destDirectory, purpose: "migration destination", required: destinationBytes},
	})
	plan := MigrationPreflight{
		SourcePrimaryBytes: primaryBytes,
		SourceWALBytes:     walBytes,
		Space:              space,
	}
	if spaceErr != nil {
		return plan, fmt.Errorf("migrate v1 to v2 preflight: %w", spaceErr)
	}
	return plan, nil
}

func inspectMigrationDestination(
	path string,
	sourceHeader engine.Header,
	digest [sha256.Size]byte,
	opts *MigrationOptions,
) (bool, uint64, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, 0, nil
	} else if err != nil {
		return false, 0, fmt.Errorf("migrate v1 to v2 preflight: destination: %w", err)
	}

	dest, err := openDBWithMigration(path, migrationDestinationOptions(sourceHeader, opts), false, true)
	if err != nil {
		return false, 0, fmt.Errorf("migrate v1 to v2 preflight: open destination: %w", err)
	}
	state, found, stateErr := loadMigrationState(dest.btree)
	closeErr := dest.Close()
	if stateErr != nil {
		return false, 0, fmt.Errorf("migrate v1 to v2 preflight: read destination resume state: %w", stateErr)
	}
	if closeErr != nil {
		return false, 0, fmt.Errorf("migrate v1 to v2 preflight: close destination: %w", closeErr)
	}
	if !found || state.sourceDigest != digest {
		return false, 0, fmt.Errorf("%w: destination is not a matching resumable migration", ErrInvalidArgument)
	}
	return true, state.processedKeys, nil
}

func migrationSnapshotDirectory(destPath string, opts *MigrationOptions) (string, error) {
	directory := ""
	if opts != nil {
		directory = opts.SnapshotDirectory
	}
	if directory == "" {
		directory = filepath.Dir(destPath)
	}
	resolved, err := existingMaintenanceDirectory(directory)
	if err != nil {
		return "", fmt.Errorf("migrate v1 to v2 preflight: snapshot directory: %w", err)
	}
	return resolved, nil
}

func validateMaintenancePaths(srcPath, destPath string, allowExistingDestination bool) (string, error) {
	if srcPath == "" || destPath == "" {
		return "", fmt.Errorf("%w: source and destination paths must be non-empty", ErrInvalidArgument)
	}
	sourceAbsolute, err := filepath.Abs(srcPath)
	if err != nil {
		return "", fmt.Errorf("source path: %w", err)
	}
	destinationAbsolute, err := filepath.Abs(destPath)
	if err != nil {
		return "", fmt.Errorf("destination path: %w", err)
	}
	if filepath.Clean(sourceAbsolute) == filepath.Clean(destinationAbsolute) {
		return "", fmt.Errorf("%w: source and destination paths must be distinct", ErrInvalidArgument)
	}
	sourceInfo, err := os.Stat(sourceAbsolute)
	if err != nil {
		return "", fmt.Errorf("source: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return "", fmt.Errorf("%w: source must be a regular file", ErrInvalidArgument)
	}

	destinationInfo, destinationErr := os.Stat(destinationAbsolute)
	switch {
	case destinationErr == nil:
		if os.SameFile(sourceInfo, destinationInfo) {
			return "", fmt.Errorf("%w: source and destination identify the same file", ErrInvalidArgument)
		}
		if !allowExistingDestination {
			return "", fmt.Errorf("destination %q: %w", destPath, os.ErrExist)
		}
	case !errors.Is(destinationErr, os.ErrNotExist):
		return "", fmt.Errorf("destination: %w", destinationErr)
	}
	if destinationErr != nil && allowExistingDestination {
		if _, err := os.Stat(engine.WalPath(destinationAbsolute)); err == nil {
			return "", fmt.Errorf("destination WAL %q exists without a primary: %w", engine.WalPath(destPath), os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("destination WAL: %w", err)
		}
	}
	if !allowExistingDestination {
		for _, component := range []string{engine.WalPath(destinationAbsolute), destinationAbsolute + "-changelog"} {
			if _, err := os.Stat(component); err == nil {
				return "", fmt.Errorf("destination component %q: %w", component, os.ErrExist)
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("destination component %q: %w", component, err)
			}
		}
	}
	if _, err := os.Stat(destinationAbsolute + "-changelog"); err == nil {
		return "", fmt.Errorf("destination changelog %q: %w", destPath+"-changelog", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("destination changelog: %w", err)
	}
	return existingMaintenanceDirectory(filepath.Dir(destinationAbsolute))
}

func existingMaintenanceDirectory(directory string) (string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %q is not a directory", ErrInvalidArgument, resolved)
	}
	return filepath.Clean(resolved), nil
}

func maintenanceSourceOptions(opts *Options) *Options {
	if opts == nil {
		return &Options{PageCacheSize: -1}
	}
	cloned := *opts
	cloned.PageCacheSize = -1
	cloned.ChangelogEnabled = false
	cloned.ChangelogPath = ""
	cloned.ChangelogLazy = false
	return &cloned
}

func maintenanceFileBytes(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return 0, fmt.Errorf("%w: %q is not a regular file with a valid size", ErrInvalidArgument, path)
	}
	return uint64(info.Size()), nil //nolint:gosec // size is checked non-negative above.
}

func optionalMaintenanceFileBytes(path string) (uint64, error) {
	bytes, err := maintenanceFileBytes(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return bytes, err
}

func inspectMaintenanceSpace(parts []maintenanceSpacePart) ([]MaintenanceSpaceRequirement, error) {
	type accumulated struct {
		requirement MaintenanceSpaceRequirement
	}
	groups := make([]accumulated, 0, len(parts))
	byFilesystem := make(map[string]int, len(parts))
	for _, part := range parts {
		directory, err := existingMaintenanceDirectory(part.directory)
		if err != nil {
			return nil, err
		}
		filesystemID, known, err := fsutil.FilesystemID(directory)
		if err != nil {
			return nil, err
		}
		key := "path:" + directory
		if known {
			key = "filesystem:" + filesystemID
		}
		if index, ok := byFilesystem[key]; ok {
			groups[index].requirement.RequiredBytes = saturatingAdd(groups[index].requirement.RequiredBytes, part.required)
			groups[index].requirement.Purpose += " and " + part.purpose
			continue
		}
		byFilesystem[key] = len(groups)
		groups = append(groups, accumulated{requirement: MaintenanceSpaceRequirement{
			Directory:     directory,
			Purpose:       part.purpose,
			RequiredBytes: part.required,
		}})
	}

	requirements := make([]MaintenanceSpaceRequirement, 0, len(groups))
	var insufficient []string
	for _, group := range groups {
		available, known, err := fsutil.AvailableBytes(group.requirement.Directory)
		if err != nil {
			return nil, err
		}
		group.requirement.AvailableBytes = available
		group.requirement.AvailableKnown = known
		requirements = append(requirements, group.requirement)
		if known && available < group.requirement.RequiredBytes {
			insufficient = append(insufficient, fmt.Sprintf(
				"%s requires %d bytes in %q; %d bytes available",
				group.requirement.Purpose,
				group.requirement.RequiredBytes,
				group.requirement.Directory,
				available,
			))
		}
	}
	if len(insufficient) > 0 {
		return requirements, fmt.Errorf("%w: %s", ErrInsufficientSpace, strings.Join(insufficient, "; "))
	}
	return requirements, nil
}

func saturatingAdd(left, right uint64) uint64 {
	if right > math.MaxUint64-left {
		return math.MaxUint64
	}
	return left + right
}

func saturatingMultiply(value, multiplier uint64) uint64 {
	if multiplier != 0 && value > math.MaxUint64/multiplier {
		return math.MaxUint64
	}
	return value * multiplier
}
