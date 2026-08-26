package engine

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/hexxla/hexxladb/internal/fsutil"
)

// Engine is a minimal page-oriented store with redo WAL.
type Engine struct {
	path     string
	db       *os.File
	wal      *os.File
	hooks    PageHooks
	pageSize int // logical page size in bytes (set at Open, immutable)
	// physicalPageSize includes any authenticated data-page envelope.
	physicalPageSize int
	// pageReuseEnabled is false only for legacy formats and controlled evidence baselines.
	pageReuseEnabled bool
	lastSeq          uint64
	// nextWALSeq is the high-water for allocating redo [pendingRedo.seq] values; it must stay
	// monotonic when write transactions can overlap the group-WAL flusher. Initialized from lastSeq
	// at [Open] (first Add(1) yields lastSeq+1).
	nextWALSeq         atomic.Uint64
	walMACEnabled      bool
	walMACKey          [32]byte
	headerMACEnabled   bool
	headerMACKey       [32]byte
	lastPageID         uint64
	lastPageGeneration uint64
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
	// wastedBytes accumulates legacy extend-only overflow waste. This counter is
	// in-memory only (resets on Open) and is exposed via WastedBytes().
	wastedBytes atomic.Uint64
}

// WalPath returns the WAL path for a primary database path.
func WalPath(primary string) string {
	return primary + "-wal"
}

// Open opens or creates a database at path and replays the WAL.
func Open(path string, opts *Options) (*Engine, error) {
	return open(path, opts, fsutil.SyncParents)
}

