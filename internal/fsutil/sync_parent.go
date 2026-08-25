package fsutil

import (
	"fmt"
	"path/filepath"
)

// SyncParents makes directory-entry changes for paths durable on supported
// production platforms. Duplicate parent directories are synced once.
func SyncParents(paths ...string) error {
	parents := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		parent := filepath.Dir(path)
		if _, seen := parents[parent]; seen {
			continue
		}
		parents[parent] = struct{}{}
		if err := syncDirectory(parent); err != nil {
			return fmt.Errorf("sync parent directory %q: %w", parent, err)
		}
	}
	return nil
}
