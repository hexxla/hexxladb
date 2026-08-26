package engine

import (
	"fmt"
	"io"
	"time"

	"github.com/hexxla/hexxladb/internal/engine/crashtest"
)

type groupJob struct {
	pending []pendingRedo
	hdr     Header
	done    chan error
}

// GroupWALEnabled reports whether the engine was opened with [Options.GroupWAL.Enabled].
func (e *Engine) GroupWALEnabled() bool { return e.groupWALEnabled() }

// GroupWALStats returns counters for the group-WAL flusher: total [applyGroupBatch] invocations,
// how many of those processed two or more logical commits in one barrier, and how many
// [wal.Sync] calls the flusher performed (one per applied batch that wrote WAL records).
func (e *Engine) GroupWALStats() (applyBatches, batchesWith2PlusJobs, walSynces uint64) {
	if e == nil {
		return 0, 0, 0
	}
	return e.groupWALStatsApplyBatches.Load(),
		e.groupWALStatsBatchesWith2OrMoreJobs.Load(),
		e.groupWALStatsWalSynces.Load()
}

func (e *Engine) groupWALEnabled() bool {
	return e.groupWALCfg.Enabled && e.groupJobCh != nil
}

func (e *Engine) setGroupUnflushed(h Header) {
	e.groupUnflushedMu.Lock()
	e.groupUnflushed = h
	e.groupUnflushedSet = true
	e.groupUnflushedMu.Unlock()
}

func (e *Engine) clearGroupUnflushed() {
	e.groupUnflushedMu.Lock()
	e.groupUnflushedSet = false
	e.groupUnflushed = Header{}
	e.groupUnflushedMu.Unlock()
}

func (e *Engine) startGroupWALFlusher() {
	if !e.groupWALCfg.Enabled {
		return
	}
	maxWait := max(time.Duration(0), e.groupWALCfg.MaxBatchWait)
	e.groupWALCfg = GroupWAL{Enabled: true, MaxBatchWait: maxWait}
	e.groupJobCh = make(chan *groupJob, 256)
	e.groupStop = make(chan struct{})
	e.groupOverlay = make(map[uint64][]byte)
	e.groupFlusherWG.Add(1)
	go e.runGroupWALFlusher()
}

func (e *Engine) stopGroupWALFlusher() {
	if e.groupJobCh == nil {
		return
	}
	close(e.groupStop)
	e.groupFlusherWG.Wait()
	// Idempotent [Engine.Close]: tests or error paths may close the engine twice.
	e.groupJobCh = nil
	e.groupStop = nil
}

func (e *Engine) runGroupWALFlusher() {
	defer e.groupFlusherWG.Done()
	maxWait := e.groupWALCfg.MaxBatchWait
	for {
		select {
		case <-e.groupStop:
			e.drainGroupJobChNoWait()
			return
		case j, ok := <-e.groupJobCh:
			if !ok {
				return
			}
			batch := []*groupJob{j}
			if maxWait == 0 {
			collectReady:
				for {
					select {
					case <-e.groupStop:
						e.applyGroupBatch(batch)
						e.drainGroupJobChNoWait()
						return
					case j2, ok := <-e.groupJobCh:
						if !ok {
							e.applyGroupBatch(batch)
							return
						}
						batch = append(batch, j2)
					default:
						break collectReady
					}
				}
				e.applyGroupBatch(batch)
				continue
			}
			t := time.NewTimer(maxWait)
		collect:
			for {
				select {
				case <-e.groupStop:
					t.Stop()
					e.applyGroupBatch(batch)
					e.drainGroupJobChNoWait()
					return
				case j2, ok := <-e.groupJobCh:
					if !ok {
						t.Stop()
						e.applyGroupBatch(batch)
						return
					}
					batch = append(batch, j2)
				case <-t.C:
					break collect
				}
			}
			t.Stop()
			e.applyGroupBatch(batch)
		}
	}
}

// drainGroupJobChNoWait flushes all jobs already buffered in the channel (no blocking for new work).
func (e *Engine) drainGroupJobChNoWait() {
	for {
		select {
		case j, ok := <-e.groupJobCh:
			if !ok {
				return
			}
			e.applyGroupBatch([]*groupJob{j})
		default:
			return
		}
	}
}

func (e *Engine) applyGroupBatch(jobs []*groupJob) {
	if len(jobs) == 0 {
		return
	}
	e.groupWALStatsApplyBatches.Add(1)
	if len(jobs) >= 2 {
		e.groupWALStatsBatchesWith2OrMoreJobs.Add(1)
	}

	if err := e.applyGroupBatchPipeline(jobs); err != nil {
		// Broadcast error to all waiters and revert overlay state.
		for _, j := range jobs {
			j.done <- err
		}
		for _, j := range jobs {
			for i := range j.pending {
				e.removeGroupOverlay(j.pending[i].pageID)
			}
		}
		e.clearGroupUnflushed()
		return
	}

	for _, j := range jobs {
		j.done <- nil
	}
}

