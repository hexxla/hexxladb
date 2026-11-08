package engine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRejectsUnsafePlaintextMVCCRedoWithoutChangingFiles(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"unmarked", "missing-page", "corrupt-tail"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "database")
			opts := &Options{UseFormatV2: true}
			eng, err := Open(path, opts)
			if err != nil {
				t.Fatal(err)
			}
			hdr, err := eng.ReadHeader()
			if err != nil {
				t.Fatal(err)
			}
			hdr.LastWALSeq, hdr.CommitSeq, hdr.NextPageID = 1, 1, 2
			page := encodeWALRecord(1, 1, make([]byte, DefaultPageSize), DefaultPageSize)
			var wal []byte
			switch mode {
			case "unmarked":
				wal = page
			case "missing-page":
				wal = eng.encodeHeaderWALRecord(hdr, 1)
			case "corrupt-tail":
				wal = append(eng.encodeHeaderWALRecord(hdr, 1), page...)
				wal = append(wal, 1)
			}
			if err := eng.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(WalPath(path), wal, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path, opts); !errors.Is(err, ErrCorruptWAL) {
				t.Fatalf("Open = %v, want ErrCorruptWAL", err)
			}
			primary, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(primary, before) {
				t.Fatalf("primary changed after rejected replay: %v", err)
			}
			pending, err := os.ReadFile(WalPath(path))
			if err != nil || !bytes.Equal(pending, wal) {
				t.Fatalf("WAL changed after rejected replay: %v", err)
			}
		})
	}
}

func TestFailedPrimaryWriteFencesRetriesUntilReplay(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "database")
	opts := &Options{UseFormatV2: true}
	eng, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	if err := eng.BeginWriteTxn(); err != nil {
		t.Fatal(err)
	}
	value := bytes.Repeat([]byte{0x2b}, DefaultPageSize)
	if err := eng.WritePage(1, value); err != nil {
		t.Fatal(err)
	}
	if err := eng.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := eng.CommitWriteTxn(); err == nil {
		t.Fatal("commit wrote to a closed primary")
	}
	if err := eng.BeginWriteTxn(); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("retry = %v, want ErrRecoveryRequired", err)
	}
	_ = eng.Close()
	recovered, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	got, err := recovered.ReadPage(1)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatalf("page was not recovered from durable redo: %v", err)
	}
}
