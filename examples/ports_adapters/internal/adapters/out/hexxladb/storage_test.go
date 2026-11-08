package hexxladbout

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
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
	key, err := hexxladb.Pack(hexxladb.Coord{Q: 16, R: 32})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := hexxladb.CellRecord{Key: key, RawContent: "storage-smoke"}
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
