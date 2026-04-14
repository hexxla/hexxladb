package index_test

import (
	"bytes"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestSourceKey_roundTripAndOrder(t *testing.T) {
	t.Parallel()
	p := lattice.PackedCoord{1, 2}
	k1, err := index.SourceKey("aaa", p)
	if err != nil {
		t.Fatal(err)
	}
	id, got, err := index.ParseSourceKey(k1)
	if err != nil {
		t.Fatal(err)
	}
	if id != "aaa" || got != p {
		t.Fatalf("got id=%q p=%v", id, got)
	}
	k2, err := index.SourceKey("bbb", p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(k1, k2) >= 0 {
		t.Fatalf("aaa should sort before bbb")
	}
	from, to, err := index.SourceRangePrefix("gamma")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(from, to) > 0 {
		t.Fatal("range from/to")
	}
}