// applyGroupBatchPipeline executes the WAL-write → primary-write → header-update → truncate
// sequence. Returns nil on success or the first error encountered.
func (e *Engine) applyGroupBatchPipeline(jobs []*groupJob) error {
	if err := e.groupWriteWAL(jobs); err != nil {
		return err
	}
	crashtest.At("group_wal_appended")
	if err := e.wal.Sync(); err != nil {
		return err
	}
	e.groupWALStatsWalSynces.Add(1)
	crashtest.At("group_wal_synced")

	if err := e.groupWritePrimary(jobs); err != nil {
		return err
	}
	crashtest.At("group_primary_written")
	if err := e.syncPrimary(); err != nil {
		return err
	}
	crashtest.At("group_primary_synced")

	if err := e.groupFinalizeHeader(jobs); err != nil {
		return err
	}

	return e.groupTruncateWAL()
}

// groupWriteWAL appends WAL records for all jobs.
func (e *Engine) groupWriteWAL(jobs []*groupJob) error {
	for _, job := range jobs {
		for i := range job.pending {
			p := &job.pending[i]
			rec := encodeWALRecordWithMAC(p.seq, p.pageID, p.plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
			n, err := e.wal.Write(rec)
			e.walSize += int64(n)
			if err != nil {
				return err
			}
		}
	}
	if e.headerMACEnabled {
		final := groupFinalHeader(jobs)
		rec := e.encodeHeaderWALRecord(final)
		n, err := e.wal.Write(rec)
		e.walSize += int64(n)
		if err != nil {
			return err
		}
	}
	return nil
}

// groupWritePrimary applies pending pages to the primary file and removes overlay entries.
func (e *Engine) groupWritePrimary(jobs []*groupJob) error {
	for _, job := range jobs {
		for i := range job.pending {
			p := &job.pending[i]
			if err := e.writePrimaryData(p.pageID, p.plain); err != nil {
				return err
			}
			e.removeGroupOverlay(p.pageID)
		}
	}
	return nil
}

// groupFinalizeHeader writes the final header with updated LastWALSeq, syncs, and clears staging.
func (e *Engine) groupFinalizeHeader(jobs []*groupJob) error {
	final := groupFinalHeader(jobs)
	if err := e.writeHeader(final); err != nil {
		return err
	}
	crashtest.At("group_header_written")
	if err := e.syncPrimary(); err != nil {
		return err
	}
	e.lastSeq = final.LastWALSeq
	e.clearGroupUnflushed()
	return nil
}

func groupFinalHeader(jobs []*groupJob) Header {
	var lastRedoSeq uint64
	for _, job := range jobs {
		for i := range job.pending {
			if job.pending[i].seq > lastRedoSeq {
				lastRedoSeq = job.pending[i].seq
			}
		}
	}
	final := jobs[len(jobs)-1].hdr
	final.LastWALSeq = lastRedoSeq
	return final
}

// groupTruncateWAL resets the WAL after the primary is durable.
// It shrinks to the exact bytes written this cycle (not zero) so the kernel
// retains allocated inode blocks, avoiding fallocate on the next commit.
func (e *Engine) groupTruncateWAL() error {
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

// commitWriteTxnGrouped finishes a write transaction via the group-WAL pipeline. [Engine.wtxn] is
// already cleared. Sets overlay and staging before enqueue, then blocks until the flusher has
// applied the batch that includes this job.
func (e *Engine) commitWriteTxnGrouped(txn *writeTxnState) error {
	wait, err := e.enqueueGroupWALJob(txn)
	if err != nil {
		return err
	}
	if wait == nil {
		return nil
	}
	return wait()
}

// enqueueGroupWALJob installs overlay + staging, enqueues a flusher job, and returns a function
// that blocks until that job is durable. For an empty [writeTxnState.pending] it performs the
// header-only path synchronously and returns (nil, nil).
func (e *Engine) enqueueGroupWALJob(txn *writeTxnState) (wait func() error, err error) {
	if len(txn.pending) == 0 {
		if e.headerMACEnabled {
			return nil, e.commitAuthenticatedHeaderOnly(txn.hdr)
		}
		if err := e.writeHeader(txn.hdr); err != nil {
			return nil, err
		}
		if err := e.syncPrimary(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	txn.hdr.LastWALSeq = txn.pending[len(txn.pending)-1].seq

	for i := range txn.pending {
		p := &txn.pending[i]
		cp := make([]byte, len(p.plain))
		copy(cp, p.plain)
		e.groupOverlayMu.Lock()
		if e.groupOverlay == nil {
			e.groupOverlay = make(map[uint64][]byte)
		}
		e.groupOverlay[p.pageID] = cp
		e.groupOverlayMu.Unlock()
	}
	e.setGroupUnflushed(txn.hdr)

	j := &groupJob{
		pending: txn.pending,
		hdr:     txn.hdr,
		done:    make(chan error, 1),
	}
	select {
	case e.groupJobCh <- j:
		return func() error { return <-j.done }, nil
	case <-e.groupStop:
		e.clearGroupUnflushed()
		for i := range txn.pending {
			e.removeGroupOverlay(txn.pending[i].pageID)
		}
		return nil, fmt.Errorf("engine: closed during group commit")
	}
}

func (e *Engine) removeGroupOverlay(pageID uint64) {
	e.groupOverlayMu.Lock()
	if e.groupOverlay != nil {
		delete(e.groupOverlay, pageID)
	}
	e.groupOverlayMu.Unlock()
}
