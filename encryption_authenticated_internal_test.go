package hexxladb

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestAuthenticatedEncryptionIsDefaultForNewEncryptedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authenticated.db")
	key := []byte("authenticated format test key")
	db, err := Open(path, &Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), CellRecord{Key: coord, RawContent: "authenticated"})
	}); err != nil {
		t.Fatal(err)
	}
	hdr, err := db.eng.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.FormatVersion != engine.AuthenticatedFormatVersion || hdr.Features&engine.FeatureAuthenticatedDataPages == 0 {
		t.Fatalf("header = %+v, want authenticated format", hdr)
	}
	stats, err := db.StorageStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.PhysicalPageSize != stats.PageSize+engine.AuthenticatedPageOverhead {
		t.Fatalf("page sizes = %d/%d", stats.PageSize, stats.PhysicalPageSize)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	physicalPageSize := int64(hdr.PageSize + engine.AuthenticatedPageOverhead)
	rootOffset := int64(hdr.PageSize) + int64(hdr.BTreeRoot-1)*physicalPageSize
	file, err := os.OpenFile(path, os.O_RDWR, 0) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	var altered [1]byte
	if _, err := file.ReadAt(altered[:], rootOffset+32); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	altered[0] ^= 0x80
	if _, err := file.WriteAt(altered[:], rootOffset+32); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(path, &Options{EncryptionKey: key})
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("tampered page open error = %v, want ErrCorruptDatabase", err)
	}
}

func TestAuthenticatedHeaderTamperFailsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "header.db")
	key := []byte("authenticated header test key")
	db, err := Open(path, &Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	var altered [1]byte
	if _, err := file.ReadAt(altered[:], 24); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	altered[0] ^= 0x01
	if _, err := file.WriteAt(altered[:], 24); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, &Options{EncryptionKey: key}); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("header tamper error = %v, want ErrCorruptDatabase", err)
	}
}

func TestAuthenticatedStaleRootPageFailsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale-root.db")
	key := []byte("authenticated stale root key")
	db, err := Open(path, &Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 1})
	if err != nil {
		t.Fatal(err)
	}
	put := func(content string) {
		t.Helper()
		if err := db.Update(func(tx *Tx) error {
			return tx.PutCell(t.Context(), CellRecord{Key: coord, RawContent: content})
		}); err != nil {
			t.Fatal(err)
		}
	}
	put("first")
	before, err := db.eng.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	physicalPageSize := int64(before.PageSize + engine.AuthenticatedPageOverhead)
	rootOffset := int64(before.PageSize) + int64(before.BTreeRoot-1)*physicalPageSize
	oldRoot := make([]byte, physicalPageSize)
	reader, err := os.Open(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadAt(oldRoot, rootOffset); err != nil {
		_ = reader.Close()
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	put("second")
	after, err := db.eng.ReadHeader()
	if err != nil {
		t.Fatal(err)
	}
	if before.BTreeRoot != after.BTreeRoot || before.BTreeRootGeneration == after.BTreeRootGeneration {
		t.Fatalf("root/generation before=%d/%d after=%d/%d", before.BTreeRoot, before.BTreeRootGeneration, after.BTreeRoot, after.BTreeRootGeneration)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt(oldRoot, rootOffset); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, &Options{EncryptionKey: key}); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("stale root error = %v, want ErrCorruptDatabase", err)
	}
}

func TestAuthenticatedPrimaryTruncationFailsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "truncated.db")
	key := []byte("authenticated truncation key")
	db, err := Open(path, &Options{EncryptionKey: key})
	if err != nil {
		t.Fatal(err)
	}
	coord, err := lattice.Pack(lattice.Coord{Q: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.PutCell(t.Context(), CellRecord{Key: coord, RawContent: "truncate me"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, info.Size()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, &Options{EncryptionKey: key}); !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("truncated primary error = %v, want ErrCorruptDatabase", err)
	}
}
