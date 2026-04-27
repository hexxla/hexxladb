package engine

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Engine is a minimal page-oriented store with redo WAL.
type Engine struct {
	path     string
	db       *os.File
	wal      *os.File
	hooks    PageHooks
	pageSize int // effective page size in bytes (set at Open, immutable)
	lastSeq  uint64
	// nextWALSeq is the high-water for allocating redo [pendingRedo.seq] values; it must stay
	// monotonic when write transactions can overlap the group-WAL flusher. Initialized from lastSeq
	// at [Open] (first Add(1) yields lastSeq+1).
	nextWALSeq    atomic.Uint64
	walMACEnabled bool
	walMACKey     [32]byte
	// usePrimaryFdatasync selects fdatasync vs fsync on the primary; see [Engine.syncPrimary].
	usePrimaryFdatasync bool
	// maxValueBytes is the effective per-database value size ceiling (read from header at Open).
	maxValueBytes uint32
	// embeddingDim is the fixed vector dimension (0 = disabled). Immutable after creation.
	embeddingDim uint16
	// embeddingMetric is the distance function for embedding search.
	embeddingMetric DistanceMetric
	// pageBufPool is an instance-level pool of page-sized buffers.
	pageBufPool sync.Pool
	// wtxn is set between [Engine.BeginWriteTxn] and commit/abort. Not used concurrently.
	wtxn *writeTxnState
	// Group commit (Options.GroupWAL) — set at Open.
	groupWALCfg       GroupWAL
	groupJobCh        chan *groupJob
	groupStop         chan struct{}
	groupFlusherWG    sync.WaitGroup
	groupOverlay      map[uint64][]byte
	groupOverlayMu    sync.RWMutex
	groupUnflushedMu  sync.RWMutex
	groupUnflushed    Header
	groupUnflushedSet bool
	// groupWALStats* are observability counters for the group flusher (see [Engine.GroupWALStats]).
	groupWALStatsApplyBatches           atomic.Uint64
	groupWALStatsBatchesWith2OrMoreJobs atomic.Uint64
	groupWALStatsWalSynces              atomic.Uint64
}

// WalPath returns the WAL path for a primary database path.
func WalPath(primary string) string {
	return primary + "-wal"
}

