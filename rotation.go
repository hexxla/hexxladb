package hexxladb

import (
	"bytes"
	"fmt"
	"os"
)

// RotateOptions configures [RotateEncryptionWithOptions].
type RotateOptions struct {
	// BatchSize controls how many KV rows are copied per write transaction.
	BatchSize int
	// OnProgress is called after each copied row.
	OnProgress func(copied int64)
}

// RotateEncryption rewrites the database at path using newOpts encryption settings.
// It performs an offline logical copy (all key/value rows) into a temporary database,
// atomically swaps files, and removes stale WAL files.
func RotateEncryption(path string, currentOpts, newOpts *Options) error {
	return RotateEncryptionWithOptions(path, currentOpts, newOpts, nil)
}

// RotateEncryptionWithOptions performs offline rotation with streaming copy and optional progress callback.
func RotateEncryptionWithOptions(path string, currentOpts, newOpts *Options, ropts *RotateOptions) error {
	if path == "" {
		return ErrInvalidArgument
	}
	if newOpts == nil || (len(newOpts.EncryptionKey) == 0 && newOpts.Passphrase == "") {
		return ErrEncryptionOptions
	}
	copyChangelog, changelogPath, err := rotationChangelogPlan(path, currentOpts, newOpts)
	if err != nil {
		return err
	}
	src, err := Open(path, currentOpts)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	tmpPath := path + ".rotate.tmp"
	_ = os.Remove(tmpPath)
	_ = os.Remove(tmpPath + "-wal")
	dstOpts := *newOpts
	tmpChangelogPath := ""
	if copyChangelog {
		tmpChangelogPath = changelogPath + ".rotate.tmp"
		_ = os.Remove(tmpChangelogPath)
		dstOpts.ChangelogPath = tmpChangelogPath
	}
	dst, err := Open(tmpPath, &dstOpts)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	batchSize := 2048
	var onProgress func(int64)
	if ropts != nil {
		if ropts.BatchSize > 0 {
			batchSize = ropts.BatchSize
		}
		onProgress = ropts.OnProgress
	}
	if err := rotateCopyData(src, dst, batchSize, onProgress); err != nil {
		return err
	}
	if copyChangelog {
		if err := src.changelog.CopyTo(dst.changelog); err != nil {
			return err
		}
	}
	if err := dst.Close(); err != nil {
		return err
	}
	dst = nil
	if err := src.Close(); err != nil {
		return err
	}
	src = nil

	return rotateSwapFiles(path, tmpPath, changelogPath, tmpChangelogPath)
}

func rotationChangelogPlan(path string, currentOpts, newOpts *Options) (bool, string, error) {
	currentEnabled := currentOpts != nil && currentOpts.ChangelogEnabled
	newEnabled := newOpts != nil && newOpts.ChangelogEnabled
	if currentEnabled != newEnabled {
		return false, "", fmt.Errorf("%w: encryption rotation must preserve ChangelogEnabled", ErrInvalidArgument)
	}
	if !currentEnabled {
		return false, "", nil
	}
	currentPath := configuredChangelogPath(path, currentOpts)
	newPath := configuredChangelogPath(path, newOpts)
	if currentPath != newPath {
		return false, "", fmt.Errorf("%w: encryption rotation cannot change ChangelogPath", ErrInvalidArgument)
	}
	return true, currentPath, nil
}

func configuredChangelogPath(path string, opts *Options) string {
	if opts != nil && opts.ChangelogPath != "" {
		return opts.ChangelogPath
	}
	return path + "-changelog"
}

// rotateRow is a key/value pair captured during rotation.
type rotateRow struct{ k, v []byte }

// rotateCopyData performs the batched logical copy from src to dst.
func rotateCopyData(src, dst *DB, batchSize int, onProgress func(int64)) error {
	var from []byte
	var copied int64
	for {
		batch := make([]rotateRow, 0, batchSize)
		if err := src.View(func(tx *Tx) error {
			return tx.AscendRange(from, nil, func(k, v []byte) bool {
				if from != nil && bytes.Equal(k, from) {
					return true
				}
				batch = append(batch, rotateRow{
					k: append([]byte(nil), k...),
					v: append([]byte(nil), v...),
				})
				return len(batch) < batchSize
			})
		}); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		if err := dst.Update(func(tx *Tx) error {
			for i := range batch {
				if err := tx.putDirect(batch[i].k, batch[i].v); err != nil {
					return err
				}
				copied++
				if onProgress != nil {
					onProgress(copied)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		from = batch[len(batch)-1].k
	}
}

// rotateSwapFiles atomically replaces the original DB file with the rotated tmp file.
func rotateSwapFiles(path, tmpPath, changelogPath, tmpChangelogPath string) error {
	backupPath := path + ".rotate.bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("hexxladb: rotate backup: %w", err)
	}
	changelogBackupPath := ""
	if changelogPath != "" {
		changelogBackupPath = changelogPath + ".rotate.bak"
		_ = os.Remove(changelogBackupPath)
		if err := os.Rename(changelogPath, changelogBackupPath); err != nil {
			_ = os.Rename(backupPath, path)
			return fmt.Errorf("hexxladb: rotate changelog backup: %w", err)
		}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		if changelogBackupPath != "" {
			_ = os.Rename(changelogBackupPath, changelogPath)
		}
		return fmt.Errorf("hexxladb: rotate swap: %w", err)
	}
	if changelogPath != "" {
		if err := os.Rename(tmpChangelogPath, changelogPath); err != nil {
			_ = os.Remove(path)
			_ = os.Rename(backupPath, path)
			_ = os.Rename(changelogBackupPath, changelogPath)
			return fmt.Errorf("hexxladb: rotate changelog swap: %w", err)
		}
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(tmpPath + "-wal")
	_ = os.Remove(backupPath + "-wal")
	_ = os.Remove(backupPath)
	if changelogBackupPath != "" {
		_ = os.Remove(changelogBackupPath)
	}
	return nil
}
