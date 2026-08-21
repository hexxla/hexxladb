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
	// walSize tracks the number of bytes written to the WAL since the last reset.
	// Used to truncate to the last-written size (not zero) so the kernel retains
	// the allocated inode blocks, avoiding fallocate on the next write.
	walSize int64
	// cache is the optional CLOCK-Pro page cache. Nil when disabled.
	cache *pageCache
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
	// wastedBytes accumulates the logical byte size of freed overflow pages.
	// Since the engine has no freelist, freed pages become dead space until Compact.
	// This counter is in-memory only (resets on Open) and is exposed via WastedBytes().
	wastedBytes atomic.Uint64
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

	flags := os.O_RDWR | os.O_CREATE
	if opts != nil && opts.CreateExclusive {
		flags |= os.O_EXCL
	}
	// #nosec G304 -- path is the caller-chosen database file (public Open API).
	db, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	exclusiveOpen := opts != nil && opts.CreateExclusive
	exclusiveComplete := !exclusiveOpen
	walPath := WalPath(path)
	walCreated := false
	defer func() {
		if exclusiveComplete {
			return
		}
		_ = db.Close()
		_ = os.Remove(path)
		if walCreated {
			_ = os.Remove(walPath)
		}
	}()
	if err := lockDatabaseFile(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	st, err := db.Stat()
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	effectivePageSize, err := openBootstrapPageSize(db, st, opts, usePrimaryFdatasync)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	hdr, err := readHeaderAt(db, effectivePageSize)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := openValidateEncryptionKey(opts, hdr); err != nil {
		_ = db.Close()
		return nil, err
	}

	walFlags := os.O_RDWR | os.O_CREATE
	if exclusiveOpen {
		walFlags |= os.O_EXCL
	}
	// #nosec G304 -- WAL path is derived from the primary DB path (same contract as primary).
	wal, err := os.OpenFile(walPath, walFlags, 0o600)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	walCreated = exclusiveOpen

	newLast, walMACEnabled, walMACKey, err := openReplayWAL(db, wal, &hdr, opts, effectivePageSize, usePrimaryFdatasync)
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	effectiveMaxVal, err := openFinalizeHeader(db, &hdr, opts)
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	if err := openValidateEmbedding(opts, hdr); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
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
		e.cache = newPageCache(opts.PageCacheSize)
	}
	e.nextWALSeq.Store(newLast)
	if e.groupWALCfg.Enabled {
		e.startGroupWALFlusher()
	}
	exclusiveComplete = true
	return e, nil
}

// openBootstrapPageSize determines the effective page size — either from opts for new files,
// or from the existing header prefix.
func openBootstrapPageSize(db *os.File, st os.FileInfo, opts *Options, usePrimaryFdatasync bool) (int, error) {
	if st.Size() == 0 {
		return openInitNewDB(db, opts, usePrimaryFdatasync)
	}
	ps, err := bootstrapPageSize(db)
	if err != nil {
		return 0, err
	}
	if st.Size() < int64(ps) {
		return 0, ErrCorruptHeader
	}
	return int(ps), nil
}

// openInitNewDB writes the initial header for a brand-new database and returns the page size.
func openInitNewDB(db *os.File, opts *Options, usePrimaryFdatasync bool) (int, error) {
	ps, err := resolvePageSize(opts)
	if err != nil {
		return 0, err
	}
	mvb, err := resolveMaxValueBytes(opts)
	if err != nil {
		return 0, err
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
			return 0, fmt.Errorf("%w: invalid distance metric %d", ErrInvalidEmbeddingConfig, embMetric)
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
		if err := openApplyEncryptionToHeader(&hdr, opts); err != nil {
			return 0, err
		}
	}
	if err := writeHeaderAt(db, hdr); err != nil {
		return 0, err
	}
	if err := syncFilePrimary(db, usePrimaryFdatasync); err != nil {
		return 0, err
	}
	return int(ps), nil
}

// openApplyEncryptionToHeader sets encryption features and salt on a new header.
func openApplyEncryptionToHeader(hdr *Header, opts *Options) error {
	hdr.Features |= FeatureEncryptedDataPages
	if opts.EnableWALMAC {
		hdr.Features |= FeatureWALKeyedMAC
	}
	if opts.EncryptionSalt == ([16]byte{}) {
		if _, err := rand.Read(hdr.EncryptionSalt[:]); err != nil {
			return err
		}
	} else {
		hdr.EncryptionSalt = opts.EncryptionSalt
	}
	hdr.EncryptionKeyCheck = opts.EncryptionKeyCheck
	return nil
}

