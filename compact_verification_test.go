package hexxladb_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestCompactToWithOptionsVerificationRejectsUnhealthyCandidate(t *testing.T) {
	t.Parallel()
	db, sourcePath := openCompactDB(t, nil)
	first, err := hexxladb.Pack(hexxladb.Coord{Q: 10, R: 10})
	if err != nil {
		t.Fatal(err)
	}
	second, err := hexxladb.Pack(hexxladb.Coord{Q: 11, R: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *hexxladb.Tx) error {
		return tx.PutSeam(t.Context(), hexxladb.SeamRecord{
			ID:       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			CellA:    first,
			CellB:    second,
			SeamType: hexxladb.SeamTypeConflict,
		})
	}); err != nil {
		t.Fatal(err)
	}
	closeCompactDB(t, db)

	destinationPath := filepath.Join(t.TempDir(), "destination.db")
	err = hexxladb.CompactToWithOptions(
		t.Context(),
		sourcePath,
		destinationPath,
		nil,
		&hexxladb.CompactOptions{VerifyDestination: true},
	)
	if !errors.Is(err, hexxladb.ErrCorruptDatabase) {
		t.Fatalf("verified compaction error=%v, want ErrCorruptDatabase", err)
	}
	if _, err := os.Stat(destinationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unhealthy destination was not removed: %v", err)
	}
}
