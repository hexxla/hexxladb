package hexxladb

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"

	"github.com/hexxla/hexxladb/internal/engine"
)

const (
	authenticatedMasterBytes = 32
	hkdfInfoAuthenticatedV3  = "hexxladb-authenticated-master-v3"
	pageEnvelopePrefixBytes  = 8 + chacha20poly1305.NonceSizeX
)

// buildAuthenticatedEncryptionHooks returns format-v3 page transforms. The
// physical envelope is generation(8) || random nonce(24) || ciphertext || tag(16).
func buildAuthenticatedEncryptionHooks(master []byte, salt [16]byte) (*engine.PageHooks, error) {
	pageKey := deriveEncryptionSubkey(master, salt, "hexxladb-page-aead-v3")
	aead, err := chacha20poly1305.NewX(pageKey[:])
	clear(pageKey[:])
	if err != nil {
		return nil, err
	}
	return &engine.PageHooks{
		PhysicalPageOverhead: engine.AuthenticatedPageOverhead,
		BeforeWriteVersioned: func(pageID, generation uint64, plain []byte) ([]byte, error) {
			if pageID == 0 || !engine.IsValidPageSize(uint32(len(plain))) { //nolint:gosec // supported page sizes fit uint32.
				return nil, engine.ErrBadPageSize
			}
			out := make([]byte, pageEnvelopePrefixBytes, len(plain)+engine.AuthenticatedPageOverhead)
			binary.BigEndian.PutUint64(out[:8], generation)
			nonce := out[8:pageEnvelopePrefixBytes]
			if _, err := rand.Read(nonce); err != nil {
				return nil, err
			}
			aad := authenticatedPageAAD(salt, uint32(len(plain)), pageID, generation) //nolint:gosec // validated page size.
			return aead.Seal(out, nonce, plain, aad[:]), nil
		},
		AfterRead: func(pageID uint64, data []byte) ([]byte, error) {
			logicalSize := len(data) - engine.AuthenticatedPageOverhead
			if pageID == 0 || logicalSize < 0 || !engine.IsValidPageSize(uint32(logicalSize)) { //nolint:gosec // negative is checked first.
				return nil, engine.ErrBadPageSize
			}
			generation := binary.BigEndian.Uint64(data[:8])
			nonce := data[8:pageEnvelopePrefixBytes]
			ciphertext := data[pageEnvelopePrefixBytes:]
			aad := authenticatedPageAAD(salt, uint32(logicalSize), pageID, generation) //nolint:gosec // validated page size.
			plain, err := aead.Open(nil, nonce, ciphertext, aad[:])
			if err != nil {
				return nil, fmt.Errorf("%w: page %d", engine.ErrPageAuthentication, pageID)
			}
			return plain, nil
		},
	}, nil
}

func authenticatedPageAAD(salt [16]byte, pageSize uint32, pageID, generation uint64) [48]byte {
	var aad [48]byte
	copy(aad[:8], "HXPAGE03")
	copy(aad[8:24], salt[:])
	binary.BigEndian.PutUint32(aad[24:28], 3)
	binary.BigEndian.PutUint32(aad[28:32], pageSize)
	binary.BigEndian.PutUint64(aad[32:40], pageID)
	binary.BigEndian.PutUint64(aad[40:48], generation)
	return aad
}

func deriveAuthenticatedMaster(secret []byte) ([]byte, error) {
	if len(secret) == 0 {
		return nil, errors.New("hexxladb: empty encryption secret")
	}
	r := hkdf.New(sha256.New, secret, nil, []byte(hkdfInfoAuthenticatedV3))
	out := make([]byte, authenticatedMasterBytes)
	if _, err := r.Read(out); err != nil {
		return nil, err
	}
	return out, nil
}

func deriveEncryptionSubkey(master []byte, salt [16]byte, label string) [32]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(label))
	_, _ = mac.Write(salt[:])
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

func deriveHeaderMACKey(master []byte, salt [16]byte) [32]byte {
	return deriveEncryptionSubkey(master, salt, "hexxladb-header-mac-v3")
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

func deriveEncryptionKeyCheck(master []byte, salt [16]byte) [engine.HeaderEncryptionKeyCheckLen]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("hexxladb-m9-key-check-v1"))
	_, _ = mac.Write(salt[:])
	sum := mac.Sum(nil)
	var out [engine.HeaderEncryptionKeyCheckLen]byte
	copy(out[:], sum[:engine.HeaderEncryptionKeyCheckLen])
	return out
}

func deriveWALMACKey(master []byte) [32]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("hexxladb-m9-wal-mac-v1"))
	sum := mac.Sum(nil)
	var out [32]byte
	copy(out[:], sum[:32])
	return out
}

func deriveChangelogKey(master []byte, salt [16]byte) [32]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("hexxladb-changelog-master-v2"))
	_, _ = mac.Write(salt[:])
	sum := mac.Sum(nil)
	defer clear(sum)
	var out [32]byte
	copy(out[:], sum)
	return out
}
