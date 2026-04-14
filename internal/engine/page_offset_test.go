package engine

import (
	"errors"
	"math"
	"testing"
)

func TestPageByteOffset_overflow(t *testing.T) {
	t.Parallel()
	tooBig := uint64(math.MaxInt64)/uint64(PageSize) + 2
	_, err := pageByteOffset(tooBig)
	if !errors.Is(err, ErrBadPageID) {
		t.Fatalf("want ErrBadPageID, got %v", err)
	}
}

func TestPageByteOffset_ok(t *testing.T) {
	t.Parallel()
	off, err := pageByteOffset(2)
	if err != nil {
		t.Fatal(err)
	}
	if off != 2*int64(PageSize) {
		t.Fatalf("offset %d", off)
	}
}
