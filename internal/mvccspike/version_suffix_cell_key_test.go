package mvccspike_test

import (
	"path/filepath"
	"testing"

	"github.com/hexxla/hexxladb"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/mvccspike"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestCellPhysicalKeyWithVersionSuffix_parseCommitSeqRoundTrip(t *testing.T) {
	t.Parallel()
	p, err := lattice.Pack(lattice.Coord{Q: 2, R: -3})
	if err != nil {
		t.Fatal(err)
	}
	const want uint64 = 0xdeadbeefcafebabe
	k := mvccspike.CellPhysicalKeyWithVersionSuffix(p, want)
	got, ok := mvccspike.ParseCommitSeqFromPhysicalKey(k)
	if !ok {
		t.Fatal("ParseCommitSeqFromPhysicalKey")
	}
	if got != want {
		t.Fatalf("seq: got %x want %x", got, want)
	}
}

// Two physical keys for one logical cell; visibility by read_seq (E1 stub).
func TestCellPhysicalKeyWithVersionSuffix_twoCommitsVisibleByReadSeq(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "e1.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	p, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	rec1 := record.CellRecord{Key: p, RawContent: "v-at-seq-1"}
	rec2 := record.CellRecord{Key: p, RawContent: "v-at-seq-2"}
	wire1, err := record.EncodeCell(rec1)
	if err != nil {
		t.Fatal(err)
	}
	wire2, err := record.EncodeCell(rec2)
	if err != nil {
		t.Fatal(err)
	}

	const seq1, seq2 uint64 = 1, 2
	k1 := mvccspike.CellPhysicalKeyWithVersionSuffix(p, seq1)
	k2 := mvccspike.CellPhysicalKeyWithVersionSuffix(p, seq2)

	if err = db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.Put(k1, wire1); err != nil {
			return err
		}
		return tx.Put(k2, wire2)
	}); err != nil {
		t.Fatal(err)
	}

	from, to := mvccspike.CellVersionSuffixScanBounds(p)
	var got []mvccspike.VersionKV
	err = db.View(func(tx *hexxladb.Tx) error {
		return tx.AscendRange(from, to, func(k, v []byte) bool {
			seq, ok := mvccspike.ParseCommitSeqFromPhysicalKey(k)
			if !ok {
				t.Errorf("bad physical key %q", k)
				return false
			}
			got = append(got, mvccspike.VersionKV{CommitSeq: seq, Value: append([]byte(nil), v...)})
			return true
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("versions: got %d want 2", len(got))
	}

	cases := []struct {
		readSeq uint64
		want    string
	}{
		{0, ""},
		{1, "v-at-seq-1"},
		{2, "v-at-seq-2"},
		{99, "v-at-seq-2"},
	}
	for _, tc := range cases {
		val, seq, ok := mvccspike.SelectVisible(got, tc.readSeq)
		if tc.want == "" {
			if ok {
				t.Fatalf("readSeq=%d: want missing, got seq=%d val=%q", tc.readSeq, seq, val)
			}
			continue
		}
		if !ok {
			t.Fatalf("readSeq=%d: want %q, got missing", tc.readSeq, tc.want)
		}
		dec, err := record.DecodeCell(val)
		if err != nil {
			t.Fatalf("readSeq=%d: DecodeCell: %v", tc.readSeq, err)
		}
		if dec.RawContent != tc.want {
			t.Fatalf("readSeq=%d: got %q want %q (commitSeq=%d)", tc.readSeq, dec.RawContent, tc.want, seq)
		}
	}
}

func BenchmarkCellPhysicalKeyWithVersionSuffix_putTwoPhysicalRows(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		b.Fatal(err)
	}
	wire, err := record.EncodeCell(record.CellRecord{Key: p, RawContent: "x"})
	if err != nil {
		b.Fatal(err)
	}
	k1 := mvccspike.CellPhysicalKeyWithVersionSuffix(p, 1)
	k2 := mvccspike.CellPhysicalKeyWithVersionSuffix(p, 2)
	b.ResetTimer()
	for b.Loop() {
		err = db.Update(func(tx *hexxladb.Tx) error {
			if err := tx.Put(k1, wire); err != nil {
				return err
			}
			return tx.Put(k2, wire)
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCellPhysicalKeyWithVersionSuffix_putOnePhysicalRow(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench1.db")
	db, err := hexxladb.Open(path, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close() })
	p, err := lattice.Pack(lattice.Coord{Q: 1, R: -1})
	if err != nil {
		b.Fatal(err)
	}
	wire, err := record.EncodeCell(record.CellRecord{Key: p, RawContent: "x"})
	if err != nil {
		b.Fatal(err)
	}
	k := mvccspike.CellPhysicalKeyWithVersionSuffix(p, 1)
	b.ResetTimer()
	for b.Loop() {
		err = db.Update(func(tx *hexxladb.Tx) error {
			return tx.Put(k, wire)
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
