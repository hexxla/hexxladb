//go:build !linux

package engine

import "os"

// primaryDataSyncFile falls back to a full [os.File.Sync] on platforms without fdatasync(2)
// in this build (e.g. darwin, windows).
func primaryDataSyncFile(f *os.File) error {
	return f.Sync()
}
