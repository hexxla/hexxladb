// Vector Scale Evidence runs a bounded, deterministic embedding workload and
// emits aggregate JSON for build, query, recall, reopen, churn, memory, and
// storage behavior. It creates and removes only a temporary database.
//
// Usage:
//
//	go run ./examples/vector_scale_evidence -cells 10000 -dimension 32
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
	maxVectorCells   = 100_000
	maxVectorSamples = 1_000
	maxVectorBatch   = 1_000
)

type config struct {
	cells      int
	dimension  int
	samples    int
	topK       int
	churn      int
	batchSize  int
	seed       uint64
	pageSize   uint
	cacheBytes int64
	buildMode  string
}

type latencySummary struct {
	Samples int64 `json:"samples"`
	MinNS   int64 `json:"min_ns"`
	P50NS   int64 `json:"p50_ns"`
	P95NS   int64 `json:"p95_ns"`
	MaxNS   int64 `json:"max_ns"`
	MeanNS  int64 `json:"mean_ns"`
}

type queryReport struct {
	Latency      latencySummary               `json:"latency"`
	ExactLatency latencySummary               `json:"exact_oracle_latency"`
	RecallAtK    float64                      `json:"recall_at_k"`
	Path         hexxladb.EmbeddingSearchPath `json:"path"`
	EfSearch     int                          `json:"ef_search"`
}

type evidenceReport struct {
	SchemaVersion int `json:"schema_version"`
	Runtime       struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	} `json:"runtime"`
	Workload struct {
		Cells      int    `json:"cells"`
		Dimension  int    `json:"dimension"`
		Samples    int    `json:"samples"`
		TopK       int    `json:"top_k"`
		Churn      int    `json:"updates_and_deletes"`
		BatchSize  int    `json:"batch_size"`
		Seed       uint64 `json:"seed"`
		PageSize   uint   `json:"page_size"`
		CacheBytes int64  `json:"cache_bytes"`
		BuildMode  string `json:"build_mode"`
	} `json:"workload"`
	Build struct {
		Latency                latencySummary               `json:"batch_latency"`
		DurationNS             int64                        `json:"duration_ns"`
		VectorsPerSecond       float64                      `json:"vectors_per_second"`
		IngestDurationNS       int64                        `json:"ingest_duration_ns"`
		IngestVectorsPerSecond float64                      `json:"ingest_vectors_per_second"`
		RebuildDurationNS      int64                        `json:"rebuild_duration_ns"`
		PublishDurationNS      int64                        `json:"publish_duration_ns"`
		PathBeforeRebuild      hexxladb.EmbeddingSearchPath `json:"path_before_rebuild,omitempty"`
	} `json:"build"`
	BeforeReopen queryReport `json:"before_reopen"`
	Reopen       struct {
		DurationNS int64       `json:"duration_ns"`
		Query      queryReport `json:"query"`
	} `json:"reopen"`
	Churn struct {
		Updates          int            `json:"updates"`
		Deletes          int            `json:"deletes"`
		UpdateLatency    latencySummary `json:"update_batch_latency"`
		DeleteLatency    latencySummary `json:"delete_batch_latency"`
		ReopenDurationNS int64          `json:"reopen_duration_ns"`
		Query            queryReport    `json:"query_after_reopen"`
	} `json:"churn"`
	Resources struct {
		TotalAllocBytes     uint64 `json:"total_alloc_bytes"`
		PeakHeapInuseBuild  uint64 `json:"peak_heap_inuse_during_build"`
		HeapInuseAfterBuild uint64 `json:"heap_inuse_after_build"`
		HeapInuseAfterChurn uint64 `json:"heap_inuse_after_churn"`
		PrimaryBytes        uint64 `json:"primary_bytes"`
		WALBytes            uint64 `json:"wal_bytes"`
		LiveBytes           uint64 `json:"live_bytes"`
		ReclaimableBytes    uint64 `json:"reclaimable_bytes"`
		ChangelogBytes      uint64 `json:"changelog_bytes"`
	} `json:"resources"`
}

type vectorSet struct {
	coords  []hexxladb.PackedCoord
	vectors [][]float32
	active  []bool
}

