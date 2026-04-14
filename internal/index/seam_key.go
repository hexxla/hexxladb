package index

import (
	"fmt"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/record"
)

// SeamPrefix is the ASCII prefix for primary seam keys per HEXXLA_DB.md.
const SeamPrefix = "seam/"

// SeamKey returns the full storage key seam/<ulid> for a valid ULID string.
func SeamKey(ulidStr string) ([]byte, error) {
	_, err := ulid.Parse(ulidStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", record.ErrInvalidULID, err)
	}
	out := make([]byte, 0, len(SeamPrefix)+len(ulidStr))
	out = append(out, SeamPrefix...)
	out = append(out, ulidStr...)
	return out, nil
}

// SeamScanUpperBound returns an upper bound for AscendRange over all seam/<ulid>
// keys (lexicographic): every valid seam key is strictly less than this value.
func SeamScanUpperBound() []byte {
	return []byte("seam0")
}
