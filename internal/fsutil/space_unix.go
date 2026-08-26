//go:build linux || darwin

package fsutil

import (
	"fmt"
	"math"
	"strconv"

	"golang.org/x/sys/unix"
)

// AvailableBytes reports bytes available to an unprivileged process on the
// filesystem containing directory.
func AvailableBytes(directory string) (uint64, bool, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(directory, &stat); err != nil {
		return 0, false, fmt.Errorf("stat filesystem %q: %w", directory, err)
	}
	blockSize := uint64(stat.Bsize) //nolint:gosec // filesystem block sizes are non-negative.
	availableBlocks := uint64(stat.Bavail)
	if blockSize != 0 && availableBlocks > math.MaxUint64/blockSize {
		return math.MaxUint64, true, nil
	}
	return availableBlocks * blockSize, true, nil
}

// FilesystemID returns a stable identifier for the mounted filesystem
// containing directory.
func FilesystemID(directory string) (string, bool, error) {
	var stat unix.Stat_t
	if err := unix.Stat(directory, &stat); err != nil {
		return "", false, fmt.Errorf("stat directory %q: %w", directory, err)
	}
	return strconv.FormatUint(uint64(stat.Dev), 10), true, nil
}
