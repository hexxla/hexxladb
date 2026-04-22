package index_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestTagKey_roundTrip(t *testing.T) {
	t.Parallel()
	p := lattice.PackedCoord{1, 2}
	key, err := index.TagKey("alpha", p)
	if err != nil {
		t.Fatal(err)
	}
	tag, got, err := index.ParseTagKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "alpha" || got != p {
		t.Fatalf("got %q %+v", tag, got)
	}
}

func TestTagKeyWithVersion_roundTrip(t *testing.T) {
	t.Parallel()
	p := lattice.PackedCoord{3, 4}
	key, err := index.TagKeyWithVersion("beta", p, 42)
	if err != nil {
		t.Fatal(err)
	}
	tag, got, err := index.ParseTagKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "beta" || got != p {
		t.Fatal(tag, got)
	}
}

func TestTagRangePrefix_lexOrder(t *testing.T) {
	t.Parallel()
	lo, err := lattice.Pack(lattice.Coord{Q: 0, R: 0})
	if err != nil {
		t.Fatal(err)
	}
	k1, err := index.TagKey("z", lo)
	if err != nil {
		t.Fatal(err)
	}
	from, to, err := index.TagRangePrefix("z")
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) < string(from) || string(k1) > string(to) {
		t.Fatalf("key %s not in [%s,%s]", k1, from, to)
	}
}
