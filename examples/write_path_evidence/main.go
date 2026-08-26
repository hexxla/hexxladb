// Write Path Evidence Example
//
// Runs bounded MVCC write workloads and emits aggregate JSON. Temporary
// databases are removed before exit; cell content, coordinates, and paths are
// not included in the report.
//
// Usage:
//
//	go run ./examples/write_path_evidence
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/hexxla/hexxladb"
)

const (
	defaultSamples         = 100
	defaultBatchSamples    = 25
	defaultBatchSize       = 100
	defaultWarmupCells     = 200
	defaultEmbeddingSample = 25
	defaultEmbeddingWarmup = 200
	maxSamples             = 1_000
	maxBatchSize           = 500
	maxWarmupCells         = 10_000
	maxEmbeddingWarmup     = 2_000
	embeddingDimension     = 32
)

type config struct {
	samples          int
	batchSamples     int
	batchSize        int
	warmupCells      int
	embeddingSamples int
	embeddingWarmup  int
}

type latencySummary struct {
	Samples int64 `json:"samples"`
	MinNS   int64 `json:"min_ns"`
	P50NS   int64 `json:"p50_ns"`
	P95NS   int64 `json:"p95_ns"`
	P99NS   int64 `json:"p99_ns"`
	MaxNS   int64 `json:"max_ns"`
	MeanNS  int64 `json:"mean_ns"`
}

type phaseSummary struct {
	Calls                 uint64 `json:"calls"`
	Commits               uint64 `json:"commits"`
	LockWaitNSPerCall     int64  `json:"lock_wait_ns_per_call"`
	CallbackNSPerCall     int64  `json:"callback_ns_per_call"`
	DurabilityNSPerCall   int64  `json:"durability_ns_per_call"`
	FinalizationNSPerCall int64  `json:"finalization_ns_per_call"`
}

type referenceTarget struct {
	P95NS                    int64   `json:"p95_ns_at_most"`
	ThroughputAtLeast        float64 `json:"throughput_at_least"`
	ThroughputUnit           string  `json:"throughput_unit"`
	AllocBytesPerUnitAtMost  uint64  `json:"alloc_bytes_per_unit_at_most"`
	WALSyncsPerCommitAtMost  float64 `json:"wal_syncs_per_commit_at_most"`
	GrowthBytesPerUnitAtMost int64   `json:"growth_bytes_per_unit_at_most"`
}

type workloadReport struct {
	Latency              latencySummary  `json:"latency"`
	ElapsedNS            int64           `json:"elapsed_ns"`
	CommitsPerSecond     float64         `json:"commits_per_second"`
	CellsPerSecond       float64         `json:"cells_per_second"`
	AllocBytesPerCommit  uint64          `json:"alloc_bytes_per_commit"`
	AllocBytesPerCell    uint64          `json:"alloc_bytes_per_cell"`
	HeapAllocBytesAfter  uint64          `json:"heap_alloc_bytes_after"`
	PrimaryGrowthBytes   int64           `json:"primary_growth_bytes"`
	GrowthBytesPerCommit int64           `json:"growth_bytes_per_commit"`
	GrowthBytesPerCell   int64           `json:"growth_bytes_per_cell"`
	WritePhases          phaseSummary    `json:"write_phases"`
	ApplyBatches         uint64          `json:"apply_batches"`
	MultiJobBatches      uint64          `json:"multi_job_batches"`
	WALSyncs             uint64          `json:"wal_syncs"`
	WALSyncsPerCommit    float64         `json:"wal_syncs_per_commit"`
	ReferenceTarget      referenceTarget `json:"reference_target"`
	MeetsReferenceTarget bool            `json:"meets_reference_target"`
}

type evidenceReport struct {
	SchemaVersion int `json:"schema_version"`
	Runtime       struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	} `json:"runtime"`
	Workload struct {
		Samples          int `json:"single_samples"`
		BatchSamples     int `json:"batch_samples"`
		BatchSize        int `json:"batch_size"`
		WarmupCells      int `json:"warmup_cells"`
		EmbeddingSamples int `json:"embedding_samples"`
		EmbeddingWarmup  int `json:"embedding_warmup"`
		EmbeddingDims    int `json:"embedding_dimensions"`
	} `json:"workload"`
	SingleCell    workloadReport `json:"single_cell"`
	BatchCells    workloadReport `json:"batch_cells"`
	CellEmbedding workloadReport `json:"cell_embedding_32d"`
}

