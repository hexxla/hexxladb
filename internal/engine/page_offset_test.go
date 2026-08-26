package engine

import (
	"errors"
	"math"
	"testing"
)

func TestPageByteOffset_overflow(t *testing.T) {
	t.Parallel()
	tooBig := uint64(math.MaxInt64)/uint64(DefaultPageSize) + 2
	_, err := pageByteOffset(tooBig, DefaultPageSize, DefaultPageSize)
	if !errors.Is(err, ErrBadPageID) {
		t.Fatalf("want ErrBadPageID, got %v", err)
	}
}

func TestPageByteOffset_ok(t *testing.T) {
	t.Parallel()
	off, err := pageByteOffset(2, DefaultPageSize, DefaultPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if off != 2*int64(DefaultPageSize) {
		t.Fatalf("offset %d", off)
	}
}

func TestPageByteOffset_authenticatedStride(t *testing.T) {
	t.Parallel()
	off, err := pageByteOffset(2, DefaultPageSize, DefaultPageSize+AuthenticatedPageOverhead)
	if err != nil {
		t.Fatal(err)
	}
	want := int64(2*DefaultPageSize + AuthenticatedPageOverhead)
	if off != want {
		t.Fatalf("offset = %d, want %d", off, want)
	}
}