// Open opens or creates a database at path and replays the WAL.
func Open(path string, opts *Options) (*Engine, error) {
	var hooks PageHooks
	if opts != nil && opts.Hooks != nil {
		hooks = *opts.Hooks
	}
	usePrimaryFdatasync := opts != nil && opts.UsePrimaryFdatasync

	// #nosec G304 -- path is the caller-chosen database file (public Open API).
	db, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}

	st, err := db.Stat()
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Bootstrap page size: for new files, use opts; for existing files, read from header.
	var effectivePageSize int
	if st.Size() == 0 {
		ps, psErr := resolvePageSize(opts)
		if psErr != nil {
			_ = db.Close()
			return nil, psErr
		}
		effectivePageSize = int(ps)

		mvb, mvbErr := resolveMaxValueBytes(opts)
		if mvbErr != nil {
			_ = db.Close()
			return nil, mvbErr
		}
		ver := formatVersionV1
		if opts != nil && opts.UseFormatV2 {
			ver = formatVersionV2
		}
		var embDim uint16
		var embMetric DistanceMetric
		if opts != nil && opts.EmbeddingDim > 0 {
			embDim = opts.EmbeddingDim
			embMetric = opts.EmbeddingMetric
			if !IsValidDistanceMetric(embMetric) {
				_ = db.Close()
				return nil, fmt.Errorf("%w: invalid distance metric %d", ErrInvalidEmbeddingConfig, embMetric)
			}
		}
		hdr := Header{
			FormatVersion:   ver,
			PageSize:        ps,
			LastWALSeq:      0,
			NextPageID:      1,
			CommitSeq:       0,
			MaxValueBytes:   mvb,
			EmbeddingDim:    embDim,
			EmbeddingMetric: embMetric,
		}
		if opts != nil && opts.NewEncryptedDB {
			hdr.Features |= FeatureEncryptedDataPages
			if opts.EnableWALMAC {
				hdr.Features |= FeatureWALKeyedMAC
			}
			if opts.EncryptionSalt == ([16]byte{}) {
				if _, err := rand.Read(hdr.EncryptionSalt[:]); err != nil {
					_ = db.Close()
					return nil, err
				}
			} else {
				hdr.EncryptionSalt = opts.EncryptionSalt
			}
			hdr.EncryptionKeyCheck = opts.EncryptionKeyCheck
		}
		if err := writeHeaderAt(db, hdr); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := syncFilePrimary(db, usePrimaryFdatasync); err != nil {
			_ = db.Close()
			return nil, err
		}
	} else {
		// Existing file: bootstrap page size from the header prefix.
		ps, psErr := bootstrapPageSize(db)
		if psErr != nil {
			_ = db.Close()
			return nil, psErr
		}
		effectivePageSize = int(ps)
		if st.Size() < int64(effectivePageSize) {
			_ = db.Close()
			return nil, ErrCorruptHeader
		}
	}

	hdr, err := readHeaderAt(db, effectivePageSize)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if opts != nil && opts.ExpectEncryptionKeyCheck && hdr.Features&FeatureEncryptedDataPages != 0 {
		if hdr.EncryptionKeyCheck != ([HeaderEncryptionKeyCheckLen]byte{}) && hdr.EncryptionKeyCheck != opts.EncryptionKeyCheck {
			_ = db.Close()
			return nil, ErrBadEncryptionKey
		}
	}

	walPath := WalPath(path)
	// #nosec G304 -- WAL path is derived from the primary DB path (same contract as primary).
	wal, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	// Replay the full WAL. A fixed 1 GiB read cap truncated large sessions and made
	// replay fail with ErrCorruptWAL (partial tail). Cap allocation at 16 GiB instead.
	const maxWALReplayBytes = 16 << 30
	walInfo, err := wal.Stat()
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if walInfo.Size() > maxWALReplayBytes {
		_ = db.Close()
		_ = wal.Close()
		return nil, fmt.Errorf("%w: WAL size %d exceeds max replay size", ErrCorruptWAL, walInfo.Size())
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	walData := make([]byte, walInfo.Size())
	if len(walData) > 0 {
		if _, err := io.ReadFull(wal, walData); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
	}

	apply := func(_ uint64, pageID uint64, payload []byte) error {
		off, err := pageByteOffset(pageID, effectivePageSize)
		if err != nil {
			return err
		}
		_, err = db.WriteAt(payload, off)
		return err
	}

	walMACEnabled := hdr.Features&FeatureWALKeyedMAC != 0
	var walMACKey [32]byte
	if walMACEnabled {
		if opts == nil || !opts.EnableWALMAC {
			_ = db.Close()
			_ = wal.Close()
			return nil, ErrBadEncryptionKey
		}
		walMACKey = opts.WALMACKey
	}
	maxSeq, err := parseAndReplayWALWithMAC(walData, hdr.LastWALSeq, apply, walMACKey, walMACEnabled, effectivePageSize)
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	newLast := max(hdr.LastWALSeq, maxSeq)
	if newLast != hdr.LastWALSeq {
		hdr.LastWALSeq = newLast
		if err := writeHeaderAt(db, hdr); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
		if err := syncFilePrimary(db, usePrimaryFdatasync); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
	}

	if err := wal.Truncate(0); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if err := wal.Sync(); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	if opts != nil && opts.MaxValueBytes != 0 && opts.MaxValueBytes != hdr.MaxValueBytes {
		// Caller explicitly set a limit that differs from the stored header — validate and persist.
		mvb, mvbErr := resolveMaxValueBytes(opts)
		if mvbErr != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, mvbErr
		}
		hdr.MaxValueBytes = mvb
		if err := writeHeaderAt(db, hdr); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, err
		}
	}
	effectiveMaxVal := hdr.MaxValueBytes
	if effectiveMaxVal == 0 {
		effectiveMaxVal = DefaultMaxValueBytes
	}

	// Embedding: validate consistency between opts and header on reopen.
	if opts != nil && opts.EmbeddingDim > 0 && hdr.EmbeddingDim > 0 && opts.EmbeddingDim != hdr.EmbeddingDim {
		_ = db.Close()
		_ = wal.Close()
		return nil, fmt.Errorf("%w: embedding dimension mismatch: options=%d, header=%d", ErrInvalidEmbeddingConfig, opts.EmbeddingDim, hdr.EmbeddingDim)
	}
	if opts != nil && opts.EmbeddingDim > 0 && hdr.EmbeddingDim > 0 && opts.EmbeddingMetric != hdr.EmbeddingMetric {
		_ = db.Close()
		_ = wal.Close()
		return nil, fmt.Errorf("%w: embedding metric mismatch: options=%d, header=%d", ErrInvalidEmbeddingConfig, opts.EmbeddingMetric, hdr.EmbeddingMetric)
	}

	e := &Engine{
		path:                path,
		db:                  db,
		wal:                 wal,
		hooks:               hooks,
		pageSize:            effectivePageSize,
		lastSeq:             newLast,
		walMACEnabled:       walMACEnabled,
		walMACKey:           walMACKey,
		usePrimaryFdatasync: usePrimaryFdatasync,
		maxValueBytes:       effectiveMaxVal,
		embeddingDim:        hdr.EmbeddingDim,
		embeddingMetric:     hdr.EmbeddingMetric,
	}
	e.pageBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, e.pageSize)
			return &b
		},
	}
	if opts != nil {
		e.groupWALCfg = opts.GroupWAL
	}
	e.nextWALSeq.Store(newLast)
	if e.groupWALCfg.Enabled {
		e.startGroupWALFlusher()
	}
	return e, nil
}

