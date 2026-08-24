package engine

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestBTreeStorageStatsTracksReachableAndReclaimablePages(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "storage-stats.db")
	eng, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	bt := OpenBTree(eng)

	empty, err := bt.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if empty.AllocatedPages != 1 || empty.ReachablePages != 1 || empty.ReclaimableBytes != 0 {
		t.Fatalf("empty database stats: %#v", empty)
	}

	value := incompressibleBytes(6000)
	if err := bt.Put([]byte("large"), value); err != nil {
		t.Fatal(err)
	}
	populated, err := bt.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if populated.ReachablePages <= empty.ReachablePages || populated.ReclaimableBytes != 0 {
		t.Fatalf("populated database stats: %#v", populated)
	}

	if err := bt.Put([]byte("large"), bytes.Repeat([]byte{0xA5}, len(value))); err != nil {
		t.Fatal(err)
	}
	rewritten, err := bt.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.ReclaimableBytes == 0 || rewritten.ReachablePages >= populated.ReachablePages {
		t.Fatalf("rewritten database stats: before=%#v after=%#v", populated, rewritten)
	}

	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	eng, err = Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	bt = OpenBTree(eng)
	afterReopen, err := bt.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if afterReopen.ReclaimableBytes != rewritten.ReclaimableBytes || afterReopen.ReachablePages != rewritten.ReachablePages {
		t.Fatalf("persistent accounting changed across reopen: before=%#v after=%#v", rewritten, afterReopen)
	}
}
