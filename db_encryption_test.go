package hexxladb_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
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