// Close releases file handles.
func (e *Engine) Close() error {
	e.wtxn = nil
	e.stopGroupWALFlusher()
	var errs []error
	if e.wal != nil {
		errs = append(errs, e.wal.Close())
		e.wal = nil
	}
	if e.db != nil {
		errs = append(errs, e.db.Close())
		e.db = nil
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// PageSizeInt returns the effective page size in bytes for this engine instance.
func (e *Engine) PageSizeInt() int { return e.pageSize }

func releaseNothing() {}

func sliceSameBase(a, b []byte) bool {
	if len(a) != len(b) || len(a) == 0 {
		return len(a) == len(b)
	}
	return unsafe.SliceData(a) == unsafe.SliceData(b)
}

// visibleHeader returns the logical file header: active write-txn state, or disk
// merged with any group-WAL state not yet applied to the primary.
func (e *Engine) visibleHeader() (Header, error) {
	if e.wtxn != nil {
		return e.wtxn.hdr, nil
	}
	if e.db == nil {
		return Header{}, fmt.Errorf("engine: closed")
	}
	disk, err := readHeaderAt(e.db, e.pageSize)
	if err != nil {
		return Header{}, err
	}
	e.groupUnflushedMu.RLock()
	gset := e.groupUnflushedSet
	g := e.groupUnflushed
	e.groupUnflushedMu.RUnlock()
	if gset && g.LastWALSeq > disk.LastWALSeq {
		return g, nil
	}
	return disk, nil
}

// readPagePooled reads a physical page into a pooled buffer when possible.
// Release must be called exactly once after the returned slice is no longer used.
// AfterRead hooks may return a freshly allocated slice; release is always safe to call.
func (e *Engine) readPagePooled(pageID uint64) (data []byte, release func(), err error) {
	if e.db == nil {
		return nil, releaseNothing, fmt.Errorf("engine: closed")
	}

	// In-memory view of a single write transaction.
	if e.wtxn != nil {
		if pageID == 0 {
			page := encodeHeaderPage(e.wtxn.hdr)
			bp := e.pageBufPool.Get().(*[]byte)
			buf := (*bp)[:e.pageSize]
			copy(buf, page)
			out, err := e.hooks.transformRead(0, buf)
			if err != nil {
				e.pageBufPool.Put(bp)
				return nil, releaseNothing, err
			}
			if sliceSameBase(out, buf) {
				return out, func() { e.pageBufPool.Put(bp) }, nil
			}
			e.pageBufPool.Put(bp)
			return out, releaseNothing, nil
		}
		if plain, ok := e.wtxn.dirty[pageID]; ok {
			bp := e.pageBufPool.Get().(*[]byte)
			buf := (*bp)[:e.pageSize]
			copy(buf, plain)
			out, err := e.hooks.transformRead(pageID, buf)
			if err != nil {
				e.pageBufPool.Put(bp)
				return nil, releaseNothing, err
			}
			if sliceSameBase(out, buf) {
				return out, func() { e.pageBufPool.Put(bp) }, nil
			}
			e.pageBufPool.Put(bp)
			return out, releaseNothing, nil
		}
	}

	// Data pages logically on primary after group commit enqueue but before flusher.
	// Do not merge overlay while a write txn is active: [readPagePooled] must see the same view
	// as the in-txn dirty map; overlay can otherwise win over disk for pages this txn has not
	// dirtied yet and desync rebalance walks (MVCC prune vs group WAL).
	if pageID >= 1 && e.wtxn == nil {
		e.groupOverlayMu.RLock()
		plain, ok := e.groupOverlay[pageID]
		e.groupOverlayMu.RUnlock()
		if ok {
			bp := e.pageBufPool.Get().(*[]byte)
			buf := (*bp)[:e.pageSize]
			copy(buf, plain)
			out, err := e.hooks.transformRead(pageID, buf)
			if err != nil {
				e.pageBufPool.Put(bp)
				return nil, releaseNothing, err
			}
			if sliceSameBase(out, buf) {
				return out, func() { e.pageBufPool.Put(bp) }, nil
			}
			e.pageBufPool.Put(bp)
			return out, releaseNothing, nil
		}
	}

	// Page 0: logical header (includes group-WAL staging when ahead of primary).
	if pageID == 0 {
		vh, err := e.visibleHeader()
		if err != nil {
			return nil, releaseNothing, err
		}
		page := encodeHeaderPage(vh)
		bp := e.pageBufPool.Get().(*[]byte)
		buf := (*bp)[:e.pageSize]
		copy(buf, page)
		out, err := e.hooks.transformRead(0, buf)
		if err != nil {
			e.pageBufPool.Put(bp)
			return nil, releaseNothing, err
		}
		if sliceSameBase(out, buf) {
			return out, func() { e.pageBufPool.Put(bp) }, nil
		}
		e.pageBufPool.Put(bp)
		return out, releaseNothing, nil
	}

	bp := e.pageBufPool.Get().(*[]byte)
	buf := (*bp)[:e.pageSize]

	off, err := pageByteOffset(pageID, e.pageSize)
	if err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if _, err := e.db.ReadAt(buf, off); err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	out, err := e.hooks.transformRead(pageID, buf)
	if err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if sliceSameBase(out, buf) {
		return out, func() { e.pageBufPool.Put(bp) }, nil
	}
	e.pageBufPool.Put(bp)
	return out, releaseNothing, nil
}

// ReadPage returns page pageID (0 = header page including meta prefix).
// Internal btree paths use readPagePooled instead to avoid copying.
func (e *Engine) ReadPage(pageID uint64) ([]byte, error) {
	data, release, err := e.readPagePooled(pageID)
	if err != nil {
		return nil, err
	}
	defer release()
	return slices.Clone(data), nil
}

// WritePage writes a full data page (pageID >= 1). Logs redo, fsyncs, updates header.
func (e *Engine) WritePage(pageID uint64, data []byte) error {
	if e.db == nil || e.wal == nil {
		return fmt.Errorf("engine: closed")
	}
	if pageID < 1 {
		return ErrBadPageID
	}
	if len(data) != e.pageSize {
		return ErrBadPageSize
	}
	plain, err := e.hooks.transformWrite(pageID, append([]byte(nil), data...))
	if err != nil {
		return err
	}
	if len(plain) != e.pageSize {
		return ErrBadPageSize
	}

	if e.wtxn != nil {
		plainCopy := append([]byte(nil), plain...)
		seq := e.nextWALSeq.Add(1)
		e.wtxn.pending = append(e.wtxn.pending, pendingRedo{seq: seq, pageID: pageID, plain: plainCopy})
		e.wtxn.dirty[pageID] = plainCopy
		if pageID+1 > e.wtxn.hdr.NextPageID {
			e.wtxn.hdr.NextPageID = pageID + 1
		}
		return nil
	}

	seq := e.nextWALSeq.Add(1)
	rec := encodeWALRecordWithMAC(seq, pageID, plain, e.walMACKey, e.walMACEnabled, e.pageSize)
	if _, err := e.wal.Write(rec); err != nil {
		return err
	}
	if err := e.wal.Sync(); err != nil {
		return err
	}

	return e.persistRedoPage(seq, pageID, plain)
}

// persistRedoPage writes the plaintext page and header after the WAL record for seq
// is already durable (caller must have synced the WAL). Used by [Engine.WritePage]
// and by tests exploring batched WAL sync + sequential primary application.
func (e *Engine) persistRedoPage(seq, pageID uint64, plain []byte) error {
	off, err := pageByteOffset(pageID, e.pageSize)
	if err != nil {
		return err
	}
	if _, err := e.db.WriteAt(plain, off); err != nil {
		return err
	}
	if err := e.syncPrimary(); err != nil {
		return err
	}

	hdr, err := readHeaderAt(e.db, e.pageSize)
	if err != nil {
		return err
	}
	hdr.LastWALSeq = seq
	if pageID+1 > hdr.NextPageID {
		hdr.NextPageID = pageID + 1
	}
	if err := writeHeaderAt(e.db, hdr); err != nil {
		return err
	}
	if err := e.syncPrimary(); err != nil {
		return err
	}

	e.lastSeq = seq
	return nil
}

// Path returns the primary database path.
func (e *Engine) Path() string { return e.path }

// LastWALSeq returns the last applied redo sequence (for tests).
func (e *Engine) LastWALSeq() uint64 { return e.lastSeq }

// ReadHeader returns the current file header (page 0 prefix).
func (e *Engine) ReadHeader() (Header, error) {
	if e.db == nil {
		return Header{}, fmt.Errorf("engine: closed")
	}
	return e.visibleHeader()
}

// UpdateHeader reads the header, applies mut, writes page 0, and fsyncs the DB file.
// Preserves all fields mut does not change (e.g. BTreeRoot vs WAL seq).
// During a write transaction, mut is applied in memory only; disk is updated at
// [Engine.CommitWriteTxn].
func (e *Engine) UpdateHeader(mut func(*Header)) error {
	if e.db == nil {
		return fmt.Errorf("engine: closed")
	}
	if e.wtxn != nil {
		mut(&e.wtxn.hdr)
		return nil
	}
	hdr, err := readHeaderAt(e.db, e.pageSize)
	if err != nil {
		return err
	}
	mut(&hdr)
	if err := writeHeaderAt(e.db, hdr); err != nil {
		return err
	}
	return e.syncPrimary()
}
