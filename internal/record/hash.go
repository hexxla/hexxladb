package record

import (
	"crypto/sha256"
)

// HashRawContent returns SHA-256 of raw cell content (facet derivation anchor).
func HashRawContent(raw []byte) [32]byte {
	return sha256.Sum256(raw)
}