// openValidateEncryptionKey checks if the encryption key matches the stored key check.
func openValidateEncryptionKey(opts *Options, hdr Header) error {
	if opts == nil || !opts.ExpectEncryptionKeyCheck {
		return nil
	}
	if hdr.Features&FeatureEncryptedDataPages == 0 {
		return nil
	}
	if hdr.EncryptionKeyCheck != ([HeaderEncryptionKeyCheckLen]byte{}) && hdr.EncryptionKeyCheck != opts.EncryptionKeyCheck {
		return ErrBadEncryptionKey
	}
	return nil
}

// openReplayWAL reads and replays the WAL, then truncates it.
// Returns the new last WAL sequence, MAC config, and any error.
func openReplayWAL(db, wal *os.File, hdr *Header, opts *Options, pageSize int, usePrimaryFdatasync bool) (lastSeq uint64, macEnabled bool, macKey [32]byte, err error) {
	const maxWALReplayBytes = 16 << 30

	walInfo, statErr := wal.Stat()
	if statErr != nil {
		return 0, false, macKey, statErr
	}
	if walInfo.Size() > maxWALReplayBytes {
		return 0, false, macKey, fmt.Errorf("%w: WAL size %d exceeds max replay size", ErrCorruptWAL, walInfo.Size())
	}

	walData, readErr := openReadWALData(wal, walInfo.Size())
	if readErr != nil {
		return 0, false, macKey, readErr
	}

	apply := func(_ uint64, pageID uint64, payload []byte) error {
		off, offErr := pageByteOffset(pageID, pageSize)
		if offErr != nil {
			return offErr
		}
		_, offErr = db.WriteAt(payload, off)
		return offErr
	}

	macEnabled = hdr.Features&FeatureWALKeyedMAC != 0
	if macEnabled {
		if opts == nil || !opts.EnableWALMAC {
			return 0, false, macKey, ErrBadEncryptionKey
		}
		macKey = opts.WALMACKey
	}

	maxSeq, replayErr := parseAndReplayWALWithMAC(walData, hdr.LastWALSeq, apply, macKey, macEnabled, pageSize)
	if replayErr != nil {
		return 0, false, macKey, replayErr
	}

	lastSeq = max(hdr.LastWALSeq, maxSeq)
	if lastSeq != hdr.LastWALSeq {
		hdr.LastWALSeq = lastSeq
		if wErr := writeHeaderAt(db, *hdr); wErr != nil {
			return 0, false, macKey, wErr
		}
		if sErr := syncFilePrimary(db, usePrimaryFdatasync); sErr != nil {
			return 0, false, macKey, sErr
		}
	}

	if tErr := openTruncateWAL(wal); tErr != nil {
		return 0, false, macKey, tErr
	}
	return lastSeq, macEnabled, macKey, nil
}

// openReadWALData reads all WAL bytes from the beginning.
func openReadWALData(wal *os.File, size int64) ([]byte, error) {
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	walData := make([]byte, size)
	if len(walData) > 0 {
		if _, err := io.ReadFull(wal, walData); err != nil {
			return nil, err
		}
	}
	return walData, nil
}

// openTruncateWAL truncates and syncs the WAL file after replay.
func openTruncateWAL(wal *os.File) error {
	if err := wal.Truncate(0); err != nil {
		return err
	}
	if _, err := wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return wal.Sync()
}

// openFinalizeHeader updates MaxValueBytes in the header if needed and returns effective max.
func openFinalizeHeader(db *os.File, hdr *Header, opts *Options) (uint32, error) {
	if opts != nil && opts.MaxValueBytes != 0 && opts.MaxValueBytes != hdr.MaxValueBytes {
		mvb, err := resolveMaxValueBytes(opts)
		if err != nil {
			return 0, err
		}
		hdr.MaxValueBytes = mvb
		if err := writeHeaderAt(db, *hdr); err != nil {
			return 0, err
		}
	}
	effective := hdr.MaxValueBytes
	if effective == 0 {
		effective = DefaultMaxValueBytes
	}
	return effective, nil
}