func main() {
	cfg := config{}
	flag.IntVar(&cfg.cells, "cells", 10_000, "vectors to build (100..100000)")
	flag.IntVar(&cfg.dimension, "dimension", 32, "vector dimension (1..65535)")
	flag.IntVar(&cfg.samples, "samples", 25, "query samples (1..1000)")
	flag.IntVar(&cfg.topK, "top-k", 10, "neighbors used for latency and recall")
	flag.IntVar(&cfg.churn, "churn", 100, "vectors to update and vectors to delete")
	flag.IntVar(&cfg.batchSize, "batch-size", 500, "vectors per transaction (1..1000)")
	flag.Uint64Var(&cfg.seed, "seed", 1, "deterministic vector seed")
	flag.UintVar(&cfg.pageSize, "page-size", 4096, "new database page size")
	flag.Int64Var(&cfg.cacheBytes, "cache-bytes", 64<<20, "page-cache byte budget (-1 disables)")
	flag.StringVar(&cfg.buildMode, "build-mode", "deferred-rebuild", "graph build mode: synchronous or deferred-rebuild")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vector scale evidence: %v\n", err)
		os.Exit(2)
	}
	report, err := run(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vector scale evidence: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "vector scale evidence: encode report: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(cfg config) error {
	if cfg.cells < 100 || cfg.cells > maxVectorCells {
		return fmt.Errorf("cells must be between 100 and %d", maxVectorCells)
	}
	if cfg.dimension < 1 || cfg.dimension > math.MaxUint16 {
		return fmt.Errorf("dimension must be between 1 and %d", uint16(math.MaxUint16))
	}
	if cfg.samples < 1 || cfg.samples > maxVectorSamples {
		return fmt.Errorf("samples must be between 1 and %d", maxVectorSamples)
	}
	if cfg.topK < 1 || cfg.topK > cfg.cells {
		return errors.New("top-k must be between 1 and cells")
	}
	if cfg.churn < 0 || cfg.churn*2 >= cfg.cells {
		return errors.New("churn must be non-negative and leave at least one active vector")
	}
	if cfg.batchSize < 1 || cfg.batchSize > maxVectorBatch {
		return fmt.Errorf("batch-size must be between 1 and %d", maxVectorBatch)
	}
	if !slices.Contains([]uint{4096, 8192, 16384, 65536}, cfg.pageSize) {
		return errors.New("page-size must be one of 4096, 8192, 16384, or 65536")
	}
	if cfg.cacheBytes < -1 {
		return errors.New("cache-bytes must be -1, 0, or positive")
	}
	if cfg.buildMode != "synchronous" && cfg.buildMode != "deferred-rebuild" {
		return errors.New("build-mode must be synchronous or deferred-rebuild")
	}
	if cfg.buildMode == "deferred-rebuild" && cfg.cells > hexxladb.MaxEmbeddingIndexRebuildVectors {
		return fmt.Errorf("deferred-rebuild cells must not exceed %d", hexxladb.MaxEmbeddingIndexRebuildVectors)
	}
	return nil
}

