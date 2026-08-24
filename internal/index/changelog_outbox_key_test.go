package index_test

import (
	"bytes"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestChangelogOutboxKey_roundTripAndBounds(t *testing.T) {
	logicalKey := []byte("cell/coordinate")
	key := index.ChangelogOutboxKey(42, 7, logicalKey)
	commitSeq, ordinal, gotKey, ok := index.ParseChangelogOutboxKey(key)
	if !ok || commitSeq != 42 || ordinal != 7 || !bytes.Equal(gotKey, logicalKey) {
		t.Fatalf("decoded key: ok=%v commit=%d ordinal=%d logical=%q", ok, commitSeq, ordinal, gotKey)
	}
	from, to := index.ChangelogOutboxBounds()
	if bytes.Compare(key, from) < 0 || bytes.Compare(key, to) > 0 {
		t.Fatalf("key %q outside [%q, %q]", key, from, to)
	}
}