// openValidateEmbedding checks embedding config consistency between opts and header on reopen.
func openValidateEmbedding(opts *Options, hdr Header) error {
	if opts == nil || opts.EmbeddingDim == 0 || hdr.EmbeddingDim == 0 {
		return nil
	}
	if opts.EmbeddingDim != hdr.EmbeddingDim {
		return fmt.Errorf("%w: embedding dimension mismatch: options=%d, header=%d", ErrInvalidEmbeddingConfig, opts.EmbeddingDim, hdr.EmbeddingDim)
	}
	if opts.EmbeddingMetric != hdr.EmbeddingMetric {
		return fmt.Errorf("%w: embedding metric mismatch: options=%d, header=%d", ErrInvalidEmbeddingConfig, opts.EmbeddingMetric, hdr.EmbeddingMetric)
	}
	return nil
}

// Close releases file handles.
func (e *Engine) Close() error {
	e.wtxn = nil
	e.cache = nil
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

// PageCacheStats returns cumulative page cache hit and miss counts.
// Both are zero when the cache is disabled (Options.PageCacheSize == 0).
func (e *Engine) PageCacheStats() (hits, misses int64) {
	if e.cache == nil {
		return 0, 0
	}
	return e.cache.stats()
}

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
			return e.pooledTransformRead(0, encodeHeaderPage(e.wtxn.hdr))
		}
		if plain, ok := e.wtxn.dirty[pageID]; ok {
			return e.pooledTransformRead(pageID, plain)
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
			return e.pooledTransformRead(pageID, plain)
		}
	}

	// Page 0: logical header (includes group-WAL staging when ahead of primary).
	if pageID == 0 {
		vh, err := e.visibleHeader()
		if err != nil {
			return nil, releaseNothing, err
		}
		return e.pooledTransformRead(0, encodeHeaderPage(vh))
	}

	return e.readPageFromDisk(pageID)
}

// pooledTransformRead copies src into a pooled buffer, applies transformRead, and
// returns the result with an appropriate release function.
func (e *Engine) pooledTransformRead(pageID uint64, src []byte) (data []byte, release func(), err error) {
	bp := e.pageBufPool.Get().(*[]byte)
	buf := (*bp)[:e.pageSize]
	copy(buf, src)
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

// readPageFromDisk reads a data page directly from the primary file, consulting
// the page cache first when enabled.
func (e *Engine) readPageFromDisk(pageID uint64) (data []byte, release func(), err error) {
	// Cache check: on hit return a pooled copy so the caller's release contract is unchanged.
	if e.cache != nil {
		if cached := e.cache.get(pageID); cached != nil {
			bp := e.pageBufPool.Get().(*[]byte)
			buf := (*bp)[:e.pageSize]
			copy(buf, cached)
			return buf, func() { e.pageBufPool.Put(bp) }, nil
		}
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
	// Populate cache with the post-transform (decrypted) bytes.
	if e.cache != nil {
		e.cache.set(pageID, out)
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
	if err := e.writePrimaryData(pageID, plain); err != nil {
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

// WastedBytes returns the cumulative logical byte size of overflow-page chains
// freed since this [Engine] was opened. These pages are dead space on disk until
// the next [CompactTo]. The counter resets to zero on [Open] / [Engine.Close] + reopen.
func (e *Engine) WastedBytes() uint64 { return e.wastedBytes.Load() }

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
	_, err := e.UpdateHeaderGet(mut)
	return err
}

// UpdateHeaderGet is like [UpdateHeader] but also returns the header value after
// mut is applied. Callers that need to cache the resulting header (e.g. BTreeRoot)
// can use this to avoid a second [ReadHeader] pread.
func (e *Engine) UpdateHeaderGet(mut func(*Header)) (Header, error) {
	if e.db == nil {
		return Header{}, fmt.Errorf("engine: closed")
	}
	if e.wtxn != nil {
		mut(&e.wtxn.hdr)
		return e.wtxn.hdr, nil
	}
	hdr, err := readHeaderAt(e.db, e.pageSize)
	if err != nil {
		return Header{}, err
	}
	mut(&hdr)
	if err := writeHeaderAt(e.db, hdr); err != nil {
		return Header{}, err
	}
	return hdr, e.syncPrimary()
}
