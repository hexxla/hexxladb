package hexxladb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/fsutil"
)

const rotationStateVersion = 1

type rotationState struct {
	Version       int    `json:"version"`
	ChangelogPath string `json:"changelog_path,omitempty"`
}

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
	if pending, err := rotationPending(path); err != nil {
		return err
	} else if pending {
		if err := recoverInterruptedRotationFromDisk(path, changelogPath); err != nil {
			return err
		}
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
func rotateSwapFiles(path, tmpPath, changelogPath, tmpChangelogPath string) (retErr error) {
	state := rotationState{
		Version:       rotationStateVersion,
		ChangelogPath: changelogPath,
	}
	if err := writeRotationState(path, state); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed || retErr == nil {
			return
		}
		if recoveryErr := recoverInterruptedRotation(path, state); recoveryErr != nil {
			retErr = errors.Join(retErr, recoveryErr)
		}
	}()

	backupPath := path + ".rotate.bak"
	if err := removeRotationFiles(backupPath); err != nil {
		return fmt.Errorf("hexxladb: rotate remove stale backup: %w", err)
	}
	if err := renameRotationFile(path, backupPath); err != nil {
		return fmt.Errorf("hexxladb: rotate backup: %w", err)
	}
	changelogBackupPath := ""
	if changelogPath != "" {
		changelogBackupPath = changelogPath + ".rotate.bak"
		if err := removeRotationFiles(changelogBackupPath); err != nil {
			return fmt.Errorf("hexxladb: rotate remove stale changelog backup: %w", err)
		}
		if err := renameRotationFile(changelogPath, changelogBackupPath); err != nil {
			return fmt.Errorf("hexxladb: rotate changelog backup: %w", err)
		}
	}
	if err := renameRotationFile(tmpPath, path); err != nil {
		return fmt.Errorf("hexxladb: rotate swap: %w", err)
	}
	if changelogPath != "" {
		if err := renameRotationFile(tmpChangelogPath, changelogPath); err != nil {
			return fmt.Errorf("hexxladb: rotate changelog swap: %w", err)
		}
	}
	// Remove every WAL name that could be paired with the new primary before
	// clearing the recovery marker. A crash after the marker is removed must
	// never reopen the new primary beside the old primary's WAL.
	if err := removeRotationFiles(
		path+"-wal",
		tmpPath+"-wal",
		backupPath+"-wal",
	); err != nil {
		return fmt.Errorf("hexxladb: rotate remove stale WAL: %w", err)
	}
	if err := removeRotationFiles(rotationStatePath(path)); err != nil {
		return fmt.Errorf("hexxladb: rotate commit state: %w", err)
	}
	committed = true
	if err := removeRotationFiles(
		backupPath,
		changelogBackupPath,
	); err != nil {
		return fmt.Errorf("%w: %w", ErrRotationCleanup, err)
	}
	return nil
}

// RecoverInterruptedRotation rolls an uncommitted filesystem swap back to the
// old primary and optional changelog. opts must carry the same changelog
// enablement/path used by the interrupted rotation; encryption credentials are
// not read or required.
func RecoverInterruptedRotation(path string, opts *Options) error {
	if path == "" {
		return ErrInvalidArgument
	}
	expectedChangelogPath := ""
	if opts != nil && opts.ChangelogEnabled {
		expectedChangelogPath = configuredChangelogPath(path, opts)
	}
	return recoverInterruptedRotationFromDisk(path, expectedChangelogPath)
}

func recoverInterruptedRotationFromDisk(path, expectedChangelogPath string) error {
	state, err := readRotationState(path)
	if err != nil {
		return err
	}
	if state.ChangelogPath != expectedChangelogPath {
		return fmt.Errorf("%w: changelog configuration does not match recovery state", ErrRotationIncomplete)
	}
	return recoverInterruptedRotation(path, state)
}

func recoverInterruptedRotation(path string, state rotationState) error {
	if err := restoreRotationFile(path, path+".rotate.bak"); err != nil {
		return fmt.Errorf("%w: restore primary: %w", ErrRotationIncomplete, err)
	}
	if state.ChangelogPath != "" {
		if err := restoreRotationFile(state.ChangelogPath, state.ChangelogPath+".rotate.bak"); err != nil {
			return fmt.Errorf("%w: restore changelog: %w", ErrRotationIncomplete, err)
		}
	}
	cleanup := []string{path + ".rotate.tmp", path + ".rotate.tmp-wal"}
	if state.ChangelogPath != "" {
		cleanup = append(cleanup, state.ChangelogPath+".rotate.tmp")
	}
	if err := removeRotationFiles(cleanup...); err != nil {
		return fmt.Errorf("%w: remove temporary files: %w", ErrRotationIncomplete, err)
	}
	if err := removeRotationFiles(rotationStatePath(path)); err != nil {
		return fmt.Errorf("%w: clear recovery state: %w", ErrRotationIncomplete, err)
	}
	return nil
}

func rotationStatePath(path string) string {
	return path + ".rotate.state"
}

func rotationPending(path string) (bool, error) {
	_, err := os.Lstat(rotationStatePath(path))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("hexxladb: inspect rotation state: %w", err)
	}
}

func writeRotationState(path string, state rotationState) (retErr error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	statePath := rotationStatePath(path)
	// #nosec G304 -- state path is derived from the caller-selected database path.
	file, err := os.OpenFile(statePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("hexxladb: create rotation state: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, file.Close())
		if retErr != nil {
			retErr = errors.Join(retErr, removeRotationFiles(statePath))
		}
	}()
	if _, err := file.Write(encoded); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := fsutil.SyncParents(statePath); err != nil {
		return err
	}
	return nil
}

func readRotationState(path string) (rotationState, error) {
	statePath := rotationStatePath(path)
	info, err := os.Lstat(statePath)
	if err != nil {
		return rotationState{}, fmt.Errorf("%w: inspect state: %w", ErrRotationIncomplete, err)
	}
	if !info.Mode().IsRegular() {
		return rotationState{}, fmt.Errorf("%w: recovery state is not a regular file", ErrRotationIncomplete)
	}
	// #nosec G304 -- state path is derived from the caller-selected database path.
	encoded, err := os.ReadFile(statePath)
	if err != nil {
		return rotationState{}, fmt.Errorf("%w: read state: %w", ErrRotationIncomplete, err)
	}
	var state rotationState
	if err := json.Unmarshal(encoded, &state); err != nil || state.Version != rotationStateVersion {
		return rotationState{}, fmt.Errorf("%w: invalid recovery state", ErrRotationIncomplete)
	}
	return state, nil
}

func restoreRotationFile(path, backupPath string) error {
	backupInfo, backupErr := os.Lstat(backupPath)
	switch {
	case backupErr == nil:
		if !backupInfo.Mode().IsRegular() {
			return fmt.Errorf("rotation backup %q is not a regular file", backupPath)
		}
		if err := removeRotationFiles(path); err != nil {
			return err
		}
		return renameRotationFile(backupPath, path)
	case !errors.Is(backupErr, os.ErrNotExist):
		return backupErr
	}
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return nil
}

func renameRotationFile(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	return fsutil.SyncParents(from, to)
}

func removeRotationFiles(paths ...string) (retErr error) {
	removed := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		err := os.Remove(path)
		switch {
		case err == nil:
			removed = append(removed, path)
		case errors.Is(err, os.ErrNotExist):
		default:
			retErr = errors.Join(retErr, err)
		}
	}
	if len(removed) == 0 {
		return retErr
	}
	return errors.Join(retErr, fsutil.SyncParents(removed...))
}
