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

// CompactOptions configures bounded copy-compaction.
type CompactOptions struct {
	// BatchSize is the maximum number of physical key/value rows committed to the
	// destination at once. Zero uses 4096. Values must be between 1 and 4096.
	BatchSize int
	// OnProgress is called after each destination batch is durably committed.
	// CopiedKeys is cumulative and monotonic. Cancel the operation through the
	// context; callbacks must not call write methods on the source DB.
	OnProgress func(CompactProgress)
}

// CompactProgress is a durable progress checkpoint emitted by compaction.
type CompactProgress struct {
	CopiedKeys uint64
}

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
// The source database is opened exclusively for the duration of the copy, so it
// must not already be open in this or another process. destPath must not already
// exist. On error or context cancellation the partial destPath file is removed.
//
// Options from the source header (format version, MVCC flag, encryption,
// MaxValueBytes) are propagated to the destination automatically. The opts
// parameter, if non-nil, supplies encryption credentials required to open an
// encrypted source; non-encryption fields in opts are ignored (source header
// values take precedence).
func CompactTo(ctx context.Context, srcPath, destPath string, opts *Options) error {
	return CompactToWithOptions(ctx, srcPath, destPath, opts, nil)
}

// CompactToWithOptions is [CompactTo] with bounded-batch and progress controls.
func CompactToWithOptions(ctx context.Context, srcPath, destPath string, opts *Options, compactOpts *CompactOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	batchSize, onProgress, err := resolveCompactOptions(compactOpts)
	if err != nil {
		return err
	}

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

	src.mu.RLock()
	defer src.mu.RUnlock()
	srcHdr, err := src.eng.ReadHeader()
	if err != nil {
		return fmt.Errorf("compact: read source header: %w", err)
	}
	ch := src.cachedHdr.Load()
	srcTx := &Tx{db: src, readSeq: ch.commitSeq, cachedBTreeRoot: ch.btreeRoot}
	return compactFromTx(ctx, srcTx, destPath, compactDestOpts(srcHdr, opts), srcHdr, batchSize, onProgress)
}

// Compact creates a compacted copy of the open database at destPath. The source
// DB remains open and usable; a read lock is held for the duration of the copy.
// destPath must not already exist.
//
// Measure and prune first, validate the destination, and preserve the source
// primary/WAL/changelog set until the replacement reopens successfully. See
// docs/hexxladb/OPERATIONS.md for the complete replacement workflow.
func (db *DB) Compact(ctx context.Context, destPath string) error {
	return db.CompactWithOptions(ctx, destPath, nil)
}

