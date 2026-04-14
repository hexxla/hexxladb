package engine

import (
	"errors"
	"testing"
)

func TestHeader_roundTrip(t *testing.T) {
	t.Parallel()
	h := Header{
		FormatVersion:  formatVersionV1,
		PageSize:       uint32(PageSize),
		LastWALSeq:     42,
		NextPageID:     7,
		BTreeRoot:      3,
		Features:       0,
		EncryptionSalt: [16]byte{},
	}
	page := encodeHeaderPage(h)
	if len(page) != PageSize {
		t.Fatalf("encodeHeaderPage len: got %d want %d", len(page), PageSize)
	}
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
	page := make([]byte, PageSize)
	page[0] = 'x'
	_, err := decodeHeaderPage(page)
	if !errors.Is(err, ErrCorruptHeader) {
		t.Fatalf("want ErrCorruptHeader, got %v", err)
	}
}