func main() {
	cfg := config{}
	flag.IntVar(&cfg.samples, "samples", defaultSamples, "measured single-cell commits (1..1000)")
	flag.IntVar(&cfg.batchSamples, "batch-samples", defaultBatchSamples, "measured batch commits (1..1000)")
	flag.IntVar(&cfg.batchSize, "batch-size", defaultBatchSize, "cells per batch commit (1..500)")
	flag.IntVar(&cfg.warmupCells, "warmup-cells", defaultWarmupCells, "ordinary cells written before measurement (0..10000)")
	flag.IntVar(&cfg.embeddingSamples, "embedding-samples", defaultEmbeddingSample, "measured cell-plus-embedding commits (1..1000)")
	flag.IntVar(&cfg.embeddingWarmup, "embedding-warmup", defaultEmbeddingWarmup, "vectors written before measurement (0..2000)")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write-path evidence: %v\n", err)
		os.Exit(2)
	}
	report, err := run(context.Background(), cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write-path evidence: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write-path evidence: encode report: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(cfg config) error {
	if cfg.samples < 1 || cfg.samples > maxSamples {
		return fmt.Errorf("samples must be between 1 and %d", maxSamples)
	}
	if cfg.batchSamples < 1 || cfg.batchSamples > maxSamples {
		return fmt.Errorf("batch-samples must be between 1 and %d", maxSamples)
	}
	if cfg.batchSize < 1 || cfg.batchSize > maxBatchSize {
		return fmt.Errorf("batch-size must be between 1 and %d", maxBatchSize)
	}
	if cfg.warmupCells < 0 || cfg.warmupCells > maxWarmupCells {
		return fmt.Errorf("warmup-cells must be between 0 and %d", maxWarmupCells)
	}
	if cfg.embeddingSamples < 1 || cfg.embeddingSamples > maxSamples {
		return fmt.Errorf("embedding-samples must be between 1 and %d", maxSamples)
	}
	if cfg.embeddingWarmup < 0 || cfg.embeddingWarmup > maxEmbeddingWarmup {
		return fmt.Errorf("embedding-warmup must be between 0 and %d", maxEmbeddingWarmup)
	}
	return nil
}

func run(ctx context.Context, cfg config) (evidenceReport, error) {
	var report evidenceReport
	single, err := runCellWorkload(ctx, cfg.warmupCells, cfg.samples, 1)
	if err != nil {
		return report, fmt.Errorf("single-cell workload: %w", err)
	}
	batch, err := runCellWorkload(ctx, cfg.warmupCells, cfg.batchSamples, cfg.batchSize)
	if err != nil {
		return report, fmt.Errorf("batch workload: %w", err)
	}
	embedding, err := runEmbeddingWorkload(ctx, cfg.embeddingWarmup, cfg.embeddingSamples)
	if err != nil {
		return report, fmt.Errorf("embedding workload: %w", err)
	}

	report.SchemaVersion = 1
	report.Runtime.GoVersion = runtime.Version()
	report.Runtime.GOOS = runtime.GOOS
	report.Runtime.GOARCH = runtime.GOARCH
	report.Workload.Samples = cfg.samples
	report.Workload.BatchSamples = cfg.batchSamples
	report.Workload.BatchSize = cfg.batchSize
	report.Workload.WarmupCells = cfg.warmupCells
	report.Workload.EmbeddingSamples = cfg.embeddingSamples
	report.Workload.EmbeddingWarmup = cfg.embeddingWarmup
	report.Workload.EmbeddingDims = embeddingDimension
	report.SingleCell = single
	report.BatchCells = batch
	report.CellEmbedding = embedding
	return report, nil
}

func runCellWorkload(ctx context.Context, warmup, samples, batchSize int) (workloadReport, error) {
	dir, err := os.MkdirTemp("", "hexxladb-write-evidence-")
	if err != nil {
		return workloadReport{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "cells.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{EnableMVCC: true})
	if err != nil {
		return workloadReport{}, err
	}
	defer func() { _ = db.Close() }()

	next := 0
	for remaining := warmup; remaining > 0; {
		count := min(remaining, 100)
		if err := putCells(ctx, db, next, count); err != nil {
			return workloadReport{}, err
		}
		next += count
		remaining -= count
	}
	return measure(path, db, samples, batchSize, func() error {
		err := putCells(ctx, db, next, batchSize)
		next += batchSize
		return err
	}, targetForCells(batchSize))
}

func runEmbeddingWorkload(ctx context.Context, warmup, samples int) (workloadReport, error) {
	dir, err := os.MkdirTemp("", "hexxladb-write-evidence-")
	if err != nil {
		return workloadReport{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "embeddings.db")
	db, err := hexxladb.Open(path, &hexxladb.Options{
		EnableMVCC:         true,
		EmbeddingDimension: embeddingDimension,
		DistanceMetric:     hexxladb.DistanceCosine,
		PageSize:           4096,
	})
	if err != nil {
		return workloadReport{}, err
	}
	defer func() { _ = db.Close() }()

	for i := range warmup {
		if err := putEmbedding(ctx, db, i); err != nil {
			return workloadReport{}, err
		}
	}
	next := warmup
	return measure(path, db, samples, 1, func() error {
		err := putEmbedding(ctx, db, next)
		next++
		return err
	}, embeddingTarget())
}

func measure(path string, db *hexxladb.DB, samples, cellsPerCommit int, operation func() error, target referenceTarget) (workloadReport, error) {
	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	statsBefore := db.WriteStats()
	applyBefore, multiBefore, syncBefore := db.GroupWALStats()
	bytesBefore := fileSize(path)

	latencies := make([]time.Duration, 0, samples)
	started := time.Now()
	for range samples {
		operationStarted := time.Now()
		if err := operation(); err != nil {
			return workloadReport{}, err
		}
		latencies = append(latencies, time.Since(operationStarted))
	}
	elapsed := time.Since(started)
	statsAfter := db.WriteStats()
	applyAfter, multiAfter, syncAfter := db.GroupWALStats()
	var memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryAfter)

	commits := uint64(samples)                //nolint:gosec // samples is validated in [1, 1000].
	cells := commits * uint64(cellsPerCommit) //nolint:gosec // cellsPerCommit is validated in [1, 500].
	allocated := memoryAfter.TotalAlloc - memoryBefore.TotalAlloc
	growth := fileSize(path) - bytesBefore
	commitDivisor := int64(samples)
	cellDivisor := int64(samples * cellsPerCommit)
	report := workloadReport{
		Latency:              summarize(latencies),
		ElapsedNS:            elapsed.Nanoseconds(),
		CommitsPerSecond:     float64(commits) / elapsed.Seconds(),
		CellsPerSecond:       float64(cells) / elapsed.Seconds(),
		AllocBytesPerCommit:  allocated / max(commits, 1),
		AllocBytesPerCell:    allocated / max(cells, 1),
		HeapAllocBytesAfter:  memoryAfter.HeapAlloc,
		PrimaryGrowthBytes:   growth,
		GrowthBytesPerCommit: growth / commitDivisor,
		GrowthBytesPerCell:   growth / cellDivisor,
		WritePhases:          summarizePhases(statsBefore, statsAfter),
		ApplyBatches:         applyAfter - applyBefore,
		MultiJobBatches:      multiAfter - multiBefore,
		WALSyncs:             syncAfter - syncBefore,
		ReferenceTarget:      target,
	}
	report.WALSyncsPerCommit = float64(report.WALSyncs) / float64(max(commits, 1))
	report.MeetsReferenceTarget = meetsTarget(report, target)
	return report, nil
}

func putCells(ctx context.Context, db *hexxladb.DB, first, count int) error {
	return db.Update(func(tx *hexxladb.Tx) error {
		for offset := range count {
			packed, err := packedCoord(first + offset)
			if err != nil {
				return err
			}
			if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: packed, RawContent: "write-evidence"}); err != nil {
				return err
			}
		}
		return nil
	})
}

func putEmbedding(ctx context.Context, db *hexxladb.DB, index int) error {
	packed, err := packedCoord(index)
	if err != nil {
		return err
	}
	vector := evidenceVector(index)
	return db.Update(func(tx *hexxladb.Tx) error {
		if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: packed, RawContent: "write-evidence-vector"}); err != nil {
			return err
		}
		return tx.PutEmbedding(packed, vector)
	})
}

