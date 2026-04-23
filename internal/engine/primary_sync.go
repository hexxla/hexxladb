package engine

import "os"

// syncPrimary flushes the primary data file. When the engine was opened with
// [Options.UsePrimaryFdatasync] and the platform supports a data-only sync, that path
// is used; otherwise [os.File.Sync] (fsync).
func (e *Engine) syncPrimary() error {
	if e.usePrimaryFdatasync {
		return primaryDataSyncFile(e.db)
	}
	return e.db.Sync()
}

// syncFilePrimary is used during [Open] before the [Engine] value is fully built.
func syncFilePrimary(f *os.File, useDataSync bool) error {
	if f == nil {
		return nil
	}
	if useDataSync {
		return primaryDataSyncFile(f)
	}
	return f.Sync()
}
