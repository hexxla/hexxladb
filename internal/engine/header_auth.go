package engine

import (
	"crypto/hmac"
	"crypto/sha256"
)

func authenticateHeader(h Header, key [32]byte) Header {
	h.AuthTag = [HeaderAuthTagLen]byte{}
	page := encodeHeaderPage(h)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(page[:headerPrefixSize])
	copy(h.AuthTag[:], mac.Sum(nil))
	return h
}

func verifyHeaderAuthentication(h Header, key [32]byte) bool {
	want := h.AuthTag
	authenticated := authenticateHeader(h, key)
	return hmac.Equal(want[:], authenticated.AuthTag[:])
}

func writeHeaderAtAuthenticated(w interface {
	WriteAt([]byte, int64) (int, error)
}, h Header, key [32]byte, enabled bool) error {
	if enabled {
		h = authenticateHeader(h, key)
	}
	return writeHeaderAt(w, h)
}
