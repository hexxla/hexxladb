package index_test

import (
	"bytes"
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestChangelogConsumerEncoding(t *testing.T) {
	for _, id := range []string{"search-indexer", "projection.v2", "tenant_7:worker"} {
		if !index.ValidChangelogConsumerID(id) {
			t.Fatalf("ValidChangelogConsumerID(%q)=false", id)
		}
		key := index.ChangelogConsumerKey(id)
		got, ok := index.ParseChangelogConsumerKey(key)
		if !ok || got != id {
			t.Fatalf("ParseChangelogConsumerKey(%q)=(%q, %v)", key, got, ok)
		}
		from, to := index.ChangelogConsumerBounds()
		if bytes.Compare(key, from) < 0 || bytes.Compare(key, to) > 0 {
			t.Fatalf("consumer key %q outside bounds", key)
		}
	}

	invalid := []string{"", "-leading", "contains/slash", "has space", "é", string(make([]byte, index.ChangelogConsumerMaxIDBytes+1))}
	for _, id := range invalid {
		if index.ValidChangelogConsumerID(id) {
			t.Fatalf("ValidChangelogConsumerID(%q)=true", id)
		}
	}
}

func TestChangelogConsumerCursorEncodingRejectsCorruption(t *testing.T) {
	value := index.EncodeChangelogConsumerCursor(42)
	seq, ok := index.DecodeChangelogConsumerCursor(value)
	if !ok || seq != 42 {
		t.Fatalf("DecodeChangelogConsumerCursor=(%d, %v)", seq, ok)
	}
	value[3] ^= 0xff
	if _, ok := index.DecodeChangelogConsumerCursor(value); ok {
		t.Fatal("corrupt cursor decoded successfully")
	}
}

func TestChangelogProjectionCheckpointEncodingRejectsCorruption(t *testing.T) {
	var digest [32]byte
	for i := range digest {
		digest[i] = byte(i)
	}
	value := index.EncodeChangelogProjectionCheckpoint(99, digest)
	seq, gotDigest, ok := index.DecodeChangelogProjectionCheckpoint(value)
	if !ok || seq != 99 || gotDigest != digest {
		t.Fatalf("DecodeChangelogProjectionCheckpoint=(%d, %x, %v)", seq, gotDigest, ok)
	}
	value[10] ^= 0xff
	if _, _, ok := index.DecodeChangelogProjectionCheckpoint(value); ok {
		t.Fatal("corrupt checkpoint decoded successfully")
	}
}