func run(cfg config) (evidenceReport, error) {
	var report evidenceReport
	tempDir, err := os.MkdirTemp("", "hexxladb-vector-evidence-")
	if err != nil {
		return report, fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	dbPath := filepath.Join(tempDir, "vectors.db")
	opts := &hexxladb.Options{
		EmbeddingDimension: uint16(cfg.dimension), //nolint:gosec // validated above
		DistanceMetric:     hexxladb.DistanceCosine,
		PageSize:           uint32(cfg.pageSize), //nolint:gosec // validated page-size set
		PageCacheSize:      cfg.cacheBytes,
	}

	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	set, queries, err := generateWorkload(cfg)
	if err != nil {
		return report, err
	}
	db, err := hexxladb.Open(dbPath, opts)
	if err != nil {
		return report, fmt.Errorf("open temporary database: %w", err)
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	stopHeapSampler := startHeapSampler()
	heapSamplerStopped := false
	defer func() {
		if !heapSamplerStopped {
			_ = stopHeapSampler()
		}
	}()
	buildStarted := time.Now()
	ingestStarted := time.Now()
	deferred := cfg.buildMode == "deferred-rebuild"
	buildLatency, err := putVectors(db, set.coords, set.vectors, cfg.batchSize, deferred)
	if err != nil {
		return report, fmt.Errorf("build graph: %w", err)
	}
	ingestDuration := time.Since(ingestStarted)
	var rebuildStats hexxladb.EmbeddingIndexRebuildStats
	var pathBeforeRebuild hexxladb.EmbeddingSearchPath
	if deferred {
		pathBeforeRebuild, err = searchPath(db, queries[0], cfg.topK)
		if err != nil {
			return report, fmt.Errorf("query before rebuild: %w", err)
		}
		if pathBeforeRebuild != hexxladb.EmbeddingSearchPathFlat {
			return report, fmt.Errorf("search path before rebuild %q, want flat", pathBeforeRebuild)
		}
		rebuildStats, err = db.RebuildEmbeddingIndex(context.Background(), &hexxladb.EmbeddingIndexRebuildOptions{MaxVectors: cfg.cells})
		if err != nil {
			return report, fmt.Errorf("rebuild graph: %w", err)
		}
	}
	buildDuration := time.Since(buildStarted)
	peakHeapInuseBuild := stopHeapSampler()
	heapSamplerStopped = true
	runtime.GC()
	var memoryAfterBuild runtime.MemStats
	runtime.ReadMemStats(&memoryAfterBuild)

	beforeReopen, err := measureQueries(db, set, queries, cfg.topK)
	if err != nil {
		return report, fmt.Errorf("query before reopen: %w", err)
	}
	if err := db.Close(); err != nil {
		return report, fmt.Errorf("close before reopen: %w", err)
	}
	db = nil

	reopenStarted := time.Now()
	db, err = hexxladb.Open(dbPath, opts)
	if err != nil {
		return report, fmt.Errorf("reopen database: %w", err)
	}
	reopenDuration := time.Since(reopenStarted)
	afterReopen, err := measureQueries(db, set, queries, cfg.topK)
	if err != nil {
		return report, fmt.Errorf("query after reopen: %w", err)
	}

	updateLatency, deleteLatency, err := applyChurn(db, set, cfg)
	if err != nil {
		return report, err
	}
	if err := db.Close(); err != nil {
		return report, fmt.Errorf("close after churn: %w", err)
	}
	db = nil
	churnReopenStarted := time.Now()
	db, err = hexxladb.Open(dbPath, opts)
	if err != nil {
		return report, fmt.Errorf("reopen after churn: %w", err)
	}
	churnReopenDuration := time.Since(churnReopenStarted)
	afterChurn, err := measureQueries(db, set, queries, cfg.topK)
	if err != nil {
		return report, fmt.Errorf("query after churn: %w", err)
	}
	storage, err := db.StorageStats()
	if err != nil {
		return report, fmt.Errorf("storage stats: %w", err)
	}
	runtime.GC()
	var memoryAfterChurn runtime.MemStats
	runtime.ReadMemStats(&memoryAfterChurn)
	if err := db.Close(); err != nil {
		return report, fmt.Errorf("close after churn: %w", err)
	}
	db = nil

	report.SchemaVersion = 1
	report.Runtime.GoVersion = runtime.Version()
	report.Runtime.GOOS = runtime.GOOS
	report.Runtime.GOARCH = runtime.GOARCH
	report.Workload.Cells = cfg.cells
	report.Workload.Dimension = cfg.dimension
	report.Workload.Samples = cfg.samples
	report.Workload.TopK = cfg.topK
	report.Workload.Churn = cfg.churn
	report.Workload.BatchSize = cfg.batchSize
	report.Workload.Seed = cfg.seed
	report.Workload.PageSize = cfg.pageSize
	report.Workload.CacheBytes = cfg.cacheBytes
	report.Workload.BuildMode = cfg.buildMode
	report.Build.Latency = summarize(buildLatency)
	report.Build.DurationNS = buildDuration.Nanoseconds()
	report.Build.VectorsPerSecond = float64(cfg.cells) / buildDuration.Seconds()
	report.Build.IngestDurationNS = ingestDuration.Nanoseconds()
	report.Build.IngestVectorsPerSecond = float64(cfg.cells) / ingestDuration.Seconds()
	report.Build.RebuildDurationNS = rebuildStats.BuildDuration.Nanoseconds()
	report.Build.PublishDurationNS = rebuildStats.PublishDuration.Nanoseconds()
	report.Build.PathBeforeRebuild = pathBeforeRebuild
	report.BeforeReopen = beforeReopen
	report.Reopen.DurationNS = reopenDuration.Nanoseconds()
	report.Reopen.Query = afterReopen
	report.Churn.Updates = cfg.churn
	report.Churn.Deletes = cfg.churn
	report.Churn.UpdateLatency = summarize(updateLatency)
	report.Churn.DeleteLatency = summarize(deleteLatency)
	report.Churn.ReopenDurationNS = churnReopenDuration.Nanoseconds()
	report.Churn.Query = afterChurn
	report.Resources.TotalAllocBytes = memoryAfterChurn.TotalAlloc - memoryBefore.TotalAlloc
	report.Resources.PeakHeapInuseBuild = peakHeapInuseBuild
	report.Resources.HeapInuseAfterBuild = memoryAfterBuild.HeapInuse
	report.Resources.HeapInuseAfterChurn = memoryAfterChurn.HeapInuse
	report.Resources.PrimaryBytes = storage.PrimaryBytes
	report.Resources.WALBytes = storage.WALBytes
	report.Resources.LiveBytes = storage.LiveBytes
	report.Resources.ReclaimableBytes = storage.ReclaimableBytes
	report.Resources.ChangelogBytes = storage.ChangelogBytes
	return report, nil
}

func generateWorkload(cfg config) (*vectorSet, [][]float32, error) {
	rng := newGenerator(cfg.seed)
	set := &vectorSet{
		coords:  make([]hexxladb.PackedCoord, cfg.cells),
		vectors: make([][]float32, cfg.cells),
		active:  make([]bool, cfg.cells),
	}
	for i := range cfg.cells {
		coord, err := hexxladb.Pack(hexxladb.Coord{Q: i / 1_000, R: i % 1_000})
		if err != nil {
			return nil, nil, fmt.Errorf("pack vector %d: %w", i, err)
		}
		set.coords[i] = coord
		set.vectors[i] = randomUnitVector(rng, cfg.dimension)
		set.active[i] = true
	}
	queries := make([][]float32, cfg.samples)
	for i := range queries {
		queries[i] = randomUnitVector(rng, cfg.dimension)
	}
	return set, queries, nil
}

func putVectors(db *hexxladb.DB, coords []hexxladb.PackedCoord, vectors [][]float32, batchSize int, deferred bool) ([]time.Duration, error) {
	latencies := make([]time.Duration, 0, (len(coords)+batchSize-1)/batchSize)
	for start := 0; start < len(coords); start += batchSize {
		end := min(start+batchSize, len(coords))
		started := time.Now()
		err := db.Update(func(tx *hexxladb.Tx) error {
			for i := start; i < end; i++ {
				if err := tx.PutEmbeddingWithOptions(coords[i], vectors[i], hexxladb.EmbeddingWriteOptions{DeferIndexMaintenance: deferred}); err != nil {
					return err
				}
			}
			return nil
		})
		latencies = append(latencies, time.Since(started))
		if err != nil {
			return latencies, fmt.Errorf("vectors %d..%d: %w", start, end, err)
		}
	}
	return latencies, nil
}

func applyChurn(db *hexxladb.DB, set *vectorSet, cfg config) ([]time.Duration, []time.Duration, error) {
	rng := newGenerator(cfg.seed ^ 0xa0761d6478bd642f)
	updates := make([][]float32, cfg.churn)
	for i := range updates {
		updates[i] = randomUnitVector(rng, cfg.dimension)
	}
	updateLatency, err := putVectors(db, set.coords[:cfg.churn], updates, cfg.batchSize, false)
	if err != nil {
		return nil, nil, fmt.Errorf("update churn: %w", err)
	}
	copy(set.vectors[:cfg.churn], updates)

	deleteLatency := make([]time.Duration, 0, (cfg.churn+cfg.batchSize-1)/cfg.batchSize)
	deleteStart := cfg.churn
	deleteEnd := cfg.churn * 2
	for start := deleteStart; start < deleteEnd; start += cfg.batchSize {
		end := min(start+cfg.batchSize, deleteEnd)
		started := time.Now()
		err := db.Update(func(tx *hexxladb.Tx) error {
			for i := start; i < end; i++ {
				if err := tx.DeleteEmbedding(set.coords[i]); err != nil {
					return err
				}
			}
			return nil
		})
		deleteLatency = append(deleteLatency, time.Since(started))
		if err != nil {
			return nil, nil, fmt.Errorf("delete vectors %d..%d: %w", start, end, err)
		}
		for i := start; i < end; i++ {
			set.active[i] = false
		}
	}
	return updateLatency, deleteLatency, nil
}

func searchPath(db *hexxladb.DB, query []float32, topK int) (hexxladb.EmbeddingSearchPath, error) {
	var path hexxladb.EmbeddingSearchPath
	err := db.View(func(tx *hexxladb.Tx) error {
		_, stats, err := tx.SearchByEmbeddingWithStats(query, hexxladb.EmbeddingSearchConfig{MaxResults: topK})
		path = stats.Path
		return err
	})
	return path, err
}

func measureQueries(db *hexxladb.DB, set *vectorSet, queries [][]float32, topK int) (queryReport, error) {
	var report queryReport
	latencies := make([]time.Duration, 0, len(queries))
	exactLatencies := make([]time.Duration, 0, len(queries))
	var matches, possible int
	for _, query := range queries {
		var results []hexxladb.EmbeddingSearchResult
		var stats hexxladb.EmbeddingSearchStats
		started := time.Now()
		err := db.View(func(tx *hexxladb.Tx) error {
			var err error
			results, stats, err = tx.SearchByEmbeddingWithStats(query, hexxladb.EmbeddingSearchConfig{MaxResults: topK})
			return err
		})
		latencies = append(latencies, time.Since(started))
		if err != nil {
			return report, err
		}
		if stats.Path != hexxladb.EmbeddingSearchPathHNSW {
			return report, fmt.Errorf("search path %q, want HNSW", stats.Path)
		}
		if report.Path == "" {
			report.Path = stats.Path
			report.EfSearch = stats.EfSearch
		}

		started = time.Now()
		exact := exactTopK(set, query, topK)
		exactLatencies = append(exactLatencies, time.Since(started))
		want := make(map[hexxladb.PackedCoord]struct{}, len(exact))
		for _, coord := range exact {
			want[coord] = struct{}{}
		}
		for _, result := range results {
			if _, ok := want[result.Coord]; ok {
				matches++
			}
		}
		possible += len(exact)
	}
	report.Latency = summarize(latencies)
	report.ExactLatency = summarize(exactLatencies)
	if possible > 0 {
		report.RecallAtK = float64(matches) / float64(possible)
	}
	return report, nil
}

type exactHit struct {
	coord hexxladb.PackedCoord
	score float64
}

func exactTopK(set *vectorSet, query []float32, topK int) []hexxladb.PackedCoord {
	hits := make([]exactHit, 0, topK)
	for i, vector := range set.vectors {
		if !set.active[i] {
			continue
		}
		score := dot(query, vector)
		at, _ := slices.BinarySearchFunc(hits, score, func(hit exactHit, target float64) int {
			switch {
			case hit.score > target:
				return -1
			case hit.score < target:
				return 1
			default:
				return 0
			}
		})
		if at >= topK {
			continue
		}
		hits = slices.Insert(hits, at, exactHit{coord: set.coords[i], score: score})
		if len(hits) > topK {
			hits = hits[:topK]
		}
	}
	coords := make([]hexxladb.PackedCoord, len(hits))
	for i := range hits {
		coords[i] = hits[i].coord
	}
	return coords
}

func dot(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

type generator struct{ state uint64 }

func newGenerator(seed uint64) *generator { return &generator{state: seed} }

func (g *generator) float64() float64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return float64(g.state>>11) / float64(uint64(1)<<53)
}

func randomUnitVector(rng *generator, dimension int) []float32 {
	vector := make([]float32, dimension)
	var norm float64
	for i := range vector {
		value := rng.float64()*2 - 1
		vector[i] = float32(value)
		norm += value * value
	}
	norm = math.Sqrt(norm)
	for i := range vector {
		vector[i] /= float32(norm)
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
		MaxNS:   values[len(values)-1],
		MeanNS:  total / int64(len(values)),
	}
}

func percentile(sorted []int64, percent int) int64 {
	index := (len(sorted) - 1) * percent / 100
	return sorted[index]
}

func startHeapSampler() func() uint64 {
	stop := make(chan struct{})
	done := make(chan uint64, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var peak uint64
		read := func() {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			peak = max(peak, memory.HeapInuse)
		}
		read()
		for {
			select {
			case <-ticker.C:
				read()
			case <-stop:
				read()
				done <- peak
				return
			}
		}
	}()
	return func() uint64 {
		close(stop)
		return <-done
	}
}
