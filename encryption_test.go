package hexxladb

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"

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

func TestEncryption_AuthenticatedPageRoundTripAndTamper(t *testing.T) {
	t.Parallel()
	master := bytes.Repeat([]byte{0x3c}, authenticatedMasterBytes)
	salt := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	hooks, err := buildAuthenticatedEncryptionHooks(master, salt)
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte{0x5a}, engine.DefaultPageSize)
	encoded, err := hooks.BeforeWriteVersioned(7, 42, plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != len(plain)+engine.AuthenticatedPageOverhead {
		t.Fatalf("encoded length = %d", len(encoded))
	}
	if binary.BigEndian.Uint64(encoded[:8]) != 42 || bytes.Contains(encoded, plain[:64]) {
		t.Fatal("authenticated envelope did not preserve generation or exposed plaintext")
	}
	decoded, err := hooks.AfterRead(7, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, plain) {
		t.Fatal("authenticated page round trip mismatch")
	}

	for _, tc := range []struct {
		name   string
		offset int
	}{
		{name: "generation", offset: 0},
		{name: "nonce", offset: 8},
		{name: "ciphertext", offset: pageEnvelopePrefixBytes},
		{name: "tag", offset: len(encoded) - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modified := bytes.Clone(encoded)
			modified[tc.offset] ^= 0x80
			if _, err := hooks.AfterRead(7, modified); !errors.Is(err, engine.ErrPageAuthentication) {
				t.Fatalf("error = %v, want ErrPageAuthentication", err)
			}
		})
	}
	if _, err := hooks.AfterRead(8, encoded); !errors.Is(err, engine.ErrPageAuthentication) {
		t.Fatalf("page swap error = %v, want ErrPageAuthentication", err)
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

func BenchmarkEncryptionPageTransform(b *testing.B) {
	key := bytes.Repeat([]byte{0x42}, 64)
	legacy, err := buildEncryptionHooks(key)
	if err != nil {
		b.Fatal(err)
	}
	authenticated, err := chacha20poly1305.NewX(key[:chacha20poly1305.KeySize])
	if err != nil {
		b.Fatal(err)
	}
	for _, pageSize := range []int{4 << 10, 64 << 10} {
		plain := bytes.Repeat([]byte{0x5a}, pageSize)
		var aad [32]byte
		binary.BigEndian.PutUint64(aad[16:24], 42)
		binary.BigEndian.PutUint64(aad[24:], 7)
		legacyCiphertext, err := legacy.BeforeWrite(42, plain)
		if err != nil {
			b.Fatal(err)
		}
		nonce := bytes.Repeat([]byte{0x24}, chacha20poly1305.NonceSizeX)
		authenticatedCiphertext := authenticated.Seal(nil, nonce, plain, aad[:])
		b.Run(fmt.Sprintf("xts/seal/%d", pageSize), func(b *testing.B) {
			b.SetBytes(int64(pageSize))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := legacy.BeforeWrite(42, plain); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("xchacha20poly1305/seal/%d", pageSize), func(b *testing.B) {
			b.SetBytes(int64(pageSize))
			b.ReportAllocs()
			for b.Loop() {
				nonce := make([]byte, chacha20poly1305.NonceSizeX)
				if _, err := rand.Read(nonce); err != nil {
					b.Fatal(err)
				}
				authenticated.Seal(nil, nonce, plain, aad[:])
			}
		})
		b.Run(fmt.Sprintf("xts/open/%d", pageSize), func(b *testing.B) {
			b.SetBytes(int64(pageSize))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := legacy.AfterRead(42, legacyCiphertext); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("xchacha20poly1305/open/%d", pageSize), func(b *testing.B) {
			b.SetBytes(int64(pageSize))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := authenticated.Open(nil, nonce, authenticatedCiphertext, aad[:]); err != nil {
					b.Fatal(err)
				}
			}
		})
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
