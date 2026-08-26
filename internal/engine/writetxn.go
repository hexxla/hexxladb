package engine

import (
	"fmt"
	"io"

	"github.com/hexxla/hexxladb/internal/engine/crashtest"
)

// writeTxnState holds in-memory state for a single engine write transaction (one
// [DB.Update] callback). Redo is buffered in memory until [Engine.CommitWriteTxn].
type writeTxnState struct {
	hdr     Header
	pending []pendingRedo
	// dirty holds on-disk form payload per key (pageID >= 1), same as WAL/plain from transformWrite.
	dirty           map[uint64][]byte
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
	if e.wtxn != nil {
		return ErrWriteTxnActive
	}
	hdr, err := e.visibleHeader()
	if err != nil {
		return err
	}
	e.wtxn = &writeTxnState{
		hdr:   hdr,
		dirty: make(map[uint64][]byte),
	}
	if err := e.loadWriteTxnFreelist(); err != nil {
		e.wtxn = nil
		return err
	}
	return nil
}

// CommitWriteTxnBeginAsync is only valid when [Options.GroupWAL] is enabled. It clears
// [Engine.wtxn], enqueues a group-WAL flusher job, and returns a function that blocks until
// that job is applied (the same durability as [CommitWriteTxn] for the group path). The caller
// may release outer mutexes after this returns, then call the returned [wait] to finish.
// If no redo was buffered, the returned [wait] is a no-op.
func (e *Engine) CommitWriteTxnBeginAsync() (wait func() error, err error) {
	if e == nil || e.db == nil {
		return nil, fmt.Errorf("engine: closed")
	}
	if e.wtxn == nil {
		return nil, ErrNoWriteTxn
	}
	if !e.groupWALEnabled() {
		return nil, fmt.Errorf("engine: CommitWriteTxnBeginAsync requires Group WAL")
	}
	if err := e.prepareFreelistCommit(); err != nil {
		return nil, err
	}
	txn := e.wtxn
	e.wtxn = nil
	waitFn, eErr := e.enqueueGroupWALJob(txn)
	if eErr != nil {
		e.wtxn = txn
		return nil, eErr
	}
	if waitFn == nil {
		return func() error { return nil }, nil
	}
	return waitFn, nil
}

// CommitWriteTxn flushes the buffered redo: WAL write + a single [wal.Sync], then
// writes each data page to the primary, one [os.File.Sync] for those pages, then
// [writeHeaderAt] and a final [os.File.Sync] on the primary. It clears the write transaction.
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
	e.wtxn = nil

	if e.groupWALEnabled() {
		return e.commitWriteTxnGrouped(txn)
	}

	if len(txn.pending) == 0 {
		if e.headerMACEnabled {
			return e.commitAuthenticatedHeaderOnly(txn.hdr)
		}
		if err := e.writeHeader(txn.hdr); err != nil {
			return err
		}
		if err := e.syncPrimary(); err != nil {
			return err
		}
		return nil
	}
	final := txn.hdr
	final.LastWALSeq = txn.pending[len(txn.pending)-1].seq

	for i := range txn.pending {
		p := &txn.pending[i]
		rec := encodeWALRecordWithMAC(p.seq, p.pageID, p.plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
		n, err := e.wal.Write(rec)
		e.walSize += int64(n)
		if err != nil {
			return err
		}
	}
	if e.headerMACEnabled {
		rec := e.encodeHeaderWALRecord(final)
		n, err := e.wal.Write(rec)
		e.walSize += int64(n)
		if err != nil {
			return err
		}
	}
	crashtest.At("classic_wal_appended")
	if err := e.wal.Sync(); err != nil {
		return err
	}
	crashtest.At("classic_wal_synced")
	for i := range txn.pending {
		p := &txn.pending[i]
		if err := e.writePrimaryData(p.pageID, p.plain); err != nil {
			return err
		}
	}
	crashtest.At("classic_primary_written")
	if err := e.syncPrimary(); err != nil {
		return err
	}
	crashtest.At("classic_primary_synced")

	if err := e.writeHeader(final); err != nil {
		return err
	}
	crashtest.At("classic_header_written")
	if err := e.syncPrimary(); err != nil {
		return err
	}
	e.lastSeq = final.LastWALSeq

	// Shrink WAL to the bytes written this cycle (not zero) so the kernel retains
	// the allocated inode blocks — avoids fallocate on the next commit.
	// walSize is reset to 0 so the next commit overwrites from position 0.
	written := e.walSize
	e.walSize = 0
	if err := e.wal.Truncate(written); err != nil {
		return err
	}
	if _, err := e.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return e.wal.Sync()
}

// commitAuthenticatedHeaderOnly protects allocator and other header-only
// transitions with the same WAL marker protocol used by page-bearing commits.
func (e *Engine) commitAuthenticatedHeaderOnly(final Header) error {
	seq := e.nextWALSeq.Add(1)
	final.LastWALSeq = seq
	record := e.encodeHeaderWALRecord(final)
	n, err := e.wal.Write(record)
	e.walSize += int64(n)
	if err != nil {
		return err
	}
	if err := e.wal.Sync(); err != nil {
		return err
	}
	crashtest.At("authenticated_header_only_wal_synced")
	if err := e.writeHeader(final); err != nil {
		return err
	}
	crashtest.At("authenticated_header_only_header_written")
	if err := e.syncPrimary(); err != nil {
		return err
	}
	e.lastSeq = seq
	written := e.walSize
	e.walSize = 0
	if err := e.wal.Truncate(written); err != nil {
		return err
	}
	if _, err := e.wal.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return e.wal.Sync()
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
