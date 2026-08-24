package hexxladb

import (
	"bytes"
	"testing"

	"github.com/hexxla/hexxladb/internal/engine"
)

func TestEncryption_XTSRoundTrip(t *testing.T) {
	t.Parallel()
	key, err := deriveXTSKeyMaterial([]byte("test-secret-key-material"), nil, []byte(hkdfInfoXTS))
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := buildEncryptionHooks(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte{0xab}, engine.DefaultPageSize)
	cipher, err := hooks.BeforeWrite(42, append([]byte(nil), plain...))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(cipher, plain) {
		t.Fatal("expected ciphertext to differ from plaintext for pageID >= 1")
	}
	again, err := hooks.AfterRead(42, append([]byte(nil), cipher...))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, plain) {
		t.Fatalf("round-trip: %d bytes differ", countDiff(again, plain))
	}
}

func TestEncryption_headerPageNotTransformed(t *testing.T) {
	t.Parallel()
	key, err := deriveXTSKeyMaterial([]byte("k"), nil, []byte(hkdfInfoXTS))
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := buildEncryptionHooks(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, engine.DefaultPageSize)
	out, err := hooks.BeforeWrite(0, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatal("page 0 must pass through BeforeWrite unchanged")
	}
}

func TestEncryption_XTSCiphertextTamperIsNotAuthenticated(t *testing.T) {
	t.Parallel()
	key, err := deriveXTSKeyMaterial([]byte("tamper-evidence-key"), nil, []byte(hkdfInfoXTS))
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := buildEncryptionHooks(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte{0x5a}, engine.DefaultPageSize)
	ciphertext, err := hooks.BeforeWrite(7, plain)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[engine.DefaultPageSize/2] ^= 0x80
	altered, err := hooks.AfterRead(7, ciphertext)
	if err != nil {
		t.Fatalf("XTS unexpectedly authenticated modified ciphertext: %v", err)
	}
	if bytes.Equal(altered, plain) {
		t.Fatal("modified XTS ciphertext unexpectedly produced the original plaintext")
	}
}

func TestDeriveKeyFromPassphrase_roundTrip(t *testing.T) {
	t.Parallel()
	salt := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	a, err := DeriveKeyFromPassphrase("correct horse battery staple", salt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeriveKeyFromPassphrase("correct horse battery staple", salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("KDF not deterministic for same inputs")
	}
}

func countDiff(a, b []byte) int {
	n := 0
	for i := range a {
		if i < len(b) && a[i] != b[i] {
			n++
		}
	}
	return n
}
