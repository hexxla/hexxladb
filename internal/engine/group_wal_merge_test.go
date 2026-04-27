package engine

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestGroupWAL_twoJobsOneBatch proves the flusher merged two logical commits: [GroupWALStats]
// reports a batch with 2+ jobs and a single WAL sync for that barrier.
func TestGroupWAL_twoJobsOneBatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "merge.db")
	e, err := Open(path, &Options{
		GroupWAL: GroupWAL{Enabled: true, MaxBatchWait: 500 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()

	page := bytes.Repeat([]byte{0x55}, DefaultPageSize)

	// G1 enqueues J1; before G1 calls wait(), G2 must Begin+enqueue J2 so the flusher collects both.
	ready := make(chan struct{})
	g2Enqueued := make(chan struct{})
	var w1 func() error
	var err1 error

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := e.BeginWriteTxn(); err != nil {
			err1 = err
			return
		}
		if err := e.WritePage(1, page); err != nil {
			err1 = err
			return
		}
		w1, err1 = e.CommitWriteTxnBeginAsync()
		if err1 != nil {
			return
		}
		close(ready)
		<-g2Enqueued
		err1 = w1()
	})

	<-ready
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := e.WritePage(2, page); err != nil {
		t.Fatal(err)
	}
	w2, err := e.CommitWriteTxnBeginAsync()
	if err != nil {
		t.Fatal(err)
	}
	close(g2Enqueued)
	if err := w2(); err != nil {
		t.Fatal(err)
	}

	wg.Wait()
	if err1 != nil {
		t.Fatalf("first wait: %v", err1)
	}
	apply, multi, walS := e.GroupWALStats()
	// One [applyGroupBatch] with two jobs => one [wal.Sync] after the grouped WAL writes.
	if apply != 1 || multi != 1 || walS != 1 {
		t.Fatalf("GroupWALStats: applyBatches=%d batchesWith2+=%d walSynces=%d (want 1,1,1)", apply, multi, walS)
	}
}
