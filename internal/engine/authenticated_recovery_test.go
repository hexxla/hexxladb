package engine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAuthenticatedWALRecoversPublishedRootAndPages(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "authenticated-recovery.db")
	opts := authenticatedRecoveryTestOptions()
	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	tree := OpenBTree(e)
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	for i := range 300 {
		key := fmt.Appendf(nil, "key/%04d", i)
		value := bytes.Repeat([]byte{byte(i)}, 80)
		if err := tree.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if e.wtxn.hdr.BTreeRoot == 0 || len(e.wtxn.pending) == 0 {
		t.Fatal("transaction did not stage a root and redo pages")
	}
	final := e.wtxn.hdr
	final.LastWALSeq = e.wtxn.pending[len(e.wtxn.pending)-1].seq
	for i := range e.wtxn.pending {
		page := &e.wtxn.pending[i]
		record := encodeWALRecordWithMAC(
			page.seq,
			page.pageID,
			page.plain,
			e.walMACKey,
			e.walMACEnabled,
			e.physicalPageSize,
		)
		if _, err := e.wal.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.wal.Write(e.encodeHeaderWALRecord(final)); err != nil {
		t.Fatal(err)
	}
	if err := e.wal.Sync(); err != nil {
		t.Fatal(err)
	}
	// Simulate process death after the WAL barrier and before primary-page or
	// header publication. Close only releases descriptors; the staged txn is not committed.
	e.wtxn = nil
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = recovered.Close() }()
	hdr, err := recovered.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.BTreeRoot != final.BTreeRoot || hdr.BTreeRootGeneration != final.BTreeRootGeneration || hdr.LastWALSeq != final.LastWALSeq {
		t.Fatalf("recovered header = %+v, want root/gen/seq %d/%d/%d", hdr, final.BTreeRoot, final.BTreeRootGeneration, final.LastWALSeq)
	}
	value, ok, err := OpenBTree(recovered).Get([]byte("key/0299"))
	if err != nil || !ok || !bytes.Equal(value, bytes.Repeat([]byte{byte(299 % 256)}, 80)) {
		t.Fatalf("recovered value ok=%v err=%v value=%x", ok, err, value)
	}
}

func TestAuthenticatedWALPageIDTamperFailsClosed(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "authenticated-wal-tamper.db")
	opts := authenticatedRecoveryTestOptions()
	e, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := OpenBTree(e).Put([]byte("key"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	final := e.wtxn.hdr
	final.LastWALSeq = e.wtxn.pending[len(e.wtxn.pending)-1].seq
	page := e.wtxn.pending[0]
	if _, err := e.wal.Write(encodeWALRecordWithMAC(
		page.seq,
		page.pageID,
		page.plain,
		e.walMACKey,
		true,
		e.physicalPageSize,
	)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.wal.Write(e.encodeHeaderWALRecord(final)); err != nil {
		t.Fatal(err)
	}
	if err := e.wal.Sync(); err != nil {
		t.Fatal(err)
	}
	e.wtxn = nil
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	wal, err := os.OpenFile(WalPath(path), os.O_RDWR, 0) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	var encodedPageID [8]byte
	binary.BigEndian.PutUint64(encodedPageID[:], page.pageID+1)
	if _, err := wal.WriteAt(encodedPageID[:], 8); err != nil {
		_ = wal.Close()
		t.Fatal(err)
	}
	if err := wal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, opts); !errors.Is(err, ErrCorruptWAL) {
		t.Fatalf("open error = %v, want ErrCorruptWAL", err)
	}
}

func authenticatedRecoveryTestOptions() *Options {
	var walKey [32]byte
	walKey[0] = 1
	var headerKey [32]byte
	headerKey[0] = 2
	return &Options{
		Hooks: &PageHooks{
			PhysicalPageOverhead: AuthenticatedPageOverhead,
			BeforeWriteVersioned: func(_ uint64, generation uint64, plain []byte) ([]byte, error) {
				physical := make([]byte, len(plain)+AuthenticatedPageOverhead)
				binary.BigEndian.PutUint64(physical[:8], generation)
				copy(physical[AuthenticatedPageOverhead:], plain)
				return physical, nil
			},
			AfterRead: func(_ uint64, physical []byte) ([]byte, error) {
				if len(physical) < AuthenticatedPageOverhead {
					return nil, ErrBadPageSize
				}
				return bytes.Clone(physical[AuthenticatedPageOverhead:]), nil
			},
		},
		UseFormatV3:        true,
		NewEncryptedDB:     true,
		NewAuthenticatedDB: true,
		WALMACKey:          walKey,
		EnableWALMAC:       true,
		HeaderMACKey:       headerKey,
		EnableHeaderMAC:    true,
	}
}
