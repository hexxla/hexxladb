package hexxladb_test

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/engine"
)

func TestOpen_encrypted_requiresKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "enc.db")
	key := []byte("sixteen.byte.key!!") // 16 bytes
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = hexxladb.Open(path, nil)
	if !errors.Is(err, hexxladb.ErrEncryptionKeyRequired) {
		t.Fatalf("Open without key: got %v want %v", err, hexxladb.ErrEncryptionKeyRequired)
	}
}

func TestOpen_plaintext_rejectsEncryptionOptions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.Put([]byte("k"), []byte("v")) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if !errors.Is(err, hexxladb.ErrDatabaseNotEncrypted) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrDatabaseNotEncrypted)
	}
}

func TestDB_encryptedKVSurvivesReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "enc_kv.db")
	key := []byte("sixteen.byte.key!!")
	opts := &hexxladb.Options{EncryptionKey: key}

	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("secret"), []byte("payload"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	err = db2.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("secret"))
		if err != nil || !ok || string(v) != "payload" {
			t.Fatalf("Get: ok=%v v=%q err=%v", ok, v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDB_encryptedPassphraseSurvivesReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "enc_pass.db")
	opts := &hexxladb.Options{Passphrase: "user-passphrase"}

	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error { return tx.Put([]byte("k"), []byte("v")) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db2, err := hexxladb.Open(path, &hexxladb.Options{Passphrase: "user-passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	err = db2.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("k"))
		if err != nil || !ok || string(v) != "v" {
			t.Fatalf("Get: ok=%v v=%q err=%v", ok, v, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOptions_encryptionConflictsWithHooks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	_, err := hexxladb.Open(path, &hexxladb.Options{
		EncryptionKey:   []byte("sixteen.byte.key!!"),
		BeforeWritePage: func(uint64, []byte) ([]byte, error) { return nil, nil },
	})
	if !errors.Is(err, hexxladb.ErrEncryptionOptions) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrEncryptionOptions)
	}
}

func TestOpen_encryptedWrongKeyFailsFast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wrong_key.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("another.16byte!!")})
	if !errors.Is(err, hexxladb.ErrEncryptionKeyMismatch) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrEncryptionKeyMismatch)
	}
}

func TestOpen_encryptedWrongPassphraseFailsFast(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "wrong_pass.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{Passphrase: "correct-passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(path, &hexxladb.Options{Passphrase: "incorrect-passphrase"})
	if !errors.Is(err, hexxladb.ErrEncryptionKeyMismatch) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrEncryptionKeyMismatch)
	}
}

func TestOpen_encryptedCorruptWALDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "enc_corrupt_wal.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, engine.DefaultPageSize)
	rec := make([]byte, 8+8+4+engine.DefaultPageSize)
	binary.BigEndian.PutUint64(rec[0:8], 1)
	binary.BigEndian.PutUint64(rec[8:16], 1)
	binary.BigEndian.PutUint32(rec[16:20], crc32.ChecksumIEEE(payload))
	copy(rec[20:], payload)
	rec[len(rec)-1] ^= 0xff // break CRC
	if err := os.WriteFile(path+"-wal", rec, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if !errors.Is(err, hexxladb.ErrCorruptDatabase) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrCorruptDatabase)
	}
}

func TestOpen_encryptedTruncatedWALDetected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "enc_trunc_wal.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, engine.DefaultPageSize)
	rec := make([]byte, 8+8+4+engine.DefaultPageSize+32)
	binary.BigEndian.PutUint64(rec[0:8], 1)
	binary.BigEndian.PutUint64(rec[8:16], 1)
	binary.BigEndian.PutUint32(rec[16:20], crc32.ChecksumIEEE(payload))
	copy(rec[20:], payload)
	rec = rec[:len(rec)-9]
	if err := os.WriteFile(path+"-wal", rec, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(path, &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")})
	if !errors.Is(err, hexxladb.ErrCorruptDatabase) {
		t.Fatalf("got %v want %v", err, hexxladb.ErrCorruptDatabase)
	}
}

func TestRotateEncryption_reencryptsDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.db")
	oldOpts := &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")}
	newOpts := &hexxladb.Options{Passphrase: "rotated-passphrase"}
	db, err := hexxladb.Open(path, oldOpts)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.Put([]byte("k"), []byte("v"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hexxladb.RotateEncryption(path, oldOpts, newOpts); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(path, oldOpts)
	if !errors.Is(err, hexxladb.ErrEncryptionKeyMismatch) {
		t.Fatalf("old key should fail after rotation: got %v", err)
	}
	db2, err := hexxladb.Open(path, newOpts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if err := db2.View(func(tx *hexxladb.Tx) error {
		v, ok, err := tx.Get([]byte("k"))
		if err != nil {
			return err
		}
		if !ok || string(v) != "v" {
			t.Fatalf("rotated data mismatch ok=%v v=%q", ok, string(v))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRotateEncryptionWithOptions_progressAndStreaming(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate_stream.db")
	oldOpts := &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")}
	newOpts := &hexxladb.Options{Passphrase: "rotated-passphrase"}
	db, err := hexxladb.Open(path, oldOpts)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		k := []byte{byte(i), 'k'}
		v := []byte{byte(i), 'v'}
		if err := db.Update(func(tx *hexxladb.Tx) error { return tx.Put(k, v) }); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var progressed int64
	err = hexxladb.RotateEncryptionWithOptions(path, oldOpts, newOpts, &hexxladb.RotateOptions{
		BatchSize: 7,
		OnProgress: func(copied int64) {
			progressed = copied
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if progressed < 50 {
		t.Fatalf("progress callback should reflect copied rows, got %d", progressed)
	}
}
