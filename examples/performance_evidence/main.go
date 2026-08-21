// Performance Evidence Example
//
// Runs a bounded, deterministic synthetic workload over Dijkstra pathfinding,
// deterministic FOV, and the super-hex occupancy prototype. It emits aggregate
// JSON only: counts, durations, result sizes, and storage/runtime metadata.
// Cell content and coordinates are never included in the report.
//
// Usage:
//
//	go run ./examples/performance_evidence -cells 2000 -samples 100 -seed 1
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/hexxla/hexxladb"
)

const (
	maxCells   = 100_000
	maxSamples = 10_000
	maxFOVRing = 512
	seedBatch  = 256
)

type config struct {
	cells         int
	samples       int
	seed          uint64
	fovRadius     int
	superHexLevel int
}

type latencySummary struct {
	Samples int64 `json:"samples"`
	MinNS   int64 `json:"min_ns"`
	P50NS   int64 `json:"p50_ns"`
	P95NS   int64 `json:"p95_ns"`
	MaxNS   int64 `json:"max_ns"`
	MeanNS  int64 `json:"mean_ns"`
}

type evidenceReport struct {
	SchemaVersion int `json:"schema_version"`
	Runtime       struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	} `json:"runtime"`
	Workload struct {
		Cells         int    `json:"cells"`
		Samples       int    `json:"samples"`
		Seed          uint64 `json:"seed"`
		FOVRadius     int    `json:"fov_radius"`
		SuperHexLevel int    `json:"superhex_level"`
	} `json:"workload"`
	Seed struct {
		Latency latencySummary `json:"latency"`
		Cells   int64          `json:"cells_written"`
		Edges   int64          `json:"edges_written"`
	} `json:"seed"`
	Dijkstra struct {
		Latency       latencySummary `json:"latency"`
		PathsFound    int64          `json:"paths_found"`
		TotalPathHops int64          `json:"total_path_hops"`
	} `json:"dijkstra"`
	FOV struct {
		Latency           latencySummary `json:"latency"`
		TotalVisibleCells int64          `json:"total_visible_cells"`
	} `json:"fov"`
	SuperHex struct {
		Rebuild          latencySummary `json:"rebuild"`
		Write            latencySummary `json:"write"`
		Sync             latencySummary `json:"sync"`
		ChangesProcessed int64          `json:"changes_processed"`
		Summaries        int            `json:"summaries"`
		LastSeq          uint64         `json:"last_seq"`
		CaughtUp         bool           `json:"caught_up"`
	} `json:"superhex"`
	Resources struct {
		TotalAllocBytes uint64 `json:"total_alloc_bytes"`
		DatabaseBytes   int64  `json:"database_bytes"`
		WALBytes        int64  `json:"wal_bytes"`
		ChangelogBytes  int64  `json:"changelog_bytes"`
	} `json:"resources"`
}

type workloadResults struct {
	seedLatency       []time.Duration
	seedCells         int64
	seedEdges         int64
	dijkstraLatency   []time.Duration
	pathsFound        int64
	totalPathHops     int64
	fovLatency        []time.Duration
	totalVisibleCells int64
	rebuildLatency    []time.Duration
	writeLatency      []time.Duration
	syncLatency       []time.Duration
	changesProcessed  int64
}

func main() {
	cfg := config{}
	flag.IntVar(&cfg.cells, "cells", 2000, "synthetic cells to seed (32..100000)")
	flag.IntVar(&cfg.samples, "samples", 100, "queries and incremental updates to observe (1..10000)")
	flag.Uint64Var(&cfg.seed, "seed", 1, "deterministic workload seed")
	flag.IntVar(&cfg.fovRadius, "fov-radius", 10, "field-of-view radius (0..512)")
	flag.IntVar(&cfg.superHexLevel, "superhex-level", 2, "aperture-7 hierarchy level (at least 1)")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "performance evidence: %v\n", err)
		os.Exit(2)
	}
	report, err := run(context.Background(), cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "performance evidence: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "performance evidence: encode report: %v\n", err)
		os.Exit(1)
	}
}

func validateConfig(cfg config) error {
	if cfg.cells < 32 || cfg.cells > maxCells {
		return fmt.Errorf("cells must be between 32 and %d", maxCells)
	}
	if cfg.samples < 1 || cfg.samples > maxSamples {
		return fmt.Errorf("samples must be between 1 and %d", maxSamples)
	}
	if cfg.fovRadius < 0 || cfg.fovRadius > maxFOVRing {
		return fmt.Errorf("fov-radius must be between 0 and %d", maxFOVRing)
	}
	if cfg.superHexLevel < 1 {
		return errors.New("superhex-level must be at least 1")
	}
	return nil
}

