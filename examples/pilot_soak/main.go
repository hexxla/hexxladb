// Pilot Soak runs a bounded, deterministic mixed workload against the
// conservative production-readiness profile and emits one aggregate JSON
// report. The caller must provide a new, empty work directory.
//
// Usage:
//
//	go run ./examples/pilot_soak -work-dir /controlled/empty/directory
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"syscall"
	"time"

	"github.com/hexxla/hexxladb"
)

const (
	defaultCells          = 10_000
	defaultDimension      = 32
	defaultOpsPerSecond   = 20
	defaultWritePercent   = 5
	defaultDuration       = 5 * time.Minute
	defaultBackupInterval = 15 * time.Minute
	defaultProgress       = time.Minute
	maxDuration           = time.Hour
	batchSize             = 500
	pageSize              = 4096
	pageCacheBytes        = 64 << 20
	maxHeapBytes          = 1 << 30
	maxStorageBytes       = 2 << 30
	consumerID            = "pilot-soak"
)

var latencyLimits = map[string]time.Duration{
	"point_read":    5 * time.Millisecond,
	"fov_read":      10 * time.Millisecond,
	"vector_search": 25 * time.Millisecond,
	"tag_scan":      50 * time.Millisecond,
	"cell_write":    25 * time.Millisecond,
	"vector_update": 2 * time.Second,
}

var minimumSamples = map[string]int64{
	"point_read":    1_000,
	"fov_read":      250,
	"vector_search": 300,
	"tag_scan":      200,
	"cell_write":    50,
	"vector_update": 10,
}

var operationNames = []string{"point_read", "fov_read", "vector_search", "tag_scan", "cell_write", "vector_update"}

