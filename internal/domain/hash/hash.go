// Package hash provides SHA-256 helpers for message digests (pure; no I/O).
package hash

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain"
)

// SHA256Hex returns the lowercase hex-encoded SHA-256 of message.
func SHA256Hex(message string) (string, error) {
	if len(message) > domain.MaxContentLen {
		return "", domain.ErrContentTooLarge
	}
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:]), nil
}
