package index_test

import (
	"bytes"
	"strings"
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

func TestParseTagKeyWithSeq_roundTrip(t *testing.T) {
	t.Parallel()
	p := lattice.PackedCoord{7, 8}
	key, err := index.TagKeyWithVersion("gamma", p, 99)
	if err != nil {
		t.Fatal(err)
	}
	tag, got, seq, hasSeq, err := index.ParseTagKeyWithSeq(key)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSeq || seq != 99 || tag != "gamma" || got != p {
		t.Fatalf("got tag=%q p=%v seq=%d hasSeq=%v", tag, got, seq, hasSeq)
	}
	keyBare, err := index.TagKey("delta", p)
	if err != nil {
		t.Fatal(err)
	}
	tag2, got2, seq2, hasSeq2, err := index.ParseTagKeyWithSeq(keyBare)
	if err != nil {
		t.Fatal(err)
	}
	if hasSeq2 || seq2 != 0 || tag2 != "delta" || got2 != p {
		t.Fatalf("bare got tag=%q seq=%d hasSeq=%v", tag2, seq2, hasSeq2)
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

func TestTagFamilyScanBounds_coversPhysicalTagKeys(t *testing.T) {
	t.Parallel()
	from, to := index.TagFamilyScanBounds()
	long := strings.Repeat("z", index.MaxTagBytes)
	var maxP lattice.PackedCoord
	maxP[0], maxP[1] = ^uint64(0), ^uint64(0)
	k, err := index.TagKeyWithVersion(long, maxP, ^uint64(0))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Compare(k, from) < 0 || bytes.Compare(k, to) > 0 {
		t.Fatalf("physical tag key out of [%q,%q]: len=%d cmp=%d", from, to, len(k), bytes.Compare(k, to))
	}
}
