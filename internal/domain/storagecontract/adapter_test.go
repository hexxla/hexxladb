package storagecontract

import (
	"path/filepath"
	"testing"

	hxdb "github.com/hexxla/hexxladb"
	hexxladbout "github.com/hexxla/hexxladb/internal/adapters/out/hexxladb"
	"github.com/hexxla/hexxladb/internal/domain"
)

// TestAdapterContract runs the full contract suite against the real
// hexxladbout.Storage adapter backed by a live database.
func TestAdapterContract(t *testing.T) {
	t.Parallel()
	RunAll(t, func(t *testing.T) domain.Storage {
		t.Helper()
		dir := t.TempDir()
		db, err := hxdb.Open(filepath.Join(dir, "contract.db"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return hexxladbout.NewStorage(db)
	})
}
