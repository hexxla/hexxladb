package hexxladb

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/fsutil"
)

const backupCopyBufferSize = 1024 * 1024

type backupFile struct {
	destPath    string
	dest        *os.File
	createdInfo os.FileInfo
}

// BackupTo creates a consistent physical backup of an open database at destPath.
// The destination primary, WAL, and, when enabled, changelog are created with
// exclusive semantics and are never overwritten. An enabled source changelog is
// always copied to destPath + "-changelog", even when the source uses a custom path.
//
// BackupTo holds a database read lock for the full capture. Existing reads may
// continue, while writes and Close wait until the backup files have been copied
// and synced. Context cancellation is checked between bounded copy chunks. On a
// returned error, files created by this call are removed and the source is unchanged.
//
// Encryption is preserved byte-for-byte. BackupTo does not require or retain the
// source credentials; restoring an encrypted backup requires the same encryption
// key or passphrase. Restore changelog-enabled backups with ChangelogEnabled set.
func (db *DB) BackupTo(ctx context.Context, destPath string) error {
	return db.backupTo(ctx, destPath, nil)
}

func (db *DB) backupTo(ctx context.Context, destPath string, captureStarted func()) error {
	if db == nil || db.closed.Load() {
		return ErrDatabaseClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if db.recoveryRequired.Load() {
		return fmt.Errorf("backup: %w: close and reopen required", ErrCommitFinalization)
	}

	files := []backupFile{
		{destPath: destPath},
		{destPath: engine.WalPath(destPath)},
	}
	if db.changelog != nil {
		files = append(files, backupFile{destPath: destPath + "-changelog"})
	} else if err := requireAbsentBackupPath(destPath + "-changelog"); err != nil {
		return err
	}

	return db.copyBackupFiles(ctx, files, captureStarted)
}

func requireAbsentBackupPath(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("backup: destination %q: %w", path, os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("backup: inspect destination %q: %w", path, err)
	}
	return nil
}

func (db *DB) copyBackupFiles(ctx context.Context, files []backupFile, captureStarted func()) (retErr error) {
	return db.copyBackupFilesWithSync(ctx, files, captureStarted, fsutil.SyncParents)
}

func (db *DB) copyBackupFilesWithSync(
	ctx context.Context,
	files []backupFile,
	captureStarted func(),
	syncParents func(...string) error,
) (retErr error) {
	defer finishBackupFiles(files, syncParents, &retErr)

	for i := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		// #nosec G304 -- destination paths are explicitly selected by the caller.
		dest, err := os.OpenFile(files[i].destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("backup: create destination %q: %w", files[i].destPath, err)
		}
		files[i].dest = dest
		info, err := dest.Stat()
		if err != nil {
			return fmt.Errorf("backup: inspect created destination %q: %w", files[i].destPath, err)
		}
		files[i].createdInfo = info
	}

	if captureStarted != nil {
		captureStarted()
	}
	buffer := make([]byte, backupCopyBufferSize)
	if err := db.eng.CopySnapshotTo(ctx, files[0].dest, files[1].dest, buffer); err != nil {
		return fmt.Errorf("backup: copy primary and WAL: %w", err)
	}
	if db.changelog != nil {
		if err := db.changelog.CopySnapshotTo(ctx, files[2].dest, buffer); err != nil {
			return fmt.Errorf("backup: copy changelog: %w", err)
		}
	}
	for i := range files {
		if err := files[i].dest.Sync(); err != nil {
			return fmt.Errorf("backup: sync destination %q: %w", files[i].destPath, err)
		}
		if err := files[i].dest.Close(); err != nil {
			return fmt.Errorf("backup: close destination %q: %w", files[i].destPath, err)
		}
		files[i].dest = nil
	}
	for i := range files {
		info, err := os.Lstat(files[i].destPath)
		if err != nil {
			return fmt.Errorf("backup: verify destination %q: %w", files[i].destPath, err)
		}
		if !os.SameFile(files[i].createdInfo, info) {
			return fmt.Errorf("backup: destination %q was replaced during capture", files[i].destPath)
		}
	}
	paths := make([]string, len(files))
	for i := range files {
		paths[i] = files[i].destPath
	}
	if err := syncParents(paths...); err != nil {
		return fmt.Errorf("backup: make destination files durable: %w", err)
	}
	return nil
}

func finishBackupFiles(files []backupFile, syncParents func(...string) error, retErr *error) {
	for i := range files {
		if files[i].dest == nil {
			continue
		}
		if err := files[i].dest.Close(); *retErr == nil && err != nil {
			*retErr = fmt.Errorf("backup: close destination %q: %w", files[i].destPath, err)
		}
	}
	if *retErr == nil {
		return
	}
	removedPaths := make([]string, 0, len(files))
	for i := range files {
		if files[i].createdInfo == nil {
			continue
		}
		info, err := os.Lstat(files[i].destPath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			*retErr = errors.Join(*retErr, fmt.Errorf("backup: inspect partial destination %q: %w", files[i].destPath, err))
		case !os.SameFile(files[i].createdInfo, info):
			*retErr = errors.Join(*retErr, fmt.Errorf("backup: partial destination %q was replaced and was not removed", files[i].destPath))
		default:
			if err := os.Remove(files[i].destPath); err != nil {
				*retErr = errors.Join(*retErr, fmt.Errorf("backup: remove partial destination %q: %w", files[i].destPath, err))
			} else {
				removedPaths = append(removedPaths, files[i].destPath)
			}
		}
	}
	if len(removedPaths) > 0 {
		if err := syncParents(removedPaths...); err != nil {
			*retErr = errors.Join(*retErr, fmt.Errorf("backup: make partial destination cleanup durable: %w", err))
		}
	}
}