// CompactWithOptions is [DB.Compact] with bounded-batch and progress controls.
func (db *DB) CompactWithOptions(ctx context.Context, destPath string, compactOpts *CompactOptions) error {
	if db == nil {
		return ErrDatabaseClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batchSize, onProgress, err := resolveCompactOptions(compactOpts)
	if err != nil {
		return err
	}

	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.activeEng() == nil {
		return ErrDatabaseClosed
	}
	srcHdr, err := db.eng.ReadHeader()
	if err != nil {
		return fmt.Errorf("compact: read source header: %w", err)
	}
	if srcHdr.Features&engine.FeatureEncryptedDataPages != 0 {
		return ErrEncryptionKeyRequired
	}
	ch := db.cachedHdr.Load()
	srcTx := &Tx{db: db, readSeq: ch.commitSeq, cachedBTreeRoot: ch.btreeRoot}
	return compactFromTx(ctx, srcTx, destPath, compactDestOpts(srcHdr, nil), srcHdr, batchSize, onProgress)
}

func resolveCompactOptions(opts *CompactOptions) (batchSize int, onProgress func(CompactProgress), err error) {
	batchSize = compactBatchSize
	if opts == nil {
		return batchSize, nil, nil
	}
	if opts.BatchSize < 0 || opts.BatchSize > compactBatchSize {
		return 0, nil, fmt.Errorf("%w: compact BatchSize must be zero or between 1 and %d", ErrInvalidArgument, compactBatchSize)
	}
	if opts.BatchSize > 0 {
		batchSize = opts.BatchSize
	}
	return batchSize, opts.OnProgress, nil
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

// compactPair holds one physical key/value row during the bounded copy from src to dest.
type compactPair struct{ k, v []byte }

func compactFromTx(
	ctx context.Context,
	srcTx *Tx,
	destPath string,
	destOpts *Options,
	srcHdr engine.Header,
	batchSize int,
	onProgress func(CompactProgress),
) (retErr error) {
	dest, err := openDB(destPath, destOpts, true)
	if err != nil {
		return fmt.Errorf("compact: create dest: %w", err)
	}
	complete := false
	defer func() {
		if err := dest.Close(); retErr == nil && err != nil {
			retErr = fmt.Errorf("compact: close dest: %w", err)
		}
		if !complete || retErr != nil {
			removeDBFiles(destPath)
		}
	}()

	if err := compactCopyTx(ctx, srcTx, dest, batchSize, onProgress); err != nil {
		return err
	}
	if srcHdr.FormatVersion >= 2 {
		dest.mu.Lock()
		err = dest.eng.UpdateHeader(func(h *engine.Header) {
			h.CommitSeq = srcHdr.CommitSeq
		})
		if err == nil {
			dest.writeSeqNext.Store(srcHdr.CommitSeq)
			dest.storeCachedHeader(srcHdr.CommitSeq, dest.cachedHdr.Load().btreeRoot)
		}
		dest.mu.Unlock()
		if err != nil {
			return fmt.Errorf("compact: update dest header: %w", err)
		}
	}
	complete = true
	return nil
}

func compactCopyTx(ctx context.Context, srcTx *Tx, dest *DB, batchSize int, onProgress func(CompactProgress)) error {
	batch := make([]compactPair, 0, batchSize)
	var copiedKeys uint64
	var copyErr error
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		copyErr = writeCompactBatch(ctx, dest, batch)
		if copyErr == nil {
			copiedKeys += uint64(len(batch)) //nolint:gosec // batch length is non-negative and bounded to 4096.
			if onProgress != nil {
				onProgress(CompactProgress{CopiedKeys: copiedKeys})
			}
		}
		batch = batch[:0]
		return copyErr == nil
	}

	err := srcTx.AscendRange(nil, nil, func(k, v []byte) bool {
		if len(batch)%compactCtxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				copyErr = err
				return false
			}
		}
		batch = append(batch, compactPair{
			k: append([]byte(nil), k...),
			v: append([]byte(nil), v...),
		})
		return len(batch) < batchSize || flush()
	})
	if err != nil {
		return fmt.Errorf("compact: scan src: %w", err)
	}
	if copyErr != nil {
		return fmt.Errorf("compact: copy: %w", copyErr)
	}
	if !flush() {
		return fmt.Errorf("compact: copy: %w", copyErr)
	}
	return nil
}

func writeCompactBatch(ctx context.Context, dest *DB, batch []compactPair) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Compaction copies physical records verbatim. Using DB.Update here would
	// synthesize an MVCC commit-time row for every batch, making the destination
	// observably different from the source and potentially advancing its history.
	dest.mu.Lock()
	defer dest.mu.Unlock()
	if dest.activeEng() == nil {
		return ErrDatabaseClosed
	}
	if err := dest.eng.BeginWriteTxn(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			dest.eng.AbortWriteTxn()
		}
	}()

	for i, p := range batch {
		if i%compactCtxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := dest.btree.Put(p.k, p.v); err != nil {
			return err
		}
	}
	if err := dest.eng.CommitWriteTxn(); err != nil {
		return err
	}
	committed = true
	if hdr, err := dest.eng.ReadHeader(); err == nil {
		dest.storeCachedHeader(hdr.CommitSeq, hdr.BTreeRoot)
	} else {
		return err
	}
	return nil
}
