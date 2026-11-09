package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

func TestTreeMonotonicInsertHeight(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			e := openTestDB(t, nil)
			tree := OpenBTree(e)
			if err := e.BeginWriteTxn(); err != nil {
				t.Fatal(err)
			}
			defer e.AbortWriteTxn()
			const count = 512
			for i := range count {
				value := i
				if reverse {
					value = count - i - 1
				}
				key := make([]byte, maxKeyBytes)
				binary.BigEndian.PutUint64(key, uint64(value))
				if err := tree.Put(key, []byte("value")); err != nil {
					t.Fatal(err)
				}
			}
			if got := validateTree(t, tree); got != count {
				t.Fatalf("entry count %d", got)
			}
			h, err := e.ReadHeader()
			if err != nil {
				t.Fatal(err)
			}
			pid, depth := h.BTreeRoot, 0
			for {
				depth++
				page, err := e.ReadPage(pid)
				if err != nil {
					t.Fatal(err)
				}
				if page[5] == btreeKindLeaf {
					break
				}
				if depth > 4 {
					t.Fatalf("tree has more than 4 levels for %d entries", count)
				}
				pid = binary.BigEndian.Uint64(page[btreeHeaderSize:])
			}
			if h.NextPageID > 100 {
				t.Fatalf("allocated %d pages for %d entries", h.NextPageID, count)
			}
			t.Logf("entries=%d depth=%d data_pages=%d staged_images=%d", count, depth, h.NextPageID-1, len(e.wtxn.pending))
		})
	}
}

func TestTreeRejectsChildAndLeafCycles(t *testing.T) {
	t.Run("internal", func(t *testing.T) {
		e := openTestDB(t, nil)
		if err := e.BeginWriteTxn(); err != nil {
			t.Fatal(err)
		}
		defer e.AbortWriteTxn()
		page, err := buildInternalPage(e.pageSize, 0, []uint64{1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.WritePage(1, page); err != nil {
			t.Fatal(err)
		}
		tree := OpenBTree(e)
		if _, _, err := tree.GetUsingRoot(1, []byte("a")); !errors.Is(err, ErrCorruptTree) {
			t.Fatalf("lookup accepted cycle: %v", err)
		}
		if _, err := tree.seekLeaf(1, nil, false); !errors.Is(err, ErrCorruptTree) {
			t.Fatalf("seek accepted cycle: %v", err)
		}
		if _, _, _, _, err := tree.findLeafWithKey(1, []byte("a")); !errors.Is(err, ErrCorruptTree) {
			t.Fatalf("delete lookup accepted cycle: %v", err)
		}
	})
	t.Run("leaf", func(t *testing.T) {
		e := openTestDB(t, nil)
		if err := e.BeginWriteTxn(); err != nil {
			t.Fatal(err)
		}
		defer e.AbortWriteTxn()
		tree := OpenBTree(e)
		if err := tree.Put([]byte("a"), []byte("value")); err != nil {
			t.Fatal(err)
		}
		h, err := e.ReadHeader()
		if err != nil {
			t.Fatal(err)
		}
		page, err := e.ReadPage(h.BTreeRoot)
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint64(page[8:16], h.BTreeRoot)
		if err := e.WritePage(h.BTreeRoot, page); err != nil {
			t.Fatal(err)
		}
		calls := 0
		err = tree.AscendRange(nil, nil, func(_, _ []byte) bool {
			calls++
			return calls < 3
		})
		if !errors.Is(err, ErrCorruptTree) || calls != 1 {
			t.Fatalf("cyclic scan calls=%d, err=%v", calls, err)
		}
	})
}

func TestOverflowRejectsInvalidLengthAndChain(t *testing.T) {
	e := openTestDB(t, nil)
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	defer e.AbortWriteTxn()
	tree := OpenBTree(e)
	for _, length := range []uint32{0, ^uint32(0)} {
		if _, err := tree.readOverflowChain(1, length); !errors.Is(err, ErrCorruptTree) {
			t.Fatalf("length %d accepted: %v", length, err)
		}
	}
	page := make([]byte, e.pageSize)
	binary.BigEndian.PutUint64(page, 1)
	if err := e.WritePage(1, page); err != nil {
		t.Fatal(err)
	}
	if _, err := tree.readOverflowChain(1, 1); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("excess overflow pages accepted: %v", err)
	}
	if err := tree.freeOverflowChain(1, 1); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("cyclic overflow free accepted: %v", err)
	}
}

func TestReverseScanRejectsMalformedParent(t *testing.T) {
	e := openTestDB(t, nil)
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	defer e.AbortWriteTxn()
	tree := OpenBTree(e)
	for i := range 20 {
		key := bytes.Repeat([]byte{byte(i)}, maxKeyBytes)
		if err := tree.Put(key, []byte("value")); err != nil {
			t.Fatal(err)
		}
	}
	h, err := e.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := tree.seekLeaf(h.BTreeRoot, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.ReadPage(pid)
	if err != nil {
		t.Fatal(err)
	}
	binary.BigEndian.PutUint64(page[16:24], pid)
	if err := e.WritePage(pid, page); err != nil {
		t.Fatal(err)
	}
	if err := tree.DescendRange(nil, nil, func(_, _ []byte) bool { return true }); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("malformed reverse parent accepted: %v", err)
	}
}

func TestCompressedLengthUsesSupportedReadLimit(t *testing.T) {
	e := openTestDB(t, nil)
	value := []byte{compressMagic, 255, 255, 255, 255, 0}
	if _, err := e.decompressValue(value); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("oversized decompression accepted: %v", err)
	}
	e.maxValueBytes = 512
	want := bytes.Repeat([]byte("value"), 2000)
	got, err := e.decompressValue(compressValue(want))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("lowered write limit prevented read: %v", err)
	}
}

func TestFreelistRejectsLiveRoot(t *testing.T) {
	e := openTestDB(t, authenticatedRecoveryTestOptions())
	tree := OpenBTree(e)
	commitTreeMutation(t, e, func() error { return tree.Put([]byte("key"), []byte("value")) })
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	defer e.AbortWriteTxn()
	root := e.wtxn.hdr.BTreeRoot
	if err := e.wtxn.addLoadedFreePage(root); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("root accepted as free: %v", err)
	}
	e.wtxn.hdr.FreelistCount = 1
	e.wtxn.hdr.InlineFreelist[0] = root
	if _, err := tree.StorageStats(); !errors.Is(err, ErrCorruptTree) {
		t.Fatalf("diagnostic accepted live/free overlap: %v", err)
	}
}

func TestRawStorageMarkersRoundTrip(t *testing.T) {
	e := openTestDB(t, nil)
	tree := OpenBTree(e)
	for _, value := range [][]byte{
		encodeOverflowStub(1, 0),
		append([]byte{compressMagic}, bytes.Repeat([]byte{255}, 20)...),
	} {
		if err := tree.Put([]byte("raw"), value); err != nil {
			t.Fatal(err)
		}
		got, ok, err := tree.Get([]byte("raw"))
		if err != nil || !ok || !bytes.Equal(got, value) {
			t.Fatalf("raw marker round trip = %x, %t, %v", got, ok, err)
		}
		if err := tree.Delete([]byte("raw")); err != nil {
			t.Fatal(err)
		}
	}
}
