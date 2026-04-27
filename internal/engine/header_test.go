package engine

import (
	"errors"
	"testing"
)

func TestHeader_roundTrip(t *testing.T) {
	t.Parallel()
	h := Header{
		FormatVersion:  formatVersionV1,
		PageSize:       uint32(DefaultPageSize),
		LastWALSeq:     42,
		NextPageID:     7,
		BTreeRoot:      3,
		Features:       0,
		EncryptionSalt: [16]byte{},
	}
	page := encodeHeaderPage(h)
	if len(page) != DefaultPageSize {
		t.Fatalf("encodeHeaderPage len: got %d want %d", len(page), DefaultPageSize)
	}
	got, err := decodeHeaderPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("decode: got %+v want %+v", got, h)
	}
}

func TestHeader_roundTrip_v2_commitSeq(t *testing.T) {
	t.Parallel()
	h := Header{
		FormatVersion:  formatVersionV2,
		PageSize:       uint32(DefaultPageSize),
		LastWALSeq:     1,
		NextPageID:     2,
		BTreeRoot:      1,
		Features:       0,
		EncryptionSalt: [16]byte{},
		CommitSeq:      42,
	}
	page := encodeHeaderPage(h)
	got, err := decodeHeaderPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("decode: got %+v want %+v", got, h)
	}
}

func TestDecodeHeaderPage_rejectsBadMagic(t *testing.T) {
	t.Parallel()
	page := make([]byte, DefaultPageSize)
	page[0] = 'x'
	_, err := decodeHeaderPage(page)
	if !errors.Is(err, ErrCorruptHeader) {
		t.Fatalf("want ErrCorruptHeader, got %v", err)
	}
}
