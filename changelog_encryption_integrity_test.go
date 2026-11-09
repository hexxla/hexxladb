package hexxladb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putChangelogIntegrityCell(t *testing.T, db *DB, content string) {
	t.Helper()
	if err := db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), CellRecord{Key: PackedCoord{}, RawContent: content})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEncryptedChangelogBackupDivergenceUsesFreshNonces(t *testing.T) {
	dir := t.TempDir()
	path, backup := filepath.Join(dir, "source.db"), filepath.Join(dir, "backup.db")
	opts := &Options{EncryptionKey: []byte("changelog backup divergence test key"), ChangelogEnabled: true}
	source, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	if err := source.BackupTo(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	putChangelogIntegrityCell(t, source, strings.Repeat("A", 64))
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(backup, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	putChangelogIntegrityCell(t, restored, strings.Repeat("B", 64))
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path + "-changelog")
	if err != nil {
		t.Fatal(err)
	}
	diverged, err := os.ReadFile(backup + "-changelog")
	if err != nil {
		t.Fatal(err)
	}
	const headerSize, lengthSize, seqSize, nonceSize = 48, 4, 8, 24
	const seqOffset = headerSize + lengthSize
	const nonceOffset = seqOffset + seqSize
	if len(original) < nonceOffset+nonceSize || len(diverged) < nonceOffset+nonceSize {
		t.Fatal("missing encrypted frames")
	}
	if !bytes.Equal(original[:headerSize], diverged[:headerSize]) {
		t.Fatal("backup changed the copied encryption header")
	}
	if binary.BigEndian.Uint64(original[seqOffset:nonceOffset]) != 1 || binary.BigEndian.Uint64(diverged[seqOffset:nonceOffset]) != 1 {
		t.Fatal("test did not reuse the logical sequence")
	}
	if bytes.Equal(original[nonceOffset:nonceOffset+nonceSize], diverged[nonceOffset:nonceOffset+nonceSize]) {
		t.Fatal("divergent backups reused an encryption nonce")
	}
	cipherOffset := nonceOffset + nonceSize
	xor := make([]byte, min(len(original), len(diverged))-cipherOffset)
	for i := range xor {
		xor[i] = original[cipherOffset+i] ^ diverged[cipherOffset+i]
	}
	if bytes.Contains(xor, bytes.Repeat([]byte{'A' ^ 'B'}, 64)) {
		t.Fatal("ciphertext XOR exposed the known plaintext difference")
	}
}

func TestChangelogAcknowledgedTailValidatedBeforeRecovery(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, consumers := range []bool{false, true} {
			for _, pending := range []bool{false, true} {
				name := "plain"
				if encrypted {
					name = "encrypted"
				}
				if consumers {
					name += "/consumer"
				} else {
					name += "/no-consumer"
				}
				if pending {
					name += "/pending"
				}
				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "data.db")
					opts := &Options{ChangelogEnabled: true}
					headerSize := int64(16)
					if encrypted {
						opts.EncryptionKey = []byte("changelog tail validation test key")
						headerSize = 48
					}
					db, err := Open(path, opts)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = db.Close() }()
					putChangelogIntegrityCell(t, db, "acknowledged")
					if consumers {
						if err := db.AdvanceChangelogConsumer("reader", 0, 1); err != nil {
							t.Fatal(err)
						}
					}
					if pending {
						db.commitFaults = &commitFaultHooks{beforeChangelogAppend: func() error { return errors.New("projection fault") }}
						err := db.Update(func(tx *Tx) error {
							return tx.PutCell(t.Context(), CellRecord{Key: PackedCoord{}, RawContent: "pending"})
						})
						if !errors.Is(err, ErrCommitDurable) {
							t.Fatalf("failed projection error = %v", err)
						}
					}
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
					if err := os.Truncate(path+"-changelog", headerSize); err != nil {
						t.Fatal(err)
					}
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					opened, err := Open(path, opts)
					if opened != nil {
						_ = opened.Close()
						t.Fatal("opened with missing acknowledged changelog history")
					}
					wantErr := ErrChangelogCorrupt
					if consumers {
						wantErr = ErrChangelogConsumerInvalidated
					}
					if !errors.Is(err, wantErr) {
						t.Fatalf("open error = %v, want %v", err, wantErr)
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatalf("rejected recovery modified authoritative checkpoint: %v", err)
					}
					info, err := os.Stat(path + "-changelog")
					if err != nil || info.Size() != headerSize {
						t.Fatalf("rejected recovery appended to truncated history: %v", err)
					}
				})
			}
		}
	}
}
