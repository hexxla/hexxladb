package index_test

import (
	"crypto/rand"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/hexxla/hexxladb/internal/index"
)

func TestSeamTimeKey_roundTrip(t *testing.T) {
	t.Parallel()
	const bucket int64 = 42
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	k, err := index.SeamTimeKey(bucket, id)
	if err != nil {
		t.Fatal(err)
	}
	b, u, err := index.ParseSeamTimeKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if b != bucket || u != id {
		t.Fatalf("got bucket=%d ulid=%q", b, u)
	}
	from, to := index.SeamTimeRangePrefix(bucket)
	if string(from) >= string(to) {
		t.Fatal("range order")
	}
}

func TestSeamSourceKey_roundTrip(t *testing.T) {
	t.Parallel()
	id := ulid.MustNew(ulid.Now(), rand.Reader).String()
	k, err := index.SeamSourceKey("src-x", id)
	if err != nil {
		t.Fatal(err)
	}
	s, u, err := index.ParseSeamSourceKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if s != "src-x" || u != id {
		t.Fatalf("got source=%q ulid=%q", s, u)
	}
	from, to, err := index.SeamSourceRangePrefix("src-x")
	if err != nil {
		t.Fatal(err)
	}
	if string(from) >= string(to) {
		t.Fatal("range order")
	}
}