func packedCoord(index int) (hexxladb.PackedCoord, error) {
	return hexxladb.Pack(hexxladb.Coord{Q: index % 100_000, R: index / 100_000})
}

func evidenceVector(index int) []float32 {
	vector := make([]float32, embeddingDimension)
	state := uint64(index + 1) //nolint:gosec // index is bounded by the 3000-row evidence configuration.
	var norm float64
	for i := range vector {
		state = state*6364136223846793005 + 1442695040888963407
		value := float32(state>>32)/4_294_967_295*2 - 1
		vector[i] = value
		norm += float64(value * value)
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vector {
		vector[i] *= scale
	}
	return vector
}

func summarize(samples []time.Duration) latencySummary {
	if len(samples) == 0 {
		return latencySummary{}
	}
	values := make([]int64, len(samples))
	var total int64
	for i, sample := range samples {
		values[i] = sample.Nanoseconds()
		total += values[i]
	}
	slices.Sort(values)
	return latencySummary{
		Samples: int64(len(values)),
		MinNS:   values[0],
		P50NS:   percentile(values, 50),
		P95NS:   percentile(values, 95),
		P99NS:   percentile(values, 99),
		MaxNS:   values[len(values)-1],
		MeanNS:  total / int64(len(values)),
	}
}

func percentile(sorted []int64, percent int) int64 {
	return sorted[(len(sorted)-1)*percent/100]
}

func summarizePhases(before, after hexxladb.WriteStats) phaseSummary {
	calls := after.Calls - before.Calls
	if calls == 0 {
		return phaseSummary{}
	}
	callCount := int64(calls) //nolint:gosec // the measured interval is bounded to at most 1000 calls.
	return phaseSummary{
		Calls:                 calls,
		Commits:               after.Commits - before.Commits,
		LockWaitNSPerCall:     (after.LockWait - before.LockWait).Nanoseconds() / callCount,
		CallbackNSPerCall:     (after.Callback - before.Callback).Nanoseconds() / callCount,
		DurabilityNSPerCall:   (after.Durability - before.Durability).Nanoseconds() / callCount,
		FinalizationNSPerCall: (after.Finalization - before.Finalization).Nanoseconds() / callCount,
	}
}

func targetForCells(batchSize int) referenceTarget {
	if batchSize == 1 {
		return referenceTarget{P95NS: int64(25 * time.Millisecond), ThroughputAtLeast: 40, ThroughputUnit: "commits/s", AllocBytesPerUnitAtMost: 512 << 10, WALSyncsPerCommitAtMost: 1, GrowthBytesPerUnitAtMost: 2 << 20}
	}
	return referenceTarget{P95NS: int64(30 * time.Millisecond), ThroughputAtLeast: 4_000, ThroughputUnit: "cells/s", AllocBytesPerUnitAtMost: 256 << 10, WALSyncsPerCommitAtMost: 1, GrowthBytesPerUnitAtMost: 2 << 20}
}

func embeddingTarget() referenceTarget {
	return referenceTarget{P95NS: int64(100 * time.Millisecond), ThroughputAtLeast: 10, ThroughputUnit: "commits/s", AllocBytesPerUnitAtMost: 64 << 20, WALSyncsPerCommitAtMost: 1, GrowthBytesPerUnitAtMost: 8 << 20}
}

func meetsTarget(report workloadReport, target referenceTarget) bool {
	throughput := report.CommitsPerSecond
	allocation := report.AllocBytesPerCommit
	growth := report.GrowthBytesPerCommit
	if target.ThroughputUnit == "cells/s" {
		throughput = report.CellsPerSecond
		allocation = report.AllocBytesPerCell
		growth = report.GrowthBytesPerCell
	}
	return report.Latency.P95NS <= target.P95NS &&
		throughput >= target.ThroughputAtLeast &&
		allocation <= target.AllocBytesPerUnitAtMost &&
		report.WALSyncsPerCommit <= target.WALSyncsPerCommitAtMost &&
		growth <= target.GrowthBytesPerUnitAtMost
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		return 0
	}
	return info.Size()
}
