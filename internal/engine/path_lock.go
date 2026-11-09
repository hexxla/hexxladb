package engine

import (
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/fsutil"
)

// AcquirePathLock excludes opens and maintenance while a recovery set is being
// replaced. Unlike the primary's inode lock, this lock survives primary renames.
// The returned file must be closed to release ownership. Never unlink the lock
// file: a second inode at the same name would permit two simultaneous owners.
// Callers must acquire this lock before opening/locking the primary.
func AcquirePathLock(path string) (*os.File, error) {
	if path == "" {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}
	file, _, err := fsutil.OpenReadWrite(path+".lock", 0o600)
	if err != nil {
		return nil, fmt.Errorf("engine: open database path lock: %w", err)
	}
	if err := lockDatabaseFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