func run(ctx context.Context, cfg config) (evidenceReport, error) {
	var report evidenceReport
	tempDir, err := os.MkdirTemp("", "hexxladb-evidence-")
	if err != nil {
		return report, fmt.Errorf("create temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	dbPath := filepath.Join(tempDir, "workload.db")
	db, err := hexxladb.Open(dbPath, &hexxladb.Options{
		ChangelogEnabled: true,
		ChangelogLazy:    true,
	})
	if err != nil {
		return report, fmt.Errorf("open temporary database: %w", err)
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	coords := generateCoords(cfg.cells)
	results := workloadResults{}
	if err := seedDatabase(ctx, db, coords, &results); err != nil {
		return report, err
	}

	summaryIndex, err := hexxladb.NewSuperHexSummaryIndex(cfg.superHexLevel)
	if err != nil {
		return report, fmt.Errorf("construct super-hex index: %w", err)
	}
	started := time.Now()
	if err := summaryIndex.Rebuild(ctx, db); err != nil {
		return report, fmt.Errorf("rebuild super-hex index: %w", err)
	}
	results.rebuildLatency = append(results.rebuildLatency, time.Since(started))

	rng := newGenerator(cfg.seed)
	for sample := range cfg.samples {
		if err := observeDijkstra(ctx, db, coords, rng, &results); err != nil {
			return report, err
		}
		if err := observeFOV(ctx, db, coords, rng, cfg.fovRadius, &results); err != nil {
			return report, err
		}
		if err := observeSuperHex(ctx, db, summaryIndex, sample, &results); err != nil {
			return report, err
		}
	}
	remaining, err := db.ReadChangelogSince(summaryIndex.LastSeq(), 1)
	if err != nil {
		return report, fmt.Errorf("check super-hex lag: %w", err)
	}

	if err := db.Close(); err != nil {
		return report, fmt.Errorf("close temporary database: %w", err)
	}
	db = nil
	var memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryAfter)

	report.SchemaVersion = 1
	report.Runtime.GoVersion = runtime.Version()
	report.Runtime.GOOS = runtime.GOOS
	report.Runtime.GOARCH = runtime.GOARCH
	report.Workload.Cells = cfg.cells
	report.Workload.Samples = cfg.samples
	report.Workload.Seed = cfg.seed
	report.Workload.FOVRadius = cfg.fovRadius
	report.Workload.SuperHexLevel = cfg.superHexLevel
	report.Seed.Latency = summarize(results.seedLatency)
	report.Seed.Cells = results.seedCells
	report.Seed.Edges = results.seedEdges
	report.Dijkstra.Latency = summarize(results.dijkstraLatency)
	report.Dijkstra.PathsFound = results.pathsFound
	report.Dijkstra.TotalPathHops = results.totalPathHops
	report.FOV.Latency = summarize(results.fovLatency)
	report.FOV.TotalVisibleCells = results.totalVisibleCells
	report.SuperHex.Rebuild = summarize(results.rebuildLatency)
	report.SuperHex.Write = summarize(results.writeLatency)
	report.SuperHex.Sync = summarize(results.syncLatency)
	report.SuperHex.ChangesProcessed = results.changesProcessed
	report.SuperHex.Summaries = len(summaryIndex.Summaries())
	report.SuperHex.LastSeq = summaryIndex.LastSeq()
	report.SuperHex.CaughtUp = len(remaining) == 0
	report.Resources.TotalAllocBytes = memoryAfter.TotalAlloc - memoryBefore.TotalAlloc
	report.Resources.DatabaseBytes = fileSize(dbPath)
	report.Resources.WALBytes = fileSize(dbPath + "-wal")
	report.Resources.ChangelogBytes = fileSize(dbPath + "-changelog")
	return report, nil
}

// generator is a small deterministic PRNG whose sequence is stable across Go
// releases, keeping evidence workloads comparable when the toolchain changes.
type generator struct{ state uint64 }

func newGenerator(seed uint64) *generator { return &generator{state: seed} }

func (g *generator) intN(n int) int {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	value := int64(g.state >> 32)
	return int(value % int64(n))
}

func generateCoords(count int) []hexxladb.Coord {
	coords := make([]hexxladb.Coord, 0, count)
	for radius := 0; len(coords) < count; radius++ {
		coords = append(coords, hexxladb.Ring(hexxladb.Coord{}, radius)...)
	}
	return coords[:count]
}

func seedDatabase(ctx context.Context, db *hexxladb.DB, coords []hexxladb.Coord, results *workloadResults) error {
	for start := 0; start < len(coords); start += seedBatch {
		end := min(start+seedBatch, len(coords))
		batchCells := int64(0)
		batchEdges := int64(0)
		started := time.Now()
		err := db.Update(func(tx *hexxladb.Tx) error {
			for i := start; i < end; i++ {
				packed, err := hexxladb.Pack(coords[i])
				if err != nil {
					return err
				}
				if err := tx.PutCell(ctx, hexxladb.CellRecord{Key: packed, RawContent: "synthetic-evidence"}); err != nil {
					return err
				}
				batchCells++
				if i+1 < len(coords) {
					if err := tx.LinkCells(coords[i], coords[i+1], "evidence-route", 1, hexxladb.ProvenanceWire{}); err != nil {
						return err
					}
					batchEdges++
				}
				if i+8 < len(coords) {
					if err := tx.LinkCells(coords[i], coords[i+8], "evidence-route", 3, hexxladb.ProvenanceWire{}); err != nil {
						return err
					}
					batchEdges++
				}
			}
			return nil
		})
		results.seedLatency = append(results.seedLatency, time.Since(started))
		if err != nil {
			return fmt.Errorf("seed cells %d..%d: %w", start, end, err)
		}
		results.seedCells += batchCells
		results.seedEdges += batchEdges
	}
	return nil
}

func observeDijkstra(ctx context.Context, db *hexxladb.DB, coords []hexxladb.Coord, rng *generator, results *workloadResults) error {
	startIndex := rng.intN(len(coords) - 1)
	goalIndex := startIndex + 1 + rng.intN(len(coords)-startIndex-1)
	var path []hexxladb.Coord
	started := time.Now()
	err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		path, err = tx.FindEdgePath(ctx, coords[startIndex], coords[goalIndex], hexxladb.FindEdgePathConfig{Filter: "evidence-route"})
		return err
	})
	results.dijkstraLatency = append(results.dijkstraLatency, time.Since(started))
	if err != nil {
		return fmt.Errorf("dijkstra sample: %w", err)
	}
	if len(path) > 0 {
		results.pathsFound++
		results.totalPathHops += int64(len(path) - 1)
	}
	return nil
}

