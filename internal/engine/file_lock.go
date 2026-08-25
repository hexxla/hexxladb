package engine

import "os"

// OpenLockedDatabaseFile opens an existing primary without recovery and takes
// the engine's normal exclusive file lock. Closing the returned file releases
// the lock. Callers must not use it to bypass engine recovery or write ordering.
func OpenLockedDatabaseFile(path string) (*os.File, error) {
	// #nosec G304 -- path is the caller-chosen database path, matching Open.
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockDatabaseFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}
