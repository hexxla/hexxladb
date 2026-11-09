package hexxladb

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
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
	// VerifyDestination runs a full health check after the copy and header update.
	// A failed check removes the candidate destination like any other compaction error.
	VerifyDestination bool
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
	return compactTo(ctx, srcPath, destPath, opts, compactOpts, false)
}

// RepairMVCCIndexesTo copy-compacts an MVCC database into a verified destination
// and reconstructs cell tag/source/time and seam source/time indexes from all
// retained primary versions. It preserves the source, commit sequences and
// tombstones. See [CompactTo] for path, locking and encryption requirements.
func RepairMVCCIndexesTo(ctx context.Context, srcPath, destPath string, opts *Options, compactOpts *CompactOptions) error {
	return compactTo(ctx, srcPath, destPath, opts, compactOpts, true)
}

func compactTo(ctx context.Context, srcPath, destPath string, opts *Options, compactOpts *CompactOptions, rebuildMVCCIndexes bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copyOpts, err := resolveCompactOptions(compactOpts)
	if err != nil {
		return err
	}
	if rebuildMVCCIndexes {
		if _, err := validateMaintenancePaths(srcPath, destPath); err != nil {
			return fmt.Errorf("repair MVCC indexes: %w", err)
		}
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
	if rebuildMVCCIndexes {
		srcOpts = maintenanceSourceOptions(opts)
		srcOpts.requireExistingPrimary = true
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
	return compactFromTx(ctx, srcTx, destPath, compactDestOpts(srcHdr, opts), srcHdr, copyOpts, rebuildMVCCIndexes)
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
	copyOpts, err := resolveCompactOptions(compactOpts)
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
	return compactFromTx(ctx, srcTx, destPath, compactDestOpts(srcHdr, nil), srcHdr, copyOpts, false)
}

func resolveCompactOptions(opts *CompactOptions) (CompactOptions, error) {
	if opts == nil {
		return CompactOptions{BatchSize: compactBatchSize}, nil
	}
	if opts.BatchSize < 0 || opts.BatchSize > compactBatchSize {
		return CompactOptions{}, fmt.Errorf("%w: compact BatchSize must be zero or between 1 and %d", ErrInvalidArgument, compactBatchSize)
	}
	resolved := *opts
	if resolved.BatchSize == 0 {
		resolved.BatchSize = compactBatchSize
	}
	return resolved, nil
}

// compactDestOpts builds Options for the destination DB from the source header.
func compactDestOpts(hdr engine.Header, srcOpts *Options) *Options {
	o := &Options{
		newIncompleteCompaction: true,
		PageSize:                hdr.PageSize,
		MaxValueBytes:           hdr.MaxValueBytes,
		EmbeddingDimension:      hdr.EmbeddingDim,
		DistanceMetric:          DistanceMetric(hdr.EmbeddingMetric),
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
	copyOpts CompactOptions,
	rebuildMVCCIndexes bool,
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
	if err := copyPhysicalRows(ctx, srcTx, dest, copyOpts, rebuildMVCCIndexes); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	if err := restorePhysicalCopyHead(dest, srcHdr.CommitSeq); err != nil {
		return fmt.Errorf("compact: update dest header: %w", err)
	}
	if copyOpts.VerifyDestination || rebuildMVCCIndexes {
		healthConfig := DefaultHealthCheckConfig()
		// Deletion intentionally leaves seams. Repair preserves those seams,
		// including any whose endpoints have since been deleted.
		healthConfig.CheckOrphans = !rebuildMVCCIndexes
		report, err := dest.HealthCheck(ctx, healthConfig)
		if err != nil {
			return fmt.Errorf("compact: verify destination: %w", err)
		}
		if healthReportHasErrors(report) {
			return fmt.Errorf(
				"compact: verify destination: %w: orphaned_seams=%d tag_index_errors=%d source_index_errors=%d",
				ErrCorruptDatabase,
				len(report.OrphanedSeams),
				report.TagIndexErrors,
				report.SourceIndexErrors,
			)
		}
	}
	if err := updateCompactIncompleteMarker(ctx, dest, false); err != nil {
		return fmt.Errorf("compact: finalize destination marker: %w", err)
	}
	complete = true
	return nil
}

func healthReportHasErrors(report HealthReport) bool {
	return len(report.OrphanedSeams) > 0 || report.TagIndexErrors > 0 || report.SourceIndexErrors > 0
}

func restorePhysicalCopyHead(dest *DB, seq uint64) error {
	dest.mu.Lock()
	defer dest.mu.Unlock()
	header, err := dest.eng.UpdateHeaderGet(func(h *engine.Header) { h.CommitSeq = seq })
	if err != nil {
		return err
	}
	dest.writeSeqNext.Store(seq)
	dest.storeCachedHeader(seq, header.BTreeRoot)
	return nil
}

func copyPhysicalRows(ctx context.Context, srcTx *Tx, dest *DB, opts CompactOptions, rebuildMVCCIndexes bool) error {
	batch := make([]compactPair, 0, opts.BatchSize)
	var copiedKeys uint64
	var copyErr error
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := writePhysicalCopyBatch(ctx, dest, batch); err != nil {
			return err
		}
		copiedKeys += uint64(len(batch)) //nolint:gosec // batch length is non-negative.
		if opts.OnProgress != nil {
			opts.OnProgress(CompactProgress{CopiedKeys: copiedKeys})
		}
		clear(batch)
		batch = batch[:0]
		return nil
	}
	emit := func(k, v []byte) error {
		batch = append(batch, compactPair{
			k: bytes.Clone(k),
			v: bytes.Clone(v),
		})
		if len(batch) == opts.BatchSize {
			return flush()
		}
		return nil
	}
	err := srcTx.AscendRange(nil, nil, func(k, v []byte) bool {
		if copyErr = ctx.Err(); copyErr != nil {
			return false
		}
		if rebuildMVCCIndexes && isRebuiltSecondaryKey(k) {
			if len(v) != 0 {
				copyErr = fmt.Errorf("%w: secondary key %x has a nonempty value", ErrCorruptDatabase, k)
				return false
			}
			return true
		}
		copyErr = emit(k, v)
		if copyErr == nil && rebuildMVCCIndexes {
			copyErr = srcTx.emitMVCCSecondaries(k, v, func(key []byte) error { return emit(key, nil) })
		}
		return copyErr == nil
	})
	if err != nil {
		return fmt.Errorf("scan source: %w", err)
	}
	if copyErr != nil {
		return fmt.Errorf("copy rows: %w", copyErr)
	}
	if err := flush(); err != nil {
		return fmt.Errorf("copy rows: %w", err)
	}
	return nil
}

func isRebuiltSecondaryKey(key []byte) bool {
	var err error
	switch {
	case bytes.HasPrefix(key, []byte(index.SourcePrefix)):
		_, _, err = index.ParseSourceKey(key)
	case bytes.HasPrefix(key, []byte(index.TimePrefix)):
		_, _, err = index.ParseTimeKey(key)
	case bytes.HasPrefix(key, []byte(index.TagPrefix)):
		_, _, err = index.ParseTagKey(key)
	case bytes.HasPrefix(key, []byte(index.SeamSourcePrefix)):
		_, _, err = index.ParseSeamSourceKey(key)
	case bytes.HasPrefix(key, []byte(index.SeamTimePrefix)):
		_, _, err = index.ParseSeamTimeKey(key)
	default:
		return false
	}
	return err == nil
}

func (tx *Tx) emitMVCCSecondaries(key, value []byte, emit func([]byte) error) error {
	switch {
	case bytes.HasPrefix(key, []byte(index.CellPrefix)):
		if len(value) == 0 {
			return nil // A tombstone has no secondary entries.
		}
		cell, seq, err := decodeCellVersionForIndexes(key, value)
		if err != nil {
			return err
		}
		return tx.visitCellSecondaryKeys(cell, seq, emit)
	case bytes.HasPrefix(key, []byte(index.SeamPrefix)):
		id, seq, err := index.ParseSeamVersionKey(key)
		if err != nil {
			return fmt.Errorf("%w: parse seam version for index repair: %w", ErrCorruptDatabase, err)
		}
		seam, err := record.DecodeSeam(value)
		if err != nil {
			return fmt.Errorf("%w: decode seam version for index repair: %w", ErrCorruptDatabase, err)
		}
		if seam.ID != id {
			return fmt.Errorf("%w: seam key and record ID differ", ErrCorruptDatabase)
		}
		return tx.visitSeamSecondaryKeys(seam, seq, emit)
	}
	return nil
}

func updateCompactIncompleteMarker(ctx context.Context, dest *DB, present bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dest.mu.Lock()
	defer dest.mu.Unlock()
	if dest.activeEng() == nil {
		return ErrDatabaseClosed
	}
	header, err := dest.eng.UpdateHeaderGet(func(header *engine.Header) {
		if present {
			header.Features |= engine.FeatureIncompleteCompaction
		} else {
			header.Features &^= engine.FeatureIncompleteCompaction
		}
	})
	if err != nil {
		return err
	}
	dest.storeCachedHeader(header.CommitSeq, header.BTreeRoot)
	return nil
}

func writePhysicalCopyBatch(ctx context.Context, dest *DB, batch []compactPair) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// A physical copy preserves versioned records verbatim. Using DB.Update here would
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
