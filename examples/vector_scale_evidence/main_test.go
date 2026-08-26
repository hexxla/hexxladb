package main

import (
	"testing"

	"github.com/hexxla/hexxladb"
)

func TestValidateConfigBuildModes(t *testing.T) {
	base := config{cells: 100, dimension: 32, samples: 1, topK: 1, batchSize: 10, pageSize: 4096, cacheBytes: -1, buildMode: "synchronous"}
	if err := validateConfig(base); err != nil {
		t.Fatalf("synchronous config: %v", err)
	}
	base.buildMode = "deferred-rebuild"
	if err := validateConfig(base); err != nil {
		t.Fatalf("deferred config: %v", err)
	}
	base.cells = hexxladb.MaxEmbeddingIndexRebuildVectors + 1
	if err := validateConfig(base); err == nil {
		t.Fatal("expected deferred rebuild hard-bound error")
	}
}

func TestStartHeapSamplerStops(t *testing.T) {
	stop := startHeapSampler()
	if peak := stop(); peak == 0 {
		t.Fatal("expected non-zero heap sample")
	}
}
