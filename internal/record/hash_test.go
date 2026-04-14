package record_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/hexxla/hexxladb/internal/record"
)

func TestHashRawContent_golden(t *testing.T) {
	t.Parallel()
	// SHA-256("hello")
	const wantHex = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatal(err)
	}
	got := record.HashRawContent([]byte("hello"))
	if !bytes.Equal(got[:], want) {
		t.Fatalf("got %x want %x", got, want)
	}
}
