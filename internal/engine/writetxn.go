package engine

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/hexxla/hexxladb/internal/engine/crashtest"
)

// writeTxnState holds in-memory state for a single engine write transaction (one
// [DB.Update] callback). Redo is buffered in memory until [Engine.CommitWriteTxn].
type writeTxnState struct {
	hdr     Header
	pending []pendingRedo
	// dirty holds on-disk form payload per key (pageID >= 1), same as WAL/plain from transformWrite.
	dirty           map[uint64][]byte
	pendingIndex    map[uint64]int
	freePageIDs     []uint64
	freePageSet     map[uint64]struct{}
	freelistPageIDs []uint64
	freelistDirty   bool
}

type pendingRedo struct {
	seq, pageID uint64
	plain       []byte
}

// BeginWriteTxn starts a write transaction. While active, [Engine.WritePage] buffers redo
// and [Engine.readPagePooled] may read from the dirty set; [Engine.ReadHeader] and
// [Engine.UpdateHeader] use an in-memory header until [Engine.CommitWriteTxn] or
// [Engine.AbortWriteTxn] is called. Only one write transaction per engine is allowed.
func (e *Engine) BeginWriteTxn() error {
	if e == nil || e.db == nil {
		return fmt.Errorf("engine: closed")
	}
	if e.commitFailed {
		return ErrRecoveryRequired
	}
	if e.wtxn != nil {
		return ErrWriteTxnActive
	}
	hdr, err := e.visibleHeader()
	if err != nil {
		return err
	}
	e.wtxn = &writeTxnState{
		hdr:          hdr,
		dirty:        make(map[uint64][]byte),
		pendingIndex: make(map[uint64]int),
	}
	if err := e.loadWriteTxnFreelist(); err != nil {
		e.wtxn = nil
		return err
	}
	return nil
}

// CommitWriteTxn publishes buffered pages and their final header together.
func (e *Engine) CommitWriteTxn() error {
	if e == nil || e.db == nil {
		return fmt.Errorf("engine: closed")
	}
	if e.wtxn == nil {
		return ErrNoWriteTxn
	}
	if err := e.prepareFreelistCommit(); err != nil {
		return err
	}
	txn := e.wtxn
	slices.SortFunc(txn.pending, func(a, b pendingRedo) int { return cmp.Compare(a.seq, b.seq) })
	e.wtxn = nil
	return e.commitPages(txn.hdr, txn.pending)
}

// WALStats reports commit attempts and successful redo syncs.
func (e *Engine) WALStats() (commitAttempts, redoSyncs uint64) {
	return e.walCommits.Load(), e.walCommitSyncs.Load()
}

// RecoveryRequired reports whether a failed engine commit fences this handle.
// The database owner serializes access through its transaction lock.
func (e *Engine) RecoveryRequired() bool { return e.commitFailed }

func (e *Engine) commitPages(final Header, pending []pendingRedo) (err error) {
	if e.commitFailed {
		return ErrRecoveryRequired
	}
	recordSize := int64(walRecordSize(e.physicalPageSize))
	if e.walMACEnabled {
		recordSize += walRecordMACSize
	}
	if int64(len(pending)) >= maxWALReplayBytes/recordSize {
		return ErrWALTooLarge
	}
	defer func() {
		if err != nil {
			e.commitFailed = true
		}
	}()
	if len(pending) == 0 {
		final.LastWALSeq = e.nextWALSeq.Add(1)
	} else {
		final.LastWALSeq = pending[len(pending)-1].seq
	}
	final.Features |= FeatureWALFrameDigest
	if err := validateHeaderIntegrity(encodeHeaderPage(final), final); err != nil {
		return err
	}
	e.walCommits.Add(1)
	if err := e.writeCommitWAL(final, pending); err != nil {
		return err
	}
	crashtest.At("commit_wal_appended")
	if err := e.wal.Sync(); err != nil {
		return err
	}
	e.walCommitSyncs.Add(1)
	crashtest.At("commit_wal_synced")
	headerOnlyAuthenticated := len(pending) == 0 && e.headerMACEnabled
	if headerOnlyAuthenticated {
		crashtest.At("authenticated_header_only_wal_synced")
	}
	for _, page := range pending {
		if err := e.writePrimaryData(page.pageID, page.plain); err != nil {
			return err
		}
	}
	crashtest.At("commit_primary_written")
	if len(pending) > 0 {
		if err := e.syncPrimary(); err != nil {
			return err
		}
	}
	crashtest.At("commit_primary_synced")
	if err := e.writeHeader(final); err != nil {
		return err
	}
	crashtest.At("commit_header_written")
	if headerOnlyAuthenticated {
		crashtest.At("authenticated_header_only_header_written")
	}
	if err := e.syncPrimary(); err != nil {
		return err
	}
	e.lastSeq = final.LastWALSeq
	e.embeddingDim, e.embeddingMetric = final.EmbeddingDim, final.EmbeddingMetric
	if len(pending) > 0 {
		last := pending[len(pending)-1]
		e.lastPageID, e.lastPageGeneration = last.pageID, last.seq
	}
	// Trim the previous commit's unused extent after the new header is durable.
	written, err := e.wal.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	extent, err := e.wal.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if extent == written {
		// This exact extent was synced before primary publication.
		return nil
	}
	if extent < written {
		return ErrCorruptWAL
	}
	if err := e.wal.Truncate(written); err != nil {
		return err
	}
	return e.wal.Sync()
}

func (e *Engine) writeCommitWAL(final Header, pending []pendingRedo) error {
	if _, err := e.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	marker := e.encodeFramedHeaderWALRecord(final, pending)
	// Legacy readers reject a pending page-zero marker. Put it first for v2
	// so those readers cannot apply new-format pages before detecting it.
	if !e.headerMACEnabled {
		if _, err := e.wal.Write(marker); err != nil {
			return err
		}
	}
	for _, page := range pending {
		rec := encodeWALRecordWithMAC(page.seq, page.pageID, page.plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
		if _, err := e.wal.Write(rec); err != nil {
			return err
		}
	}
	if e.headerMACEnabled {
		_, err := e.wal.Write(marker)
		return err
	}
	return nil
}

// AbortWriteTxn discards a write transaction without writing the WAL. Buffered state is dropped.
func (e *Engine) AbortWriteTxn() {
	if e == nil {
		return
	}
	e.wtxn = nil
}

func (e *Engine) writePrimaryData(pageID uint64, plain []byte) error {
	if len(plain) != e.physicalPageSize {
		return ErrBadPageSize
	}
	off, err := pageByteOffset(pageID, e.pageSize, e.physicalPageSize)
	if err != nil {
		return err
	}
	// Invalidate before writing so no concurrent reader can see stale cached bytes.
	if e.cache != nil {
		e.cache.invalidate(pageID)
	}
	_, err = e.db.WriteAt(plain, off)
	return err
}