func open(path string, opts *Options, syncParents func(...string) error) (*Engine, error) {
	var hooks PageHooks
	if opts != nil && opts.Hooks != nil {
		hooks = *opts.Hooks
	}
	usePrimaryFdatasync := opts != nil && opts.UsePrimaryFdatasync

	exclusiveOpen := opts != nil && opts.CreateExclusive
	db, dbCreated, err := openEngineFile(path, exclusiveOpen)
	if err != nil {
		return nil, err
	}
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
	physicalPageSize, err := physicalPageSizeForHeader(hdr, hooks, opts)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	wal, walCreated, err := openEngineFile(walPath, exclusiveOpen)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	newLast, walMACEnabled, walMACKey, err := openReplayWAL(db, wal, &hdr, opts, effectivePageSize, physicalPageSize, usePrimaryFdatasync)
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
	if dbCreated || walCreated {
		if err := syncParents(path, walPath); err != nil {
			_ = db.Close()
			_ = wal.Close()
			return nil, fmt.Errorf("engine: make database files durable: %w", err)
		}
	}

	e := &Engine{
		path:                path,
		db:                  db,
		wal:                 wal,
		hooks:               hooks,
		pageSize:            effectivePageSize,
		physicalPageSize:    physicalPageSize,
		pageReuseEnabled:    resolvePageReuseEnabled(hdr, opts),
		lastSeq:             newLast,
		walMACEnabled:       walMACEnabled,
		walMACKey:           walMACKey,
		headerMACEnabled:    hdr.FormatVersion == formatVersionV3,
		usePrimaryFdatasync: usePrimaryFdatasync,
		maxValueBytes:       effectiveMaxVal,
		embeddingDim:        hdr.EmbeddingDim,
		embeddingMetric:     hdr.EmbeddingMetric,
	}
	if opts != nil {
		e.headerMACKey = opts.HeaderMACKey
	}
	e.pageBufPool = sync.Pool{
		New: func() any {
			b := make([]byte, max(e.pageSize, e.physicalPageSize))
			return &b
		},
	}
	if err := e.validateFreelistOnOpen(hdr); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
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

func resolvePageReuseEnabled(hdr Header, opts *Options) bool {
	return hdr.FormatVersion == formatVersionV3 && (opts == nil || !opts.disablePageReuse)
}

func openEngineFile(path string, exclusive bool) (*os.File, bool, error) {
	if !exclusive {
		return fsutil.OpenReadWrite(path, 0o600)
	}
	// #nosec G304 -- path is the caller-selected primary or its derived WAL path.
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	return file, err == nil, err
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
	if opts != nil && opts.UseFormatV3 {
		ver = formatVersionV3
	} else if opts != nil && opts.UseFormatV2 {
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
	if opts != nil && opts.NewIncompleteCompaction {
		hdr.Features |= FeatureIncompleteCompaction
	}
	headerMACEnabled := opts != nil && opts.NewAuthenticatedDB
	var headerMACKey [32]byte
	if opts != nil {
		headerMACKey = opts.HeaderMACKey
	}
	if err := writeHeaderAtAuthenticated(db, hdr, headerMACKey, headerMACEnabled); err != nil {
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
	if opts.NewAuthenticatedDB {
		if !opts.UseFormatV3 || !opts.EnableWALMAC || !opts.EnableHeaderMAC {
			return fmt.Errorf("%w: authenticated format requires v3, WAL MAC, and header MAC", ErrCorruptHeader)
		}
		hdr.Features |= FeatureAuthenticatedDataPages
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
	if opts != nil && opts.ExpectEncryptionKeyCheck && hdr.Features&FeatureEncryptedDataPages != 0 &&
		hdr.EncryptionKeyCheck != ([HeaderEncryptionKeyCheckLen]byte{}) && hdr.EncryptionKeyCheck != opts.EncryptionKeyCheck {
		return ErrBadEncryptionKey
	}
	if hdr.FormatVersion == formatVersionV3 {
		if opts == nil || !opts.EnableHeaderMAC || !verifyHeaderAuthentication(hdr, opts.HeaderMACKey) {
			return ErrPageAuthentication
		}
	}
	return nil
}

func physicalPageSizeForHeader(hdr Header, hooks PageHooks, opts *Options) (int, error) {
	if hdr.FormatVersion != formatVersionV3 {
		if hdr.Features&FeatureAuthenticatedDataPages != 0 || hooks.PhysicalPageOverhead != 0 {
			return 0, fmt.Errorf("%w: authenticated page layout requires format v3", ErrCorruptHeader)
		}
		return int(hdr.PageSize), nil
	}
	required := FeatureEncryptedDataPages | FeatureWALKeyedMAC | FeatureAuthenticatedDataPages
	if hdr.Features&required != required || opts == nil || !opts.EnableWALMAC || !opts.EnableHeaderMAC {
		return 0, fmt.Errorf("%w: incomplete authenticated format features", ErrCorruptHeader)
	}
	if hooks.PhysicalPageOverhead != AuthenticatedPageOverhead || hooks.BeforeWriteVersioned == nil || hooks.AfterRead == nil {
		return 0, fmt.Errorf("%w: authenticated page transform unavailable", ErrBadEncryptionKey)
	}
	return int(hdr.PageSize) + AuthenticatedPageOverhead, nil
}

type replayPage struct {
	pageID  uint64
	payload []byte
}

type openWALReplay struct {
	db               *os.File
	hdr              *Header
	opts             *Options
	pageSize         int
	physicalPageSize int
	useFdatasync     bool
	pending          []replayPage
	committed        []replayPage
	replayHeader     *Header
}

// openReplayWAL reads and replays the WAL, then truncates it.
// Returns the new last WAL sequence, MAC config, and any error.
func openReplayWAL(db, wal *os.File, hdr *Header, opts *Options, pageSize, physicalPageSize int, usePrimaryFdatasync bool) (uint64, bool, [32]byte, error) {
	walData, err := openValidatedWALData(wal)
	if err != nil {
		return 0, false, [32]byte{}, err
	}
	macEnabled, macKey, err := openWALMACConfig(hdr, opts)
	if err != nil {
		return 0, false, macKey, err
	}
	replay := &openWALReplay{
		db: db, hdr: hdr, opts: opts,
		pageSize: pageSize, physicalPageSize: physicalPageSize,
		useFdatasync: usePrimaryFdatasync,
	}
	maxSeq, err := parseAndReplayWALWithMAC(
		walData,
		hdr.LastWALSeq,
		replay.apply,
		macKey,
		macEnabled,
		physicalPageSize,
	)
	if err != nil {
		return 0, false, macKey, err
	}
	lastSeq := max(hdr.LastWALSeq, maxSeq)
	if lastSeq != hdr.LastWALSeq {
		if err := replay.publish(lastSeq); err != nil {
			return 0, false, macKey, err
		}
	}
	if err := openTruncateWAL(wal); err != nil {
		return 0, false, macKey, err
	}
	return lastSeq, macEnabled, macKey, nil
}

func openValidatedWALData(wal *os.File) ([]byte, error) {
	const maxWALReplayBytes = 16 << 30
	info, err := wal.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxWALReplayBytes {
		return nil, fmt.Errorf("%w: WAL size %d exceeds max replay size", ErrCorruptWAL, info.Size())
	}
	return openReadWALData(wal, info.Size())
}

func openWALMACConfig(hdr *Header, opts *Options) (bool, [32]byte, error) {
	enabled := hdr.Features&FeatureWALKeyedMAC != 0
	if !enabled {
		return false, [32]byte{}, nil
	}
	if opts == nil || !opts.EnableWALMAC {
		return false, [32]byte{}, ErrBadEncryptionKey
	}
	return true, opts.WALMACKey, nil
}

func (r *openWALReplay) apply(seq, pageID uint64, payload []byte) error {
	if pageID == 0 {
		return r.applyHeaderMarker(seq, payload)
	}
	if r.hdr.FormatVersion == formatVersionV3 {
		if len(payload) < 8 || binary.BigEndian.Uint64(payload[:8]) != seq {
			return ErrCorruptWAL
		}
		r.pending = append(r.pending, replayPage{pageID: pageID, payload: payload})
		return nil
	}
	off, err := pageByteOffset(pageID, r.pageSize, r.physicalPageSize)
	if err != nil {
		return err
	}
	_, err = r.db.WriteAt(payload, off)
	return err
}

func (r *openWALReplay) applyHeaderMarker(seq uint64, payload []byte) error {
	if r.hdr.FormatVersion != formatVersionV3 || len(payload) < r.pageSize || r.opts == nil {
		return ErrCorruptWAL
	}
	candidate, err := decodeHeaderPage(payload[:r.pageSize])
	if err != nil || candidate.FormatVersion != formatVersionV3 || candidate.LastWALSeq != seq ||
		candidate.EncryptionSalt != r.hdr.EncryptionSalt || !verifyHeaderAuthentication(candidate, r.opts.HeaderMACKey) {
		return ErrCorruptWAL
	}
	r.committed = append(r.committed[:0], r.pending...)
	r.pending = r.pending[:0]
	r.replayHeader = &candidate
	return nil
}

func (r *openWALReplay) publish(lastSeq uint64) error {
	if r.hdr.FormatVersion == formatVersionV3 {
		if err := r.publishAuthenticatedPages(lastSeq); err != nil {
			return err
		}
	} else {
		r.hdr.LastWALSeq = lastSeq
	}
	var headerKey [32]byte
	headerMACEnabled := false
	if r.opts != nil {
		headerKey = r.opts.HeaderMACKey
		headerMACEnabled = r.opts.EnableHeaderMAC && r.hdr.FormatVersion == formatVersionV3
	}
	if err := writeHeaderAtAuthenticated(r.db, *r.hdr, headerKey, headerMACEnabled); err != nil {
		return err
	}
	return syncFilePrimary(r.db, r.useFdatasync)
}

func (r *openWALReplay) publishAuthenticatedPages(lastSeq uint64) error {
	if r.replayHeader == nil || r.replayHeader.LastWALSeq != lastSeq {
		return ErrCorruptWAL
	}
	for i := range r.committed {
		page := &r.committed[i]
		off, err := pageByteOffset(page.pageID, r.pageSize, r.physicalPageSize)
		if err != nil {
			return err
		}
		if _, err := r.db.WriteAt(page.payload, off); err != nil {
			return err
		}
	}
	if err := syncFilePrimary(r.db, r.useFdatasync); err != nil {
		return err
	}
	*r.hdr = *r.replayHeader
	return nil
}

func (e *Engine) encodeHeaderWALRecord(hdr Header) []byte {
	authenticated := authenticateHeader(hdr, e.headerMACKey)
	payload := make([]byte, e.physicalPageSize)
	copy(payload, encodeHeaderPage(authenticated))
	return encodeWALRecordWithMAC(
		hdr.LastWALSeq,
		0,
		payload,
		e.walMACKey,
		e.walMACEnabled,
		e.physicalPageSize,
	)
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
		if err := writeHeaderAtAuthenticated(db, *hdr, opts.HeaderMACKey, opts.EnableHeaderMAC && hdr.FormatVersion == formatVersionV3); err != nil {
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
	disk, err := e.readDiskHeader()
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

func (e *Engine) readDiskHeader() (Header, error) {
	hdr, err := readHeaderAt(e.db, e.pageSize)
	if err != nil {
		return Header{}, err
	}
	if e.headerMACEnabled && !verifyHeaderAuthentication(hdr, e.headerMACKey) {
		return Header{}, ErrPageAuthentication
	}
	return hdr, nil
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
	if len(src) > cap(*bp) {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, ErrBadPageSize
	}
	buf := (*bp)[:len(src)]
	copy(buf, src)
	if err := e.validateRootPageGeneration(pageID, buf); err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	out, err := e.hooks.transformRead(pageID, buf)
	if err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if pageID >= 1 && len(out) != e.pageSize {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, ErrBadPageSize
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
	bp := e.pageBufPool.Get().(*[]byte)
	buf := (*bp)[:e.physicalPageSize]
	// Copy cache hits directly into the caller-owned pooled page. Allocating an
	// intermediate page here multiplies HNSW traversal cost by every B+ tree
	// lookup, especially with 64 KiB pages.
	if e.cache != nil && e.cache.get(pageID, buf[:e.pageSize]) {
		return buf[:e.pageSize], func() { e.pageBufPool.Put(bp) }, nil
	}

	off, err := pageByteOffset(pageID, e.pageSize, e.physicalPageSize)
	if err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if _, err := e.db.ReadAt(buf, off); err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if err := e.validateRootPageGeneration(pageID, buf); err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	out, err := e.hooks.transformRead(pageID, buf)
	if err != nil {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, err
	}
	if len(out) != e.pageSize {
		e.pageBufPool.Put(bp)
		return nil, releaseNothing, ErrBadPageSize
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
	_, err := e.writePageWithGeneration(pageID, data)
	return err
}

// writePageWithGeneration is WritePage plus the authenticated rewrite generation.
// Allocator metadata uses the returned value to authenticate generation-linked pages.
func (e *Engine) writePageWithGeneration(pageID uint64, data []byte) (uint64, error) {
	if e.db == nil || e.wal == nil {
		return 0, fmt.Errorf("engine: closed")
	}
	if pageID < 1 {
		return 0, ErrBadPageID
	}
	if len(data) != e.pageSize {
		return 0, ErrBadPageSize
	}
	// Own one copy before invoking a hook: callers and hooks may mutate their
	// input after WritePage returns, while a write transaction retains the page
	// until its durability barrier. If the hook transforms into a different
	// backing buffer, retain a private copy of that result as well.
	owned := slices.Clone(data)
	seq := e.nextWALSeq.Add(1)
	plain, err := e.hooks.transformWrite(pageID, seq, owned)
	if err != nil {
		return 0, err
	}
	if len(plain) != e.physicalPageSize {
		return 0, ErrBadPageSize
	}

	if e.wtxn != nil {
		plainCopy := plain
		if !sliceSameBase(plain, owned) {
			plainCopy = slices.Clone(plain)
		}
		e.wtxn.pending = append(e.wtxn.pending, pendingRedo{seq: seq, pageID: pageID, plain: plainCopy})
		e.wtxn.dirty[pageID] = plainCopy
		if pageID == e.wtxn.hdr.BTreeRoot {
			e.wtxn.hdr.BTreeRootGeneration = seq
		}
		if pageID+1 > e.wtxn.hdr.NextPageID {
			e.wtxn.hdr.NextPageID = pageID + 1
		}
		return seq, nil
	}

	rec := encodeWALRecordWithMAC(seq, pageID, plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
	if _, err := e.wal.Write(rec); err != nil {
		return 0, err
	}
	if e.headerMACEnabled {
		hdr, err := e.readDiskHeader()
		if err != nil {
			return 0, err
		}
		hdr.LastWALSeq = seq
		if pageID == hdr.BTreeRoot {
			hdr.BTreeRootGeneration = seq
		}
		if pageID+1 > hdr.NextPageID {
			hdr.NextPageID = pageID + 1
		}
		if _, err := e.wal.Write(e.encodeHeaderWALRecord(hdr)); err != nil {
			return 0, err
		}
	}
	if err := e.wal.Sync(); err != nil {
		return 0, err
	}

	if err := e.persistRedoPage(seq, pageID, plain); err != nil {
		return 0, err
	}
	return seq, nil
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

	hdr, err := e.readDiskHeader()
	if err != nil {
		return err
	}
	hdr.LastWALSeq = seq
	if pageID == hdr.BTreeRoot {
		hdr.BTreeRootGeneration = seq
	}
	if pageID+1 > hdr.NextPageID {
		hdr.NextPageID = pageID + 1
	}
	if err := e.writeHeader(hdr); err != nil {
		return err
	}
	if err := e.syncPrimary(); err != nil {
		return err
	}

	e.lastSeq = seq
	e.lastPageID = pageID
	e.lastPageGeneration = seq
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
		e.refreshRootGeneration(&e.wtxn.hdr, e.wtxn.pending)
		return e.wtxn.hdr, nil
	}
	hdr, err := e.readDiskHeader()
	if err != nil {
		return Header{}, err
	}
	mut(&hdr)
	switch hdr.BTreeRoot {
	case 0:
		hdr.BTreeRootGeneration = 0
	case e.lastPageID:
		hdr.BTreeRootGeneration = e.lastPageGeneration
	}
	if err := e.writeHeader(hdr); err != nil {
		return Header{}, err
	}
	return hdr, e.syncPrimary()
}

func (e *Engine) writeHeader(hdr Header) error {
	return writeHeaderAtAuthenticated(e.db, hdr, e.headerMACKey, e.headerMACEnabled)
}

func (e *Engine) refreshRootGeneration(hdr *Header, pending []pendingRedo) {
	if hdr.BTreeRoot == 0 {
		hdr.BTreeRootGeneration = 0
		return
	}
	for i := range slices.Backward(pending) {
		if pending[i].pageID == hdr.BTreeRoot {
			hdr.BTreeRootGeneration = pending[i].seq
			return
		}
	}
}

func (e *Engine) validateRootPageGeneration(pageID uint64, physical []byte) error {
	if !e.headerMACEnabled || pageID == 0 {
		return nil
	}
	hdr, err := e.visibleHeader()
	if err != nil {
		return err
	}
	if pageID != hdr.BTreeRoot {
		return nil
	}
	if len(physical) < 8 || binary.BigEndian.Uint64(physical[:8]) != hdr.BTreeRootGeneration {
		return fmt.Errorf("%w: stale root page %d", ErrPageAuthentication, pageID)
	}
	return nil
}
