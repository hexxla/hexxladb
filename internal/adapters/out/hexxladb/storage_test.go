package hexxladbout

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// Smoke-test that the outbound adapter still satisfies [domain.Storage] forwarding to the public API.
func TestStorage_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := hexxladb.Open(filepath.Join(dir, "a.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	s := NewStorage(db)
	var key lattice.PackedCoord
	key[0] = 0x10
	key[1] = 0x20
	ctx := context.Background()
	want := record.CellRecord{Key: key, RawContent: "storage-smoke"}
	if err := s.PutCell(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetCell(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("GetCell: missing")
	}
	if got.RawContent != "storage-smoke" {
		t.Fatalf("content: got %q", got.RawContent)
	}
}
