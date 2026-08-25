package hexxladb_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
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

func TestDB_encryptedChangelogDoesNotExposeInlinePayload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "encrypted-changelog.db")
	opts := &hexxladb.Options{
		EncryptionKey:    []byte("sixteen.byte.key!!"),
		ChangelogEnabled: true,
	}
	db, err := hexxladb.Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	secret := "workstream-one-secret-sentinel"
	coord, err := lattice.Pack(lattice.Coord{Q: 4, R: -2})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: secret})
	}); err != nil {
		t.Fatal(err)
	}
	records, err := db.ReadChangelogSince(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !bytes.Contains(records[0].Inline, []byte(secret)) {
		t.Fatalf("logical changelog record did not round-trip protected content: %#v", records)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path + "-changelog")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("encrypted database changelog exposed inline record content at rest")
	}
}

func TestOpen_encryptedDatabaseRejectsLegacyPlaintextChangelog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy-changelog")
	plainDB, err := hexxladb.Open(filepath.Join(dir, "plain.db"), &hexxladb.Options{
		ChangelogEnabled: true,
		ChangelogPath:    legacyPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 1, R: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := plainDB.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: "legacy plaintext"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := plainDB.Close(); err != nil {
		t.Fatal(err)
	}

	encryptedPath := filepath.Join(dir, "encrypted.db")
	key := []byte("sixteen.byte.key!!")
	db, err := hexxladb.Open(encryptedPath, &hexxladb.Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(encryptedPath, &hexxladb.Options{
		EncryptionKey:    key,
		ChangelogEnabled: true,
		ChangelogPath:    legacyPath,
	})
	if !errors.Is(err, hexxladb.ErrChangelogPlaintext) {
		t.Fatalf("legacy changelog: want ErrChangelogPlaintext, got %v", err)
	}

	db, err = hexxladb.Open(encryptedPath, &hexxladb.Options{EncryptionKey: key})
	if err != nil {
		t.Fatalf("failed changelog open leaked database lock: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_rejectsChangelogFromAnotherEncryptedDatabase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key := []byte("sixteen.byte.key!!")
	paths := []string{filepath.Join(dir, "a.db"), filepath.Join(dir, "b.db")}
	for i, path := range paths {
		db, err := hexxladb.Open(path, &hexxladb.Options{EncryptionKey: key, ChangelogEnabled: true})
		if err != nil {
			t.Fatal(err)
		}
		coord, err := lattice.Pack(lattice.Coord{Q: i, R: -i})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Update(func(tx *hexxladb.Tx) error {
			return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: "private"})
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	foreign, err := os.ReadFile(paths[1] + "-changelog")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0]+"-changelog", foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(paths[0], &hexxladb.Options{EncryptionKey: key, ChangelogEnabled: true})
	if !errors.Is(err, hexxladb.ErrChangelogEncryptionKeyMismatch) {
		t.Fatalf("foreign encrypted changelog: want ErrChangelogEncryptionKeyMismatch, got %v", err)
	}

	plaintextPath := filepath.Join(dir, "plaintext.db")
	plain, err := hexxladb.Open(plaintextPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = hexxladb.Open(plaintextPath, &hexxladb.Options{
		ChangelogEnabled: true,
		ChangelogPath:    paths[1] + "-changelog",
	})
	if !errors.Is(err, hexxladb.ErrChangelogEncryptionKeyRequired) {
		t.Fatalf("plaintext database with encrypted changelog: want ErrChangelogEncryptionKeyRequired, got %v", err)
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
	for _, stale := range []string{path + "-wal", path + ".rotate.state"} {
		if _, err := os.Lstat(stale); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("successful rotation left stale recovery component %q: %v", stale, err)
		}
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

func TestRotateEncryption_reencryptsChangelog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rotate-changelog.db")
	changelogPath := path + ".audit"
	oldOpts := &hexxladb.Options{
		EncryptionKey:    []byte("sixteen.byte.key!!"),
		ChangelogEnabled: true,
		ChangelogPath:    changelogPath,
	}
	newOpts := &hexxladb.Options{
		Passphrase:       "rotated-passphrase",
		ChangelogEnabled: true,
		ChangelogPath:    changelogPath,
	}
	db, err := hexxladb.Open(path, oldOpts)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 2, R: -1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: "rotation secret"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hexxladb.RotateEncryption(path, oldOpts, newOpts); err != nil {
		t.Fatal(err)
	}
	for _, temporary := range []string{changelogPath + ".rotate.tmp", changelogPath + ".rotate.bak"} {
		if _, err := os.Stat(temporary); !os.IsNotExist(err) {
			t.Fatalf("rotation left temporary changelog %q: %v", temporary, err)
		}
	}
	db, err = hexxladb.Open(path, newOpts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !bytes.Contains(changes[0].Inline, []byte("rotation secret")) {
		t.Fatalf("rotated changelog mismatch: %#v", changes)
	}
}

func TestRotateEncryption_encryptsPlaintextChangelog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "encrypt-plaintext-changelog.db")
	currentOpts := &hexxladb.Options{ChangelogEnabled: true}
	newOpts := &hexxladb.Options{
		EncryptionKey:    []byte("sixteen.byte.key!!"),
		ChangelogEnabled: true,
	}
	db, err := hexxladb.Open(path, currentOpts)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: -2, R: 3})
	if err != nil {
		t.Fatal(err)
	}
	secret := "plaintext migration secret"
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutCell(t.Context(), record.CellRecord{Key: coord, RawContent: secret})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hexxladb.RotateEncryption(path, currentOpts, newOpts); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path + "-changelog")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || !bytes.HasPrefix(raw, []byte("HXCHGv02")) {
		t.Fatal("rotation did not replace the plaintext changelog with encrypted format v2")
	}
	db, err = hexxladb.Open(path, newOpts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	changes, err := db.ReadChangelogSince(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !bytes.Contains(changes[0].Inline, []byte(secret)) {
		t.Fatalf("migrated changelog mismatch: %#v", changes)
	}
}

func TestRotateEncryption_rejectsChangelogConfigurationChange(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "not-opened.db")
	for _, test := range []struct {
		name        string
		current     *hexxladb.Options
		replacement *hexxladb.Options
	}{
		{
			name:        "disable",
			current:     &hexxladb.Options{ChangelogEnabled: true},
			replacement: &hexxladb.Options{EncryptionKey: []byte("sixteen.byte.key!!")},
		},
		{
			name: "path",
			current: &hexxladb.Options{
				ChangelogEnabled: true,
				ChangelogPath:    path + "-old-changelog",
			},
			replacement: &hexxladb.Options{
				EncryptionKey:    []byte("sixteen.byte.key!!"),
				ChangelogEnabled: true,
				ChangelogPath:    path + "-new-changelog",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := hexxladb.RotateEncryption(path, test.current, test.replacement)
			if !errors.Is(err, hexxladb.ErrInvalidArgument) {
				t.Fatalf("configuration change: want ErrInvalidArgument, got %v", err)
			}
		})
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
