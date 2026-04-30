package index_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

func TestParseSourceKeyWithSeq_roundTrip(t *testing.T) {
	t.Parallel()
	p := lattice.PackedCoord{9, 10}
	key, err := index.SourceKeyWithVersion("mysrc", p, 1001)
	if err != nil {
		t.Fatal(err)
	}
	sid, got, seq, hasSeq, err := index.ParseSourceKeyWithSeq(key)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSeq || seq != 1001 || sid != "mysrc" || got != p {
		t.Fatalf("got sid=%q p=%v seq=%d hasSeq=%v", sid, got, seq, hasSeq)
	}
	base, err := index.SourceKey("bare", p)
	if err != nil {
		t.Fatal(err)
	}
	sid2, got2, seq2, hasSeq2, err := index.ParseSourceKeyWithSeq(base)
	if err != nil {
		t.Fatal(err)
	}
	if hasSeq2 || seq2 != 0 || sid2 != "bare" || got2 != p {
		t.Fatalf("bare got sid=%q seq=%d hasSeq=%v", sid2, seq2, hasSeq2)
	}
}