type config struct {
	workDir        string
	duration       time.Duration
	backupInterval time.Duration
	progress       time.Duration
	cells          int
	dimension      int
	opsPerSecond   int
	writePercent   int
	seed           uint64
	vcsRevision    string
	vcsModified    string
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

type storageReport struct {
	PrimaryBytes   uint64 `json:"primary_bytes"`
	WALBytes       uint64 `json:"wal_bytes"`
	ChangelogBytes uint64 `json:"changelog_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
}

type checkReport struct {
	Name       string  `json:"name"`
	Observed   float64 `json:"observed"`
	Limit      float64 `json:"limit"`
	Comparison string  `json:"comparison"`
	Pass       bool    `json:"pass"`
}

type report struct {
	SchemaVersion int `json:"schema_version"`
	Runtime       struct {
		GoVersion   string `json:"go_version"`
		GOOS        string `json:"goos"`
		GOARCH      string `json:"goarch"`
		VCSRevision string `json:"vcs_revision,omitempty"`
		VCSModified *bool  `json:"vcs_modified,omitempty"`
	} `json:"runtime"`
	Profile struct {
		DurationNS              int64  `json:"duration_ns"`
		Cells                   int    `json:"cells"`
		Vectors                 int    `json:"vectors"`
		Dimension               int    `json:"dimension"`
		OpsPerSecond            int    `json:"operations_per_second"`
		WritePercent            int    `json:"write_percent"`
		VectorUpdateEveryWrites int    `json:"vector_update_every_writes"`
		PageSize                int    `json:"page_size"`
		PageCacheBytes          int    `json:"page_cache_bytes"`
		BackupIntervalNS        int64  `json:"backup_interval_ns"`
		RPOSeconds              int64  `json:"rpo_seconds"`
		RTOSeconds              int64  `json:"rto_seconds"`
		Seed                    uint64 `json:"seed"`
		MVCC                    bool   `json:"mvcc"`
		PrimaryEncrypted        bool   `json:"primary_encrypted"`
		ChangelogEnabled        bool   `json:"changelog_enabled"`
		DurableConsumer         bool   `json:"durable_consumer"`
	} `json:"profile"`
	SeedDurationNS int64                     `json:"seed_duration_ns"`
	RunDurationNS  int64                     `json:"run_duration_ns"`
	Operations     map[string]uint64         `json:"operations"`
	Latency        map[string]latencySummary `json:"latency"`
	AchievedOps    float64                   `json:"achieved_operations_per_second"`
	Backups        struct {
		Completed      uint64         `json:"completed"`
		BackupLatency  latencySummary `json:"backup_latency"`
		RestoreLatency latencySummary `json:"restore_latency"`
	} `json:"backups"`
	Resources struct {
		InitialStorage storageReport `json:"initial_storage"`
		FinalStorage   storageReport `json:"final_storage"`
		MaxStorage     storageReport `json:"max_storage"`
		MaxHeapInuse   uint64        `json:"max_heap_inuse_bytes"`
	} `json:"resources"`
	Health struct {
		CellCount         int      `json:"cell_count"`
		OrphanedSeams     int      `json:"orphaned_seams"`
		TagIndexErrors    int      `json:"tag_index_errors"`
		SourceIndexErrors int      `json:"source_index_errors"`
		Warnings          []string `json:"warnings,omitempty"`
		ConsumerSeq       uint64   `json:"consumer_seq"`
	} `json:"health"`
	Checks     []checkReport `json:"checks"`
	Violations []string      `json:"violations,omitempty"`
	Pass       bool          `json:"pass"`
}

type workload struct {
	cfg        config
	db         *hexxladb.DB
	opts       *hexxladb.Options
	dbPath     string
	coords     []hexxladb.PackedCoord
	centers    []hexxladb.Coord
	vectors    [][]float32
	rng        *generator
	cursor     uint64
	writes     uint64
	operations map[string]uint64
	latencies  map[string][]time.Duration
	backups    []time.Duration
	restores   []time.Duration
	maxStorage storageReport
	maxHeap    uint64
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.workDir, "work-dir", "", "new, empty directory owned by this run (required)")
	flag.DurationVar(&cfg.duration, "duration", defaultDuration, "measured soak duration (1s..1h)")
	flag.DurationVar(&cfg.backupInterval, "backup-interval", defaultBackupInterval, "online backup/restore interval")
	flag.DurationVar(&cfg.progress, "progress-interval", defaultProgress, "stderr progress interval")
	flag.IntVar(&cfg.cells, "cells", defaultCells, "cells and vectors to seed (100..10000)")
	flag.IntVar(&cfg.dimension, "dimension", defaultDimension, "vector dimension (32 or 384)")
	flag.IntVar(&cfg.opsPerSecond, "ops-per-second", defaultOpsPerSecond, "target operations per second (1..100)")
	flag.IntVar(&cfg.writePercent, "write-percent", defaultWritePercent, "serialized write percentage (1..20)")
	flag.Uint64Var(&cfg.seed, "seed", 1, "deterministic workload seed")
	flag.StringVar(&cfg.vcsRevision, "vcs-revision", "", "source revision recorded in the report")
	flag.StringVar(&cfg.vcsModified, "vcs-modified", "", "source modification state: true, false, or unknown")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "pilot soak: %v\n", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	result, err := run(ctx, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "pilot soak: %v\n", err)
		stop()
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "pilot soak: encode report: %v\n", err)
		stop()
		os.Exit(1)
	}
	stop()
	if !result.Pass {
		os.Exit(1)
	}
}

func validateConfig(cfg config) error {
	if cfg.workDir == "" {
		return errors.New("work-dir is required")
	}
	if cfg.duration < time.Second || cfg.duration > maxDuration {
		return errors.New("duration must be between 1s and 1h")
	}
	if cfg.backupInterval < time.Second || cfg.backupInterval > maxDuration {
		return errors.New("backup-interval must be between 1s and 1h")
	}
	if cfg.progress < time.Second || cfg.progress > time.Hour {
		return errors.New("progress-interval must be between 1s and 1h")
	}
	if cfg.cells < 100 || cfg.cells > defaultCells {
		return fmt.Errorf("cells must be between 100 and %d", defaultCells)
	}
	if cfg.dimension != 32 && cfg.dimension != 384 {
		return errors.New("dimension must be 32 or 384")
	}
	if cfg.opsPerSecond < 1 || cfg.opsPerSecond > 100 {
		return errors.New("ops-per-second must be between 1 and 100")
	}
	if cfg.writePercent < 1 || cfg.writePercent > 20 {
		return errors.New("write-percent must be between 1 and 20")
	}
	if cfg.vcsModified != "" && cfg.vcsModified != "true" && cfg.vcsModified != "false" {
		return errors.New("vcs-modified must be true, false, or empty")
	}
	entries, err := os.ReadDir(cfg.workDir)
	if err != nil {
		return fmt.Errorf("inspect work-dir: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("work-dir must be empty")
	}
	return nil
}

func run(ctx context.Context, cfg config) (report, error) {
	result := newReport(cfg)
	w, err := newWorkload(cfg)
	if err != nil {
		return result, err
	}
	defer func() {
		if w.db != nil {
			_ = w.db.Close()
		}
	}()

	seedStarted := time.Now()
	if err := w.seed(ctx); err != nil {
		return result, fmt.Errorf("seed: %w", err)
	}
	result.SeedDurationNS = time.Since(seedStarted).Nanoseconds()
	initial, err := w.observeResources()
	if err != nil {
		return result, err
	}
	result.Resources.InitialStorage = initial
	runErr := w.runOperations(ctx, &result)
	if runErr != nil {
		result.Violations = append(result.Violations, runErr.Error())
	}

	if ctx.Err() == nil {
		if runErr == nil {
			if err := w.backupAndRestore(ctx); err != nil {
				result.Violations = append(result.Violations, err.Error())
			}
		}
		if err := w.reopenAndValidate(ctx, &result); err != nil {
			result.Violations = append(result.Violations, err.Error())
		}
	}
	if w.db != nil {
		final, err := w.observeResources()
		if err != nil {
			result.Violations = append(result.Violations, err.Error())
		} else {
			result.Resources.FinalStorage = final
		}
	}

	w.populateReport(&result)
	evaluate(&result)
	return result, nil
}

func newReport(cfg config) report {
	result := report{SchemaVersion: 1}
	result.Runtime.GoVersion = runtime.Version()
	result.Runtime.GOOS = runtime.GOOS
	result.Runtime.GOARCH = runtime.GOARCH
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				result.Runtime.VCSRevision = setting.Value
			case "vcs.modified":
				modified := setting.Value == "true"
				result.Runtime.VCSModified = &modified
			}
		}
	}
	if cfg.vcsRevision != "" {
		result.Runtime.VCSRevision = cfg.vcsRevision
	}
	if cfg.vcsModified != "" {
		modified := cfg.vcsModified == "true"
		result.Runtime.VCSModified = &modified
	}
	result.Profile.DurationNS = cfg.duration.Nanoseconds()
	result.Profile.Cells = cfg.cells
	result.Profile.Vectors = cfg.cells
	result.Profile.Dimension = cfg.dimension
	result.Profile.OpsPerSecond = cfg.opsPerSecond
	result.Profile.WritePercent = cfg.writePercent
	result.Profile.VectorUpdateEveryWrites = 10
	result.Profile.PageSize = pageSize
	result.Profile.PageCacheBytes = pageCacheBytes
	result.Profile.BackupIntervalNS = cfg.backupInterval.Nanoseconds()
	result.Profile.RPOSeconds = int64(cfg.backupInterval.Seconds())
	result.Profile.RTOSeconds = int64((30 * time.Minute).Seconds())
	result.Profile.Seed = cfg.seed
	result.Profile.MVCC = true
	result.Profile.PrimaryEncrypted = true
	result.Profile.ChangelogEnabled = true
	result.Profile.DurableConsumer = true
	return result
}

func newWorkload(cfg config) (*workload, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	opts := &hexxladb.Options{
		EnableMVCC:         true,
		ChangelogEnabled:   true,
		EncryptionKey:      key,
		PageSize:           pageSize,
		PageCacheSize:      pageCacheBytes,
		EmbeddingDimension: uint16(cfg.dimension), //nolint:gosec // dimensions are restricted to 32 or 384.
		DistanceMetric:     hexxladb.DistanceCosine,
	}
	dbPath := filepath.Join(cfg.workDir, "pilot.db")
	db, err := hexxladb.Open(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("open pilot database: %w", err)
	}
	w := &workload{
		cfg:        cfg,
		db:         db,
		opts:       opts,
		dbPath:     dbPath,
		coords:     make([]hexxladb.PackedCoord, cfg.cells),
		centers:    make([]hexxladb.Coord, cfg.cells),
		vectors:    make([][]float32, cfg.cells),
		rng:        newGenerator(cfg.seed),
		operations: make(map[string]uint64, len(latencyLimits)),
		latencies:  make(map[string][]time.Duration, len(latencyLimits)),
	}
	for i := range cfg.cells {
		coord := hexxladb.Coord{Q: i%100 - 50, R: i/100 - 50}
		packed, err := hexxladb.Pack(coord)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pack cell %d: %w", i, err)
		}
		w.coords[i] = packed
		w.centers[i] = coord
		w.vectors[i] = randomUnitVector(w.rng, cfg.dimension)
	}
	return w, nil
}

func (w *workload) seed(ctx context.Context) error {
	for start := 0; start < w.cfg.cells; start += batchSize {
		end := min(start+batchSize, w.cfg.cells)
		if err := w.db.Update(func(tx *hexxladb.Tx) error {
			for i := start; i < end; i++ {
				rec := hexxladb.NewFactCell(
					w.coords[i],
					fmt.Sprintf("pilot record %d", i),
					fmt.Sprintf("pilot-source-%02d", i%32),
					fmt.Sprintf("pilot-tag-%02d", i%16),
					0.95,
				)
				if err := tx.PutCell(ctx, rec); err != nil {
					return err
				}
				if err := tx.PutEmbedding(w.coords[i], w.vectors[i]); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("batch %d..%d: %w", start, end, err)
		}
		_, _ = fmt.Fprintf(os.Stderr, "pilot soak: seeded %d/%d cells and vectors\n", end, w.cfg.cells)
	}
	if err := w.db.AdvanceChangelogConsumer(consumerID, 0, 0); err != nil {
		return fmt.Errorf("register durable consumer: %w", err)
	}
	return w.drainChangelog()
}

func (w *workload) runOperations(ctx context.Context, result *report) error {
	started := time.Now()
	operationTicker := time.NewTicker(time.Second / time.Duration(w.cfg.opsPerSecond))
	progressTicker := time.NewTicker(w.cfg.progress)
	backupTicker := time.NewTicker(w.cfg.backupInterval)
	resourceTicker := time.NewTicker(min(5*time.Minute, w.cfg.duration))
	deadline := time.NewTimer(w.cfg.duration)
	defer operationTicker.Stop()
	defer progressTicker.Stop()
	defer backupTicker.Stop()
	defer resourceTicker.Stop()
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			result.RunDurationNS = time.Since(started).Nanoseconds()
			return fmt.Errorf("soak interrupted: %w", ctx.Err())
		case <-deadline.C:
			result.RunDurationNS = time.Since(started).Nanoseconds()
			return nil
		case <-progressTicker.C:
			w.logProgress(started)
		case <-resourceTicker.C:
			if _, err := w.observeResources(); err != nil {
				result.RunDurationNS = time.Since(started).Nanoseconds()
				return err
			}
		case <-backupTicker.C:
			if err := w.backupAndRestore(ctx); err != nil {
				result.RunDurationNS = time.Since(started).Nanoseconds()
				return err
			}
		case <-operationTicker.C:
			if err := w.performOperation(ctx); err != nil {
				result.RunDurationNS = time.Since(started).Nanoseconds()
				return err
			}
		}
	}
}

func (w *workload) performOperation(ctx context.Context) error {
	selection := w.rng.intn(100)
	readBoundary := 100 - w.cfg.writePercent
	var name string
	var operation func() error
	switch {
	case selection >= readBoundary:
		updateVector := (w.writes+1)%10 == 0
		name = "cell_write"
		if updateVector {
			name = "vector_update"
		}
		operation = func() error { return w.write(ctx, updateVector) }
	case selection < 55:
		name, operation = "point_read", w.pointRead
	case selection < 70:
		name, operation = "fov_read", func() error { return w.fovRead(ctx) }
	case selection < 85:
		name, operation = "vector_search", w.vectorSearch
	default:
		name, operation = "tag_scan", func() error { return w.tagScan(ctx) }
	}
	started := time.Now()
	err := operation()
	w.latencies[name] = append(w.latencies[name], time.Since(started))
	w.operations[name]++
	if err != nil {
		return fmt.Errorf("%s operation %d: %w", name, w.operations[name], err)
	}
	return nil
}

func (w *workload) pointRead() error {
	index := w.rng.intn(w.cfg.cells)
	return w.db.View(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(w.coords[index])
		if err != nil {
			return err
		}
		if !ok || rec.Key != w.coords[index] {
			return errors.New("expected seeded cell was not found")
		}
		return nil
	})
}

func (w *workload) fovRead(ctx context.Context) error {
	index := w.rng.intn(w.cfg.cells)
	return w.db.View(func(tx *hexxladb.Tx) error {
		rows, err := tx.LoadContextFOV(ctx, w.centers[index], 5, func(hexxladb.Coord) bool { return false }, hexxladb.FOVContextConfig{MaxCells: 64})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errors.New("FOV returned no cells")
		}
		return nil
	})
}

func (w *workload) vectorSearch() error {
	index := w.rng.intn(w.cfg.cells)
	return w.db.View(func(tx *hexxladb.Tx) error {
		rows, stats, err := tx.SearchByEmbeddingWithStats(w.vectors[index], hexxladb.EmbeddingSearchConfig{MaxResults: 10})
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return errors.New("vector search returned no results")
		}
		if stats.Path != hexxladb.EmbeddingSearchPathHNSW {
			return fmt.Errorf("vector search used %q instead of HNSW", stats.Path)
		}
		return nil
	})
}

func (w *workload) tagScan(ctx context.Context) error {
	tag := fmt.Sprintf("pilot-tag-%02d", w.rng.intn(16))
	return w.db.View(func(tx *hexxladb.Tx) error {
		rows := 0
		err := tx.AscendCellsByTag(ctx, tag, func(hexxladb.CellRecord) bool {
			rows++
			return rows < 32
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return errors.New("tag scan returned no cells")
		}
		return nil
	})
}

func (w *workload) write(ctx context.Context, updateVector bool) error {
	index := w.rng.intn(w.cfg.cells)
	w.writes++
	if err := w.db.Update(func(tx *hexxladb.Tx) error {
		rec, ok, err := tx.GetCell(w.coords[index])
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("write target was not found")
		}
		rec.RawContent = fmt.Sprintf("pilot record %d revision %d", index, w.writes)
		rec.Provenance.UpdatedAt = time.Now().UTC().UnixNano()
		if err := tx.PutCell(ctx, rec); err != nil {
			return err
		}
		if updateVector {
			vector := randomUnitVector(w.rng, w.cfg.dimension)
			if err := tx.PutEmbedding(w.coords[index], vector); err != nil {
				return err
			}
			w.vectors[index] = vector
		}
		return nil
	}); err != nil {
		return err
	}
	return w.drainChangelog()
}

func (w *workload) drainChangelog() error {
	for {
		records, err := w.db.ReadChangelogSince(w.cursor, 256)
		if err != nil {
			return fmt.Errorf("read changelog: %w", err)
		}
		if len(records) == 0 {
			return nil
		}
		next := records[len(records)-1].Seq
		if err := w.db.AdvanceChangelogConsumer(consumerID, w.cursor, next); err != nil {
			return fmt.Errorf("advance durable consumer: %w", err)
		}
		w.cursor = next
	}
}

func (w *workload) backupAndRestore(ctx context.Context) error {
	sequence := len(w.backups) + 1
	backupDir := filepath.Join(w.cfg.workDir, fmt.Sprintf("backup-%06d", sequence))
	if err := os.Mkdir(backupDir, 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(backupDir) }()
	backupPath := filepath.Join(backupDir, "pilot.db")
	backupStarted := time.Now()
	if err := w.db.BackupTo(ctx, backupPath); err != nil {
		return fmt.Errorf("backup %d: %w", sequence, err)
	}
	w.backups = append(w.backups, time.Since(backupStarted))

	restoreStarted := time.Now()
	restored, err := hexxladb.Open(backupPath, w.opts)
	if err != nil {
		return fmt.Errorf("open backup %d: %w", sequence, err)
	}
	closeRestored := true
	defer func() {
		if closeRestored {
			_ = restored.Close()
		}
	}()
	if err := validateDatabase(ctx, restored, w.cfg.cells, w.cursor, w.vectors[0]); err != nil {
		return fmt.Errorf("validate backup %d: %w", sequence, err)
	}
	if err := w.observeHeap(); err != nil {
		return fmt.Errorf("backup %d resource check: %w", sequence, err)
	}
	if err := restored.Close(); err != nil {
		return fmt.Errorf("close backup %d: %w", sequence, err)
	}
	closeRestored = false
	w.restores = append(w.restores, time.Since(restoreStarted))
	return nil
}

func (w *workload) reopenAndValidate(ctx context.Context, result *report) error {
	if err := w.db.Close(); err != nil {
		return fmt.Errorf("close primary before reopen: %w", err)
	}
	w.db = nil
	db, err := hexxladb.Open(w.dbPath, w.opts)
	if err != nil {
		return fmt.Errorf("reopen primary: %w", err)
	}
	w.db = db
	if err := validateDatabase(ctx, db, w.cfg.cells, w.cursor, w.vectors[0]); err != nil {
		return fmt.Errorf("validate reopened primary: %w", err)
	}
	health, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		return fmt.Errorf("final health check: %w", err)
	}
	result.Health.CellCount = health.CellCount
	result.Health.OrphanedSeams = len(health.OrphanedSeams)
	result.Health.TagIndexErrors = health.TagIndexErrors
	result.Health.SourceIndexErrors = health.SourceIndexErrors
	result.Health.Warnings = health.Warnings
	result.Health.ConsumerSeq = w.cursor
	return nil
}

func validateDatabase(ctx context.Context, db *hexxladb.DB, cells int, expectedCursor uint64, vector []float32) error {
	health, err := db.HealthCheck(ctx, hexxladb.DefaultHealthCheckConfig())
	if err != nil {
		return err
	}
	if health.CellCount != cells || len(health.OrphanedSeams) != 0 || health.TagIndexErrors != 0 || health.SourceIndexErrors != 0 || len(health.Warnings) != 0 {
		return fmt.Errorf("health mismatch: cells=%d orphans=%d tag_errors=%d source_errors=%d warnings=%d", health.CellCount, len(health.OrphanedSeams), health.TagIndexErrors, health.SourceIndexErrors, len(health.Warnings))
	}
	cursor, ok, err := db.GetChangelogConsumerCursor(consumerID)
	if err != nil {
		return err
	}
	if !ok || cursor != expectedCursor {
		return fmt.Errorf("durable consumer cursor: found=%t got=%d want=%d", ok, cursor, expectedCursor)
	}
	return db.View(func(tx *hexxladb.Tx) error {
		rows, stats, err := tx.SearchByEmbeddingWithStats(vector, hexxladb.EmbeddingSearchConfig{MaxResults: 10})
		if err != nil {
			return err
		}
		if len(rows) == 0 || stats.Path != hexxladb.EmbeddingSearchPathHNSW {
			return fmt.Errorf("vector probe: results=%d path=%q", len(rows), stats.Path)
		}
		return nil
	})
}

func (w *workload) observeResources() (storageReport, error) {
	stats, err := w.db.StorageStats()
	if err != nil {
		return storageReport{}, fmt.Errorf("storage stats: %w", err)
	}
	current := storageReport{
		PrimaryBytes:   stats.PrimaryBytes,
		WALBytes:       stats.WALBytes,
		ChangelogBytes: stats.ChangelogBytes,
		TotalBytes:     stats.PrimaryBytes + stats.WALBytes + stats.ChangelogBytes,
	}
	if current.TotalBytes > w.maxStorage.TotalBytes {
		w.maxStorage = current
	}
	if current.TotalBytes > maxStorageBytes {
		return current, fmt.Errorf("storage limit exceeded: %d > %d bytes", current.TotalBytes, maxStorageBytes)
	}
	if err := w.observeHeap(); err != nil {
		return current, err
	}
	return current, nil
}

func (w *workload) observeHeap() error {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	w.maxHeap = max(w.maxHeap, memory.HeapInuse)
	if memory.HeapInuse > maxHeapBytes {
		return fmt.Errorf("heap limit exceeded: %d > %d bytes", memory.HeapInuse, maxHeapBytes)
	}
	return nil
}

func (w *workload) logProgress(started time.Time) {
	total := totalOperations(w.operations)
	rate := float64(total) / time.Since(started).Seconds()
	writes := w.operations["cell_write"] + w.operations["vector_update"]
	_, _ = fmt.Fprintf(os.Stderr, "pilot soak: elapsed=%s operations=%d rate=%.2f/s writes=%d vector_updates=%d backups=%d storage=%d heap=%d\n", time.Since(started).Round(time.Second), total, rate, writes, w.operations["vector_update"], len(w.backups), w.maxStorage.TotalBytes, w.maxHeap)
}

func (w *workload) populateReport(result *report) {
	result.Operations = w.operations
	result.Latency = make(map[string]latencySummary, len(w.latencies))
	for name, samples := range w.latencies {
		result.Latency[name] = summarize(samples)
	}
	total := totalOperations(w.operations)
	if result.RunDurationNS > 0 {
		result.AchievedOps = float64(total) / (float64(result.RunDurationNS) / float64(time.Second))
	}
	result.Backups.Completed = uint64(len(w.backups))
	result.Backups.BackupLatency = summarize(w.backups)
	result.Backups.RestoreLatency = summarize(w.restores)
	result.Resources.MaxStorage = w.maxStorage
	result.Resources.MaxHeapInuse = w.maxHeap
}

func evaluate(result *report) {
	minimumRate := float64(result.Profile.OpsPerSecond) * 0.95
	addMinimumCheck(result, "achieved_operations_per_second", result.AchievedOps, minimumRate)
	addMinimumCheck(result, "total_operations", float64(totalOperations(result.Operations)), 5_000)
	for _, name := range operationNames {
		limit := latencyLimits[name]
		summary := result.Latency[name]
		addMinimumCheck(result, name+"_samples", float64(summary.Samples), float64(minimumSamples[name]))
		addMaximumCheck(result, name+"_p95_ns", float64(summary.P95NS), float64(limit.Nanoseconds()))
	}
	addMaximumCheck(result, "max_heap_inuse_bytes", float64(result.Resources.MaxHeapInuse), maxHeapBytes)
	addMaximumCheck(result, "max_storage_bytes", float64(result.Resources.MaxStorage.TotalBytes), maxStorageBytes)
	addMaximumCheck(result, "max_restore_ns", float64(result.Backups.RestoreLatency.MaxNS), float64((30 * time.Minute).Nanoseconds()))
	addMinimumCheck(result, "completed_backups", float64(result.Backups.Completed), 1)
	addMaximumCheck(result, "health_orphaned_seams", float64(result.Health.OrphanedSeams), 0)
	addMaximumCheck(result, "health_tag_index_errors", float64(result.Health.TagIndexErrors), 0)
	addMaximumCheck(result, "health_source_index_errors", float64(result.Health.SourceIndexErrors), 0)
	addMaximumCheck(result, "health_warnings", float64(len(result.Health.Warnings)), 0)
	addExactCheck(result, "health_cell_count", float64(result.Health.CellCount), float64(result.Profile.Cells))
	result.Pass = len(result.Violations) == 0
	for _, check := range result.Checks {
		result.Pass = result.Pass && check.Pass
	}
}

func addMaximumCheck(result *report, name string, observed, limit float64) {
	result.Checks = append(result.Checks, checkReport{Name: name, Observed: observed, Limit: limit, Comparison: "<=", Pass: observed <= limit})
}

func addMinimumCheck(result *report, name string, observed, limit float64) {
	result.Checks = append(result.Checks, checkReport{Name: name, Observed: observed, Limit: limit, Comparison: ">=", Pass: observed >= limit})
}

func addExactCheck(result *report, name string, observed, limit float64) {
	result.Checks = append(result.Checks, checkReport{Name: name, Observed: observed, Limit: limit, Comparison: "==", Pass: observed == limit})
}

func totalOperations(counts map[string]uint64) uint64 {
	var total uint64
	for _, count := range counts {
		total += count
	}
	return total
}

type generator struct{ state uint64 }

func newGenerator(seed uint64) *generator { return &generator{state: seed} }

func (g *generator) uint64() uint64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return g.state
}

func (g *generator) float64() float64 {
	return float64(g.uint64()>>11) / float64(uint64(1)<<53)
}

func (g *generator) intn(limit int) int {
	return int(g.uint64() % uint64(limit)) //nolint:gosec // limit is a small validated workload bound.
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
		P99NS:   percentile(values, 99),
		MaxNS:   values[len(values)-1],
		MeanNS:  total / int64(len(values)),
	}
}

func percentile(sorted []int64, percent int) int64 {
	index := (len(sorted)*percent+99)/100 - 1
	return sorted[max(index, 0)]
}
