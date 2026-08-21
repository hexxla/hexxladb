//go:build windows

package engine

import (
	"errors"
	"fmt"
	"math"
	"os"

	"golang.org/x/sys/windows"
)

func lockDatabaseFile(f *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		math.MaxUint32,
		math.MaxUint32,
		&overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrDatabaseLocked
	}
	if err != nil {
		return fmt.Errorf("engine: lock database file: %w", err)
	}
	return nil
}
