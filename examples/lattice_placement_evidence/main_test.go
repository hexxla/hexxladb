package main

import (
	"reflect"
	"testing"
)

func TestPlacementEvidenceIsDeterministicAndSeparatesQuality(t *testing.T) {
	t.Parallel()
	cfg := config{
		documentsPerTopic:  6,
		initialPerTopic:    4,
		neighborhoodRadius: 1,
		maxTokens:          512,
		semanticK:          4,
		seed:               7,
	}
	first, err := run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same seed and workload produced different evidence")
	}
	if first.Clustered.AfterIncremental.NeighborhoodPrecision <= first.Interleaved.AfterIncremental.NeighborhoodPrecision {
		t.Fatal("clustered placement did not improve neighborhood precision")
	}
}

func TestPlacementEvidenceRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	cfg := config{
		documentsPerTopic:  6,
		initialPerTopic:    6,
		neighborhoodRadius: 1,
		maxTokens:          512,
		semanticK:          4,
	}
	if err := validateConfig(cfg); err == nil {
		t.Fatal("expected invalid incremental split to fail")
	}
}
