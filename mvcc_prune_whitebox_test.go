package hexxladb

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestMVCC_whiteboxBtreeKeyShapeAfterRepeatedPutCell(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "shape.db")
	const iterations = 273
	db, err := Open(path, &Options{
		EnableMVCC: true,
		MVCCRetention: MVCCRetention{
			RetainCommitsBehindHead: 27,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 3, R: 9})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := range iterations {
		raw := fmt.Sprintf("payload-%08d", i)
		if err := db.Update(func(tx *Tx) error {
			return tx.PutCell(ctx, record.CellRecord{Key: p, RawContent: raw})
		}); err != nil {
			t.Fatal(err)
		}
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	var total int
	var cellVer int
	var metaKeys int
	prefixes := make(map[string]int)
	err = db.btree.AscendRange(nil, nil, func(k, _ []byte) bool {
		total++
		s := string(k)
		i := strings.IndexByte(s, '/')
		if i >= 0 {
			prefixes[s[:i+1]]++
		} else {
			prefixes[s]++
		}
		if _, _, e := index.ParseCellVersionKey(k); e == nil {
			cellVer++
		}
		if _, _, ok := index.ParseCommitTimeKey(k); ok {
			metaKeys++
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if cellVer != iterations || metaKeys != iterations || total != cellVer+metaKeys {
		t.Fatalf("btree keys total=%d cellVer=%d meta=%d want %d+%d; prefixes=%v",
			total, cellVer, metaKeys, iterations, iterations, prefixes)
	}
}
