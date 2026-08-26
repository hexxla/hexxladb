package hexxladb

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestHealthCheckRejectsCorruptVisibleCell(t *testing.T) {
	t.Parallel()
	for _, mvccEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "mvcc"}[mvccEnabled], func(t *testing.T) {
			t.Parallel()
			db, err := Open(filepath.Join(t.TempDir(), "health.db"), &Options{EnableMVCC: mvccEnabled})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			coord, err := lattice.Pack(lattice.Coord{Q: 2, R: -1})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *Tx) error {
				key := index.CellKey(coord)
				if mvccEnabled {
					key = index.CellKeyWithVersion(coord, tx.writeSeq)
				}
				return tx.putDirect(key, []byte("not-a-cell-record"))
			}); err != nil {
				t.Fatal(err)
			}

			_, err = db.HealthCheck(t.Context(), DefaultHealthCheckConfig())
			if !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("HealthCheck error = %v, want ErrCorruptDatabase", err)
			}
		})
	}
}

func TestHealthCheckRejectsCellKeyRecordMismatch(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "health.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	keyCoord, _ := lattice.Pack(lattice.Coord{Q: 1, R: 0})
	recordCoord, _ := lattice.Pack(lattice.Coord{Q: 2, R: 0})
	encoded, err := record.EncodeCell(record.CellRecord{Key: recordCoord, RawContent: "mismatch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *Tx) error {
		return tx.putDirect(index.CellKey(keyCoord), encoded)
	}); err != nil {
		t.Fatal(err)
	}

	_, err = db.HealthCheck(t.Context(), DefaultHealthCheckConfig())
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("HealthCheck error = %v, want ErrCorruptDatabase", err)
	}
}

func TestHealthCheckRejectsCorruptVisibleSeam(t *testing.T) {
	t.Parallel()
	const seamID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, mvccEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "v1", true: "mvcc"}[mvccEnabled], func(t *testing.T) {
			t.Parallel()
			db, err := Open(filepath.Join(t.TempDir(), "health.db"), &Options{EnableMVCC: mvccEnabled})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			if err := db.Update(func(tx *Tx) error {
				key, err := index.SeamKey(seamID)
				if mvccEnabled {
					key, err = index.SeamKeyWithVersion(seamID, tx.writeSeq)
				}
				if err != nil {
					return err
				}
				return tx.putDirect(key, []byte("not-a-seam-record"))
			}); err != nil {
				t.Fatal(err)
			}

			_, err = db.HealthCheck(t.Context(), DefaultHealthCheckConfig())
			if !errors.Is(err, ErrCorruptDatabase) {
				t.Fatalf("HealthCheck error = %v, want ErrCorruptDatabase", err)
			}
		})
	}
}

func TestHealthCheckRejectsMalformedSecondaryKey(t *testing.T) {
	t.Parallel()
	db, err := Open(filepath.Join(t.TempDir(), "health.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *Tx) error {
		return tx.putDirect([]byte("tag/malformed"), nil)
	}); err != nil {
		t.Fatal(err)
	}

	_, err = db.HealthCheck(t.Context(), DefaultHealthCheckConfig())
	if !errors.Is(err, ErrCorruptDatabase) {
		t.Fatalf("HealthCheck error = %v, want ErrCorruptDatabase", err)
	}
}
