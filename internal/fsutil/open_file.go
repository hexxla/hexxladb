package fsutil

import (
	"errors"
	"os"
)

// OpenReadWrite opens path for reading and writing, creating it with perm when
// absent. The returned boolean reports whether this call created the file.
func OpenReadWrite(path string, perm os.FileMode) (*os.File, bool, error) {
	// #nosec G304 -- The caller supplies an application-owned database path; opening it is this boundary's purpose.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err == nil {
		return file, true, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, false, err
	}
	// #nosec G304 -- The caller supplies an application-owned database path; opening it is this boundary's purpose.
	file, err = os.OpenFile(path, os.O_RDWR, perm)
	return file, false, err
}
