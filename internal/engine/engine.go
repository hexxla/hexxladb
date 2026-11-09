package engine

import (
	"encoding/binary"
	"errors"
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
	// monotonic across commits. Initialized from lastSeq at [Open].
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
	// inflatePool retains decoder scratch only within this engine instance.
	inflatePool sync.Pool
	// cache is the optional CLOCK-Pro page cache. Nil when disabled.
	cache *pageCache
	// wtxn is set between [Engine.BeginWriteTxn] and commit/abort. Not used concurrently.
	wtxn *writeTxnState
	// commitFailed fences later mutations after an uncertain I/O outcome.
	commitFailed   bool
	walCommits     atomic.Uint64
	walCommitSyncs atomic.Uint64
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
	db, dbCreated, err := openPrimaryFile(path, opts, exclusiveOpen)
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

	hdr, primaryHeaderErr, err := readHeaderForRecovery(db, effectivePageSize)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := openValidateEncryptionKey(opts, hdr); err != nil {
		if !errors.Is(err, ErrPageAuthentication) {
			_ = db.Close()
			return nil, err
		}
		primaryHeaderErr = err
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

	newLast, walMACEnabled, walMACKey, err := openReplayWAL(db, wal, &hdr, opts, effectivePageSize, physicalPageSize, usePrimaryFdatasync, primaryHeaderErr)
	if err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}

	effectiveMaxVal, err := resolveOpenMaxValueBytes(hdr, opts)
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
			return new(make([]byte, max(e.pageSize, e.physicalPageSize)))
		},
	}
	if err := e.validateFreelistOnOpen(hdr); err != nil {
		_ = db.Close()
		_ = wal.Close()
		return nil, err
	}
	if opts != nil {
		e.cache = newPageCache(opts.PageCacheSize, e.pageSize)
	}
	e.nextWALSeq.Store(newLast)
	if effectiveMaxVal != hdr.MaxValueBytes && opts != nil && opts.MaxValueBytes != 0 {
		if err := e.UpdateHeader(func(h *Header) { h.MaxValueBytes = effectiveMaxVal }); err != nil {
			_ = e.Close()
			return nil, err
		}
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

func openPrimaryFile(path string, opts *Options, exclusive bool) (*os.File, bool, error) {
	if exclusive || opts == nil || !opts.RequireExistingPrimary {
		return openEngineFile(path, exclusive)
	}
	// #nosec G304 -- path is the caller-selected maintenance source.
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	return file, false, err
}

// openBootstrapPageSize determines the effective page size — either from opts for new files,
// or from the existing header prefix.
func openBootstrapPageSize(db *os.File, st os.FileInfo, opts *Options, usePrimaryFdatasync bool) (int, error) {
	if st.Size() == 0 {
		if opts != nil && opts.RequireExistingPrimary {
			return 0, ErrCorruptHeader
		}
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
	ver := formatVersionV2
	if opts != nil && opts.NewAuthenticatedDB {
		ver = formatVersionV3
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
	if opts != nil && opts.NewAuthenticatedDB {
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
	if !opts.EnableWALMAC || !opts.EnableHeaderMAC || opts.EncryptionSalt == ([16]byte{}) {
		return fmt.Errorf("%w: authenticated format requires WAL MAC, header MAC, and a database salt", ErrCorruptHeader)
	}
	hdr.Features |= FeatureEncryptedDataPages | FeatureWALKeyedMAC | FeatureAuthenticatedDataPages
	hdr.EncryptionSalt = opts.EncryptionSalt
	hdr.EncryptionKeyCheck = opts.EncryptionKeyCheck
	return nil
}

// openValidateEncryptionKey checks if the encryption key matches the stored key check.
func openValidateEncryptionKey(opts *Options, hdr Header) error {
	if opts != nil && hdr.FormatVersion == formatVersionV3 &&
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

// resolveOpenMaxValueBytes validates configuration without mutating persistent state.
func resolveOpenMaxValueBytes(hdr Header, opts *Options) (uint32, error) {
	if opts != nil && opts.MaxValueBytes != 0 {
		return resolveMaxValueBytes(opts)
	}
	if hdr.MaxValueBytes == 0 {
		return DefaultMaxValueBytes, nil
	}
	return hdr.MaxValueBytes, nil
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
	var errs []error
	if e.wal != nil {
		errs = append(errs, e.wal.Close())
		e.wal = nil
	}
	if e.db != nil {
		errs = append(errs, e.db.Close())
		e.db = nil
	}
	errs = slices.DeleteFunc(errs, func(err error) bool { return err == nil })
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
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

// visibleHeader returns the active write transaction header or the durable header.
func (e *Engine) visibleHeader() (Header, error) {
	if e.commitFailed {
		return Header{}, ErrRecoveryRequired
	}
	if e.wtxn != nil {
		return e.wtxn.hdr, nil
	}
	if e.db == nil {
		return Header{}, fmt.Errorf("engine: closed")
	}
	return e.readDiskHeader()
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
	if e.commitFailed {
		return nil, releaseNothing, ErrRecoveryRequired
	}
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

	// Page 0 is the logical header.
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

// WritePage stages a full data page or commits it immediately when no transaction is open.
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
	if e.commitFailed {
		return 0, ErrRecoveryRequired
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
		page := pendingRedo{seq: seq, pageID: pageID, plain: plainCopy}
		if i, ok := e.wtxn.pendingIndex[pageID]; ok {
			e.wtxn.pending[i] = page
		} else {
			recordSize := walRecordSize(e.physicalPageSize)
			if e.walMACEnabled {
				recordSize += walRecordMACSize
			}
			if int64(len(e.wtxn.pending)+2) > maxWALReplayBytes/int64(recordSize) {
				return 0, ErrWALTooLarge
			}
			e.wtxn.pendingIndex[pageID] = len(e.wtxn.pending)
			e.wtxn.pending = append(e.wtxn.pending, page)
		}
		e.wtxn.dirty[pageID] = plainCopy
		if pageID == e.wtxn.hdr.BTreeRoot {
			e.wtxn.hdr.BTreeRootGeneration = seq
		}
		e.wtxn.hdr.NextPageID = max(e.wtxn.hdr.NextPageID, pageID+1)
		return seq, nil
	}

	hdr, err := e.readDiskHeader()
	if err != nil {
		return 0, err
	}
	if pageID == hdr.BTreeRoot {
		hdr.BTreeRootGeneration = seq
	}
	hdr.NextPageID = max(hdr.NextPageID, pageID+1)
	return seq, e.commitPages(hdr, []pendingRedo{{seq: seq, pageID: pageID, plain: plain}})
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
	if err := e.commitPages(hdr, nil); err != nil {
		return Header{}, err
	}
	hdr.LastWALSeq = e.lastSeq
	return hdr, nil
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
