package index_test

import (
	"testing"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/record"
)

func TestWeekBucketFromValidity_FloorsBeforeEpoch(t *testing.T) {
	nanos := int64(-1)
	bucket, ok := index.WeekBucketFromValidity(record.ValidityWire{ValidFrom: &nanos})
	if !ok || bucket != -1 {
		t.Fatalf("one nanosecond before epoch: bucket=%d ok=%v, want -1 true", bucket, ok)
	}
}
