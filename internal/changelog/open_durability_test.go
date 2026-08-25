package changelog

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenSyncsNewChangelogDirectoryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "changes.log")
	wantErr := errors.New("sync failed")
	var syncedPath string

	log, err := openWithSync(path, false, nil, false, func(paths ...string) error {
		if len(paths) == 1 {
			syncedPath = paths[0]
		}
		return wantErr
	})
	if log != nil {
		_ = log.Close()
		t.Fatal("openWithSync returned a log after directory sync failed")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("openWithSync error = %v, want %v", err, wantErr)
	}
	if syncedPath != path {
		t.Fatalf("synced path = %q, want %q", syncedPath, path)
	}
}
