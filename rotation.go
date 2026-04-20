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
	src, err := Open(path, currentOpts)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	tmpPath := path + ".rotate.tmp"
	_ = os.Remove(tmpPath)
	_ = os.Remove(tmpPath + "-wal")
	dst, err := Open(tmpPath, newOpts)
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
	type row struct{ k, v []byte }
	var from []byte
	var copied int64
	for {
		batch := make([]row, 0, batchSize)
		if err := src.View(func(tx *Tx) error {
			return tx.AscendRange(from, nil, func(k, v []byte) bool {
				if from != nil && bytes.Equal(k, from) {
					return true
				}
				batch = append(batch, row{
					k: append([]byte(nil), k...),
					v: append([]byte(nil), v...),
				})
				return len(batch) < batchSize
			})
		}); err != nil {
			return err
		}
		if len(batch) == 0 {
			break
		}
		if err := dst.Update(func(tx *Tx) error {
			for i := range batch {
				if err := tx.Put(batch[i].k, batch[i].v); err != nil {
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
	if err := dst.Close(); err != nil {
		return err
	}
	dst = nil
	if err := src.Close(); err != nil {
		return err
	}
	src = nil

	backupPath := path + ".rotate.bak"
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("hexxladb: rotate backup: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		return fmt.Errorf("hexxladb: rotate swap: %w", err)
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(tmpPath + "-wal")
	_ = os.Remove(backupPath + "-wal")
	_ = os.Remove(backupPath)
	return nil
}
