package engine

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// stagedFramedWAL leaves a complete, synced pending commit and the old primary.
func stagedFramedWAL(t *testing.T, authenticated bool) (*Engine, *Options, Header, []pendingRedo) {
	t.Helper()
	var opts *Options
	if authenticated {
		opts = authenticatedRecoveryTestOptions()
	}
	e, err := Open(filepath.Join(t.TempDir(), "database"), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := OpenBTree(e).Put([]byte("key"), []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateHeader(func(h *Header) { h.CommitSeq = 1 }); err != nil {
		t.Fatal(err)
	}
	if err := e.CommitWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := e.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	if err := OpenBTree(e).Put([]byte("key"), []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := e.WritePage(2, bytes.Repeat([]byte{0x55}, e.pageSize)); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(e.wtxn.pending, func(a, b pendingRedo) int { return cmp.Compare(a.seq, b.seq) })
	pending := e.wtxn.pending
	final := e.wtxn.hdr
	final.CommitSeq, final.LastWALSeq = 2, pending[len(pending)-1].seq
	final.Features |= FeatureWALFrameDigest
	if err := e.writeCommitWAL(final, pending); err != nil {
		t.Fatal(err)
	}
	if err := e.wal.Sync(); err != nil {
		t.Fatal(err)
	}
	return e, opts, final, pending
}

func TestFramedWALRepairsTornHeader(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, mode := range []string{"watermark", "watermark-beyond-wal", "zero-allocator", "root-outside-allocator", "first-upgrade"} {
			name := "v2/" + mode
			if authenticated {
				name = "v3/" + mode
			}
			t.Run(name, func(t *testing.T) {
				e, opts, final, pending := stagedFramedWAL(t, authenticated)
				path := e.path
				old, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				hdr, err := decodeHeaderPage(old)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "first-upgrade" {
					hdr.Features &^= FeatureWALFrameDigest
					if authenticated {
						hdr = authenticateHeader(hdr, e.headerMACKey)
					}
					old = encodeHeaderPage(hdr)
				}
				next := final
				if authenticated {
					next = authenticateHeader(next, e.headerMACKey)
				}
				encoded := encodeHeaderPage(next)
				switch mode {
				case "watermark", "first-upgrade":
					copy(old[:24], encoded[:24])
				case "watermark-beyond-wal":
					binary.BigEndian.PutUint64(old[16:24], final.LastWALSeq+1234)
				case "zero-allocator":
					clear(old[24:32])
				case "root-outside-allocator":
					binary.BigEndian.PutUint64(old[32:40], final.NextPageID)
				}
				for _, p := range pending {
					if err := e.writePrimaryData(p.pageID, p.plain); err != nil {
						t.Fatal(err)
					}
				}
				if err := e.syncPrimary(); err != nil {
					t.Fatal(err)
				}
				if _, err := e.db.WriteAt(old[:e.pageSize], 0); err != nil {
					t.Fatal(err)
				}
				if err := e.syncPrimary(); err != nil {
					t.Fatal(err)
				}
				if err := e.Close(); err != nil {
					t.Fatal(err)
				}
				// The same bootstrap used by public encrypted Open must accept the
				// immutable identity while leaving mutable validation to engine recovery.
				identity, err := ReadHeaderIdentityFile(path)
				if err != nil || identity.EncryptionSalt != final.EncryptionSalt {
					t.Fatalf("identity: %+v, %v", identity, err)
				}
				recovered, err := Open(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = recovered.Close() }()
				actual, err := recovered.ReadHeader()
				if err != nil || actual.CommitSeq != 2 || actual.LastWALSeq != final.LastWALSeq {
					t.Fatalf("header: %+v, %v", actual, err)
				}
				value, ok, err := OpenBTree(recovered).Get([]byte("key"))
				if err != nil || !ok || string(value) != "new" {
					t.Fatalf("value: %q, %v, %v", value, ok, err)
				}
				if err := recovered.BeginWriteTxn(); err != nil {
					t.Fatal(err)
				}
				if err := OpenBTree(recovered).Put([]byte("next"), []byte("ok")); err != nil {
					t.Fatal(err)
				}
				if err := recovered.CommitWriteTxn(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestFramedWALRejectsIncompleteOrMisroutedCommit(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, mode := range []string{"delete-page", "marker-only", "routing", "sequence", "descriptor", "identity", "zero-sequence"} {
			name := "v2/" + mode
			if authenticated {
				name = "v3/" + mode
			}
			t.Run(name, func(t *testing.T) {
				e, opts, final, pending := stagedFramedWAL(t, authenticated)
				path := e.path
				recSize := walRecordSize(e.physicalPageSize)
				if e.walMACEnabled {
					recSize += walRecordMACSize
				}
				data, err := os.ReadFile(WalPath(path))
				if err != nil {
					t.Fatal(err)
				}
				markerAt, pageAt := 0, recSize
				if authenticated {
					markerAt = len(pending) * recSize
					pageAt = 0
				}
				marker := bytes.Clone(data[markerAt : markerAt+recSize])
				switch mode {
				case "delete-page":
					data = append(bytes.Clone(data[:pageAt]), data[pageAt+recSize:]...)
				case "marker-only":
					data = marker
				case "routing":
					// Recompute the record MAC to demonstrate whole-commit identity is
					// checked independently of the per-record check. V2 needs no key.
					p := pending[0]
					replacement := encodeWALRecordWithMAC(p.seq, 2, p.plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
					copy(data[pageAt:], replacement)
				case "sequence", "zero-sequence":
					seq := pending[0].seq + 1
					if mode == "zero-sequence" {
						seq = 0
					}
					replacement := encodeWALRecordWithMAC(seq, pending[0].pageID, pending[0].plain, e.walMACKey, e.walMACEnabled, e.physicalPageSize)
					copy(data[pageAt:], replacement)
				case "descriptor":
					copy(data[markerAt:], e.encodeHeaderWALRecord(final, uint64(len(pending))))
				case "identity":
					final.EncryptionSalt[0] ^= 0x40
					copy(data[markerAt:], e.encodeFramedHeaderWALRecord(final, pending))
				}
				if err := e.Close(); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(WalPath(path), data, 0o600); err != nil {
					t.Fatal(err)
				}
				recovered, err := Open(path, opts)
				if recovered != nil {
					_ = recovered.Close()
				}
				if !errors.Is(err, ErrCorruptWAL) {
					t.Fatalf("Open = %v, want ErrCorruptWAL", err)
				}
				after, _ := os.ReadFile(path)
				wal, _ := os.ReadFile(WalPath(path))
				if !bytes.Equal(before, after) || !bytes.Equal(data, wal) {
					t.Fatal("rejected commit changed recovery files")
				}
			})
		}
	}
}

func TestFramedWALHeaderOnlyAndRepeatedWrites(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		t.Run(map[bool]string{false: "v2", true: "v3"}[authenticated], func(t *testing.T) {
			e, opts, _, _ := stagedFramedWAL(t, authenticated)
			e.AbortWriteTxn()
			// Recover the staged pending commit before testing the next transaction.
			path := e.path
			_ = e.Close()
			e, err := Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Close() }()
			if err := e.BeginWriteTxn(); err != nil {
				t.Fatal(err)
			}
			for range 1000 {
				if err := OpenBTree(e).Put([]byte("key"), []byte("final")); err != nil {
					t.Fatal(err)
				}
			}
			if len(e.wtxn.pending) != 1 || len(e.wtxn.dirty) != 1 {
				t.Fatalf("pending=%d,dirty=%d", len(e.wtxn.pending), len(e.wtxn.dirty))
			}
			if err := e.CommitWriteTxn(); err != nil {
				t.Fatal(err)
			}
			if err := e.UpdateHeader(func(h *Header) { h.CommitSeq++ }); err != nil {
				t.Fatal(err)
			}
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Close() }()
			value, ok, err := OpenBTree(e).Get([]byte("key"))
			if err != nil || !ok || string(value) != "final" {
				t.Fatalf("value=%s,ok=%v,err=%v", value, ok, err)
			}
		})
	}
}

func TestOpenValidatesBeforeDurableConfigurationChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database")
	e, err := Open(path, &Options{EmbeddingDim: 3, EmbeddingMetric: DistanceCosine})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Close()
	before, _ := os.ReadFile(path)
	_, err = Open(path, &Options{EmbeddingDim: 4, EmbeddingMetric: DistanceCosine, MaxValueBytes: 16384})
	if !errors.Is(err, ErrInvalidEmbeddingConfig) {
		t.Fatalf("Open=%v", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid options changed primary")
	}
	e, err = Open(path, &Options{MaxValueBytes: 16384})
	if err != nil {
		t.Fatal(err)
	}
	commits, syncs := e.WALStats()
	if commits != 1 || syncs != 1 {
		t.Fatalf("configuration bypassed durable commit: %d,%d", commits, syncs)
	}
	_ = e.Close()
	e, err = Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	if e.MaxValueBytes() != 16384 {
		t.Fatalf("persisted limit=%d", e.MaxValueBytes())
	}
}

func TestUncertainCommitFencesReads(t *testing.T) {
	e, _, _, _ := stagedFramedWAL(t, false)
	if err := e.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.CommitWriteTxn(); err == nil {
		t.Fatal("expected primary failure")
	}
	if _, err := e.ReadHeader(); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("ReadHeader=%v", err)
	}
	if _, err := e.ReadPage(1); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("ReadPage=%v", err)
	}
}

func TestCommitWALCleanupExtent(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		name := "plaintext"
		var opts *Options
		if authenticated {
			name, opts = "authenticated", authenticatedRecoveryTestOptions()
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "database")
			e, err := Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Close() }()
			for _, pages := range []int{1, 1, 8, 1, 4, 4} {
				if err := e.BeginWriteTxn(); err != nil {
					t.Fatal(err)
				}
				for id := range pages {
					if err := e.WritePage(uint64(id+1), bytes.Repeat([]byte{byte(pages)}, e.pageSize)); err != nil {
						t.Fatal(err)
					}
				}
				if err := e.CommitWriteTxn(); err != nil {
					t.Fatal(err)
				}
				recordSize := walRecordSize(e.physicalPageSize)
				if e.walMACEnabled {
					recordSize += walRecordMACSize
				}
				info, err := e.wal.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if want := int64((pages + 1) * recordSize); info.Size() != want {
					t.Fatalf("WAL extent = %d, want %d", info.Size(), want)
				}
			}
			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
			e, err = Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.ReadPage(1); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHeaderRejectsInvalidBoundaryAndReservedBytes(t *testing.T) {
	for _, authenticated := range []bool{false, true} {
		for _, mode := range []string{"zero-boundary", "root-outside-boundary", "overflow-boundary", "reserved"} {
			t.Run(map[bool]string{false: "v2/", true: "v3/"}[authenticated]+mode, func(t *testing.T) {
				var opts *Options
				if authenticated {
					opts = authenticatedRecoveryTestOptions()
				}
				path := filepath.Join(t.TempDir(), "database")
				e, err := Open(path, opts)
				if err != nil {
					t.Fatal(err)
				}
				_ = e.Close()
				page, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "zero-boundary":
					clear(page[24:32])
				case "root-outside-boundary":
					binary.BigEndian.PutUint64(page[32:40], 1)
				case "overflow-boundary":
					binary.BigEndian.PutUint64(page[24:32], ^uint64(0))
				case "reserved":
					page[headerReservedOffset] = 1
				}
				if err := os.WriteFile(path, page, 0o600); err != nil {
					t.Fatal(err)
				}
				recovered, err := Open(path, opts)
				if recovered != nil {
					_ = recovered.Close()
				}
				if !errors.Is(err, ErrCorruptHeader) && !errors.Is(err, ErrPageAuthentication) {
					t.Fatalf("Open=%v", err)
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(page, after) {
					t.Fatal("invalid header changed primary")
				}
			})
		}
	}
}
