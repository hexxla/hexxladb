package hexxladb

import (
	"crypto/aes"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/xts"

	"github.com/hexxla/hexxladb/internal/engine"
)

const xtsKeyBytes = 64 // AES-256-XTS: two 256-bit keys

// buildEncryptionHooks returns AES-XTS page transforms for data pages (pageID >= 1).
// Page 0 (file header) is never encrypted.
func buildEncryptionHooks(xtsKey []byte) (*engine.PageHooks, error) {
	if len(xtsKey) != xtsKeyBytes {
		return nil, fmt.Errorf("hexxladb: internal xts key length %d", len(xtsKey))
	}
	cipher, err := xts.NewCipher(aes.NewCipher, xtsKey)
	if err != nil {
		return nil, err
	}
	return &engine.PageHooks{
		BeforeWrite: func(pageID uint64, plain []byte) ([]byte, error) {
			if pageID == 0 {
				return plain, nil
			}
			if len(plain) != engine.PageSize || engine.PageSize%16 != 0 {
				return nil, engine.ErrBadPageSize
			}
			out := make([]byte, len(plain))
			cipher.Encrypt(out, plain, pageID)
			return out, nil
		},
		AfterRead: func(pageID uint64, data []byte) ([]byte, error) {
			if pageID == 0 {
				return data, nil
			}
			if len(data) != engine.PageSize || engine.PageSize%16 != 0 {
				return nil, engine.ErrBadPageSize
			}
			out := make([]byte, len(data))
			cipher.Decrypt(out, data, pageID)
			return out, nil
		},
	}, nil
}

// deriveXTSKeyMaterial derives a 64-byte AES-XTS key from an arbitrary-length secret using HKDF-SHA256.
func deriveXTSKeyMaterial(secret, salt, info []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errors.New("hexxladb: empty encryption secret")
	}
	r := hkdf.New(sha256.New, secret, salt, info)
	out := make([]byte, xtsKeyBytes)
	if _, err := r.Read(out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeriveKeyFromPassphrase derives raw key material using Argon2id (for use with [Open] passphrase mode).
func DeriveKeyFromPassphrase(passphrase string, salt []byte) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("hexxladb: empty passphrase")
	}
	if len(salt) != 16 {
		return nil, errors.New("hexxladb: salt must be 16 bytes")
	}
	// OWASP-ish defaults for embedded DB (tune if needed).
	const time = 3
	const memory = 64 * 1024
	const threads = 4
	const keyLen = 32
	return argon2.IDKey([]byte(passphrase), salt, time, memory, threads, keyLen), nil
}
