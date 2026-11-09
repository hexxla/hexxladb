package hexxladb

import (
	"context"
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
	destDirectory, err := validateMaintenancePaths(srcPath, destPath)
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

func validateMaintenancePaths(srcPath, destPath string) (string, error) {
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
		return "", fmt.Errorf("destination %q: %w", destPath, os.ErrExist)
	case !errors.Is(destinationErr, os.ErrNotExist):
		return "", fmt.Errorf("destination: %w", destinationErr)
	}
	for _, component := range []string{engine.WalPath(destinationAbsolute), destinationAbsolute + "-changelog"} {
		if _, err := os.Stat(component); err == nil {
			return "", fmt.Errorf("destination component %q: %w", component, os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("destination component %q: %w", component, err)
		}
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
