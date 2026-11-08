package main

import (
	"testing"
	"time"
)

func TestValidateConfigBounds(t *testing.T) {
	valid := config{samples: 1, batchSamples: 1, batchSize: 1, embeddingSamples: 1}
	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	invalid := valid
	invalid.embeddingWarmup = maxEmbeddingWarmup + 1
	if err := validateConfig(invalid); err == nil {
		t.Fatal("expected embedding warm-up bound error")
	}
}

func TestSummarize(t *testing.T) {
	got := summarize([]time.Duration{5, 1, 4, 2, 3})
	if got.MinNS != 1 || got.P50NS != 3 || got.P95NS != 4 || got.MaxNS != 5 || got.MeanNS != 3 {
		t.Fatalf("unexpected summary: %+v", got)
	}
}

func TestMeetsTargetUsesDeclaredUnit(t *testing.T) {
	target := targetForCells(100)
	report := workloadReport{
		Latency:            latencySummary{P95NS: target.P95NS},
		CellsPerSecond:     target.ThroughputAtLeast,
		AllocBytesPerCell:  target.AllocBytesPerUnitAtMost,
		GrowthBytesPerCell: target.GrowthBytesPerUnitAtMost,
		WALSyncsPerCommit:  target.WALSyncsPerCommitAtMost,
	}
	if !meetsTarget(report, target) {
		t.Fatal("expected report at every boundary to meet target")
	}
}