func observeFOV(ctx context.Context, db *hexxladb.DB, coords []hexxladb.Coord, rng *generator, radius int, results *workloadResults) error {
	center := coords[rng.intN(len(coords))]
	opaque := func(coord hexxladb.Coord) bool {
		if coord == center {
			return false
		}
		value := int64(coord.Q)*73_856_093 + int64(coord.R)*19_349_663
		return value%17 == 0
	}
	var visible []hexxladb.CellRecord
	started := time.Now()
	err := db.View(func(tx *hexxladb.Tx) error {
		var err error
		visible, err = tx.LoadContextFOV(ctx, center, radius, opaque, hexxladb.FOVContextConfig{MaxCells: 256})
		return err
	})
	results.fovLatency = append(results.fovLatency, time.Since(started))
	if err != nil {
		return fmt.Errorf("FOV sample: %w", err)
	}
	results.totalVisibleCells += int64(len(visible))
	return nil
}

func observeSuperHex(ctx context.Context, db *hexxladb.DB, idx *hexxladb.SuperHexSummaryIndex, sample int, results *workloadResults) error {
	probe := hexxladb.Coord{Q: 10_000 + sample/2, R: -5_000}
	packed, err := hexxladb.Pack(probe)
	if err != nil {
		return fmt.Errorf("pack super-hex probe: %w", err)
	}
	started := time.Now()
	err = db.Update(func(tx *hexxladb.Tx) error {
		if sample%2 == 0 {
			return tx.PutCell(ctx, hexxladb.CellRecord{Key: packed, RawContent: "synthetic-evidence-probe"})
		}
		return tx.DeleteCell(ctx, packed)
	})
	results.writeLatency = append(results.writeLatency, time.Since(started))
	if err != nil {
		return fmt.Errorf("write super-hex probe: %w", err)
	}

	started = time.Now()
	processed, err := idx.Sync(ctx, db, 256)
	results.syncLatency = append(results.syncLatency, time.Since(started))
	if err != nil {
		return fmt.Errorf("sync super-hex index: %w", err)
	}
	results.changesProcessed += int64(processed)
	return nil
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

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
