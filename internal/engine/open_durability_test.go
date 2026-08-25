package engine

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenSyncsNewDatabaseDirectoryEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	wantErr := errors.New("sync failed")
	var syncedPaths []string

	eng, err := open(path, nil, func(paths ...string) error {
		syncedPaths = append(syncedPaths, paths...)
		return wantErr
	})
	if eng != nil {
		_ = eng.Close()
		t.Fatal("open returned an engine after directory sync failed")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("open error = %v, want %v", err, wantErr)
	}
	wantPaths := []string{path, WalPath(path)}
	if !slices.Equal(syncedPaths, wantPaths) {
		t.Fatalf("synced paths = %q, want %q", syncedPaths, wantPaths)
	}
}
