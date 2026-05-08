package hexxladb

import (
	"context"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
)

// compactCtxCheckInterval is how many keys between context cancellation checks during compaction.
const compactCtxCheckInterval = 1024

// compactBatchSize is the number of keys copied per write transaction in compactCopy.
// Keeping this bounded caps WAL burst per commit and prevents holding the write lock
// for the entire duration of a large compact.
const compactBatchSize = 4096

// removeDBFiles removes a database file and its associated WAL.
func removeDBFiles(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
}

// CompactTo copies all data from srcPath into a fresh database at destPath,
// producing a minimal-size file with no freelist gaps. All physical keys are
// copied verbatim — including MVCC version rows and tombstones — preserving full
// history. Callers who want to strip old versions should [DB.PruneCellVersions]
// before compacting.
//
// The source database is opened read-only for the duration of the copy; destPath
// must not already exist. On error or context cancellation the partial destPath
// file is removed.
//
// Options from the source header (format version, MVCC flag, encryption,
// MaxValueBytes) are propagated to the destination automatically. The opts
// parameter, if non-nil, supplies encryption credentials required to open an
// encrypted source; non-encryption fields in opts are ignored (source header
// values take precedence).
func CompactTo(ctx context.Context, srcPath, destPath string, opts *Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Read source header to derive destination options.
	srcHdr, err := engine.ReadHeaderFile(srcPath)
	if err != nil {
		return fmt.Errorf("compact: read source header: %w", err)
	}
	destOpts := compactDestOpts(srcHdr, opts)

	// Open source with cache disabled: sequential read-once scan gets zero cache benefit
	// but would fill the 4 MiB shard pool, evicting useful hot pages.
	srcOpts := opts
	if srcOpts == nil {
		srcOpts = &Options{PageCacheSize: -1}
	} else {
		cloned := *srcOpts
		cloned.PageCacheSize = -1
		srcOpts = &cloned
	}
	src, err := Open(srcPath, srcOpts)
	if err != nil {
		return fmt.Errorf("compact: open source: %w", err)
	}
	defer src.Close() //nolint:errcheck // best-effort close on read-only source

	// Create destination.
	dest, err := Open(destPath, destOpts)
	if err != nil {
		return fmt.Errorf("compact: create dest: %w", err)
	}

	// Copy all keys inside a View (read lock) on src and Update on dest.
	copyErr := compactCopy(ctx, src, dest)
	closeErr := dest.Close()
	if copyErr != nil {
		removeDBFiles(destPath)
		return copyErr
	}
	if closeErr != nil {
		removeDBFiles(destPath)
		return fmt.Errorf("compact: close dest: %w", closeErr)
	}

	// Propagate source CommitSeq to dest header so MVCC snapshots align.
	if srcHdr.FormatVersion >= 2 {
		if err := propagateCommitSeq(destPath, destOpts, srcHdr.CommitSeq); err != nil {
			removeDBFiles(destPath)
			return err
		}
	}

	return nil
}

// Compact creates a compacted copy of the open database at destPath. The source
// DB remains open and usable; a read lock is held for the duration of the copy.
// destPath must not already exist.
//
// Typical workflow:
//
//	db.Compact(ctx, "/tmp/compacted.db")
//	db.Close()
//	os.Rename("/tmp/compacted.db", originalPath)
//	db, _ = hexxladb.Open(originalPath, opts)
func (db *DB) Compact(ctx context.Context, destPath string) error {
	if db == nil {
		return ErrDatabaseClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	db.mu.RLock()
	eng := db.activeEng()
	db.mu.RUnlock()
	if eng == nil {
		return ErrDatabaseClosed
	}

	return CompactTo(ctx, eng.Path(), destPath, nil)
}

// compactDestOpts builds Options for the destination DB from the source header.
func compactDestOpts(hdr engine.Header, srcOpts *Options) *Options {
	o := &Options{
		EnableMVCC:    hdr.FormatVersion >= 2,
		PageSize:      hdr.PageSize,
		MaxValueBytes: hdr.MaxValueBytes,
	}
	// Forward encryption credentials from caller (source opts).
	if srcOpts != nil {
		o.EncryptionKey = srcOpts.EncryptionKey
		o.Passphrase = srcOpts.Passphrase
	}
	return o
}

// compactCopy performs the key-by-key copy from src to dest in batches of
// [compactBatchSize] keys per write transaction. Batching caps WAL burst per
// commit and prevents holding the write lock for the full duration of a large compact.
func compactCopy(ctx context.Context, src, dest *DB) error {
	// Collect all keys from a single stable snapshot of src.
	type kv struct{ k, v []byte }
	var pairs []kv
	if err := src.View(func(srcTx *Tx) error {
		return srcTx.AscendRange(nil, nil, func(k, v []byte) bool {
			pairs = append(pairs, kv{
				k: append([]byte(nil), k...),
				v: append([]byte(nil), v...),
			})
			return true
		})
	}); err != nil {
		return fmt.Errorf("compact: scan src: %w", err)
	}

	// Write in batches so each commit is bounded in WAL size.
	for len(pairs) > 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("compact: copy: %w", err)
		}
		batch := pairs
		if len(batch) > compactBatchSize {
			batch = pairs[:compactBatchSize]
		}
		pairs = pairs[len(batch):]
		if err := dest.Update(func(destTx *Tx) error {
			for i, p := range batch {
				if i%compactCtxCheckInterval == 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				if err := destTx.putDirect(p.k, p.v); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("compact: copy batch: %w", err)
		}
	}
	return nil
}

// propagateCommitSeq opens the dest briefly to set the MVCC CommitSeq header to
// match the source, so ViewAt snapshots are valid on the compacted copy.
func propagateCommitSeq(destPath string, opts *Options, commitSeq uint64) error {
	d, err := Open(destPath, opts)
	if err != nil {
		return fmt.Errorf("compact: reopen dest for header: %w", err)
	}
	defer d.Close() //nolint:errcheck // best-effort close after header update
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.eng.UpdateHeader(func(h *engine.Header) {
		h.CommitSeq = commitSeq
	}); err != nil {
		return fmt.Errorf("compact: update dest header: %w", err)
	}
	d.writeSeqNext.Store(commitSeq)
	return nil
}
