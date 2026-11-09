package hexxladb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/fsutil"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
)

const (
	// DefaultEmbeddingIndexRebuildMaxVectors is the default in-memory rebuild bound.
	DefaultEmbeddingIndexRebuildMaxVectors = 10_000
	// DefaultEmbeddingIndexRebuildMaxMemoryBytes is the default conservative
	// in-process and transient-WAL budget for a rebuild.
	DefaultEmbeddingIndexRebuildMaxMemoryBytes = 2 << 30
	// MaxEmbeddingIndexRebuildVectors is the hard upper bound accepted by the rebuild API.
	MaxEmbeddingIndexRebuildVectors = 20_000
)

// EmbeddingIndexRebuildOptions bounds [DB.RebuildEmbeddingIndex].
type EmbeddingIndexRebuildOptions struct {
	// MaxVectors bounds the authoritative vectors loaded into memory. Zero uses
	// [DefaultEmbeddingIndexRebuildMaxVectors].
	MaxVectors int
	// MaxMemoryBytes bounds the conservative rebuild estimate. Zero uses
	// [DefaultEmbeddingIndexRebuildMaxMemoryBytes].
	MaxMemoryBytes uint64
}

// EmbeddingIndexRebuildStats reports aggregate completed rebuild work.
type EmbeddingIndexRebuildStats struct {
	Vectors            int
	Revision           uint64
	BuildDuration      time.Duration
	PublishDuration    time.Duration
	EstimatedPeakBytes uint64
}

type embeddingRebuildFaultHooks struct {
	beforePublish func()
}

// RebuildEmbeddingIndex builds HNSW from authoritative embeddings in bounded
// memory, then atomically replaces the published graph. While the build is in
// progress, embedding queries use exact flat search. If embeddings change
// after the snapshot, publication fails with [ErrEmbeddingIndexChanged] and
// the exact fallback remains active so the caller can retry.
// If layer zero cannot reach every vector, publication fails with
// [ErrEmbeddingIndexDisconnected] and the exact fallback remains active.
func (db *DB) RebuildEmbeddingIndex(ctx context.Context, opts *EmbeddingIndexRebuildOptions) (EmbeddingIndexRebuildStats, error) {
	var stats EmbeddingIndexRebuildStats
	limits, err := db.preflightEmbeddingIndexRebuild(ctx, opts)
	if err != nil {
		return stats, err
	}
	if err := db.markEmbeddingIndexDirty(); err != nil {
		return stats, err
	}

	candidates, revision, estimatedPeakBytes, err := db.snapshotEmbeddingsForRebuild(ctx, limits)
	if err != nil {
		return stats, err
	}
	stats.EstimatedPeakBytes = estimatedPeakBytes
	stats.Vectors = len(candidates)
	stats.Revision = revision

	buildStarted := time.Now()
	graph, err := hnsw.Build(ctx, candidates, engine.SimilarityFunc(db.EmbeddingMetric()))
	stats.BuildDuration = time.Since(buildStarted)
	if errors.Is(err, hnsw.ErrDisconnectedGraph) {
		return stats, fmt.Errorf("%w: %w", ErrEmbeddingIndexDisconnected, err)
	}
	if err != nil {
		return stats, err
	}
	if hook := db.embeddingRebuildFaults; hook != nil && hook.beforePublish != nil {
		hook.beforePublish()
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}

	publishStarted := time.Now()
	err = db.Update(func(tx *Tx) error {
		state, found, stateErr := tx.loadEmbeddingIndexState()
		if stateErr != nil {
			return stateErr
		}
		if !found || !state.dirty || state.revision != revision {
			return ErrEmbeddingIndexChanged
		}
		if err := tx.replaceEmbeddingGraph(ctx, graph); err != nil {
			return err
		}
		state.dirty = false
		return tx.putEmbeddingIndexState(state)
	})
	stats.PublishDuration = time.Since(publishStarted)
	if err != nil {
		return stats, err
	}
	return stats, nil
}

type embeddingRebuildLimits struct {
	vectors int
	memory  uint64
}

func (db *DB) preflightEmbeddingIndexRebuild(ctx context.Context, opts *EmbeddingIndexRebuildOptions) (embeddingRebuildLimits, error) {
	if ctx == nil {
		return embeddingRebuildLimits{}, fmt.Errorf("%w: nil context", ErrInvalidArgument)
	}
	if db == nil || db.closed.Load() {
		return embeddingRebuildLimits{}, ErrDatabaseClosed
	}
	maxVectors, maxMemoryBytes, err := resolveEmbeddingIndexRebuildOptions(opts)
	if err != nil {
		return embeddingRebuildLimits{}, err
	}
	if err := ctx.Err(); err != nil {
		return embeddingRebuildLimits{}, err
	}
	count, dimension, databasePath, err := db.countEmbeddings(ctx, maxVectors)
	if err != nil {
		return embeddingRebuildLimits{}, err
	}
	if count > maxVectors {
		return embeddingRebuildLimits{}, fmt.Errorf("%w: found more than %d vectors", ErrEmbeddingIndexTooLarge, maxVectors)
	}
	estimatedPeakBytes := estimateEmbeddingIndexRebuildBytes(count, dimension)
	if estimatedPeakBytes > maxMemoryBytes {
		return embeddingRebuildLimits{}, fmt.Errorf("%w: estimated peak %d bytes exceeds MaxMemoryBytes %d", ErrEmbeddingIndexTooLarge, estimatedPeakBytes, maxMemoryBytes)
	}
	if err := checkEmbeddingIndexRebuildSpace(databasePath, estimatedPeakBytes); err != nil {
		return embeddingRebuildLimits{}, err
	}
	return embeddingRebuildLimits{vectors: maxVectors, memory: maxMemoryBytes}, nil
}

func checkEmbeddingIndexRebuildSpace(databasePath string, estimate uint64) error {
	available, known, err := fsutil.AvailableBytes(filepath.Dir(databasePath))
	if err != nil {
		return fmt.Errorf("embedding index rebuild capacity: %w", err)
	}
	if known && available < estimate {
		return fmt.Errorf("%w: embedding index rebuild requires %d bytes, %d available", ErrInsufficientSpace, estimate, available)
	}
	return nil
}

func resolveEmbeddingIndexRebuildOptions(opts *EmbeddingIndexRebuildOptions) (int, uint64, error) {
	maxVectors := DefaultEmbeddingIndexRebuildMaxVectors
	maxMemoryBytes := uint64(DefaultEmbeddingIndexRebuildMaxMemoryBytes)
	if opts != nil && opts.MaxVectors != 0 {
		maxVectors = opts.MaxVectors
	}
	if opts != nil && opts.MaxMemoryBytes != 0 {
		maxMemoryBytes = opts.MaxMemoryBytes
	}
	if maxVectors < 1 || maxVectors > MaxEmbeddingIndexRebuildVectors {
		return 0, 0, fmt.Errorf("%w: MaxVectors must be between 1 and %d", ErrInvalidArgument, MaxEmbeddingIndexRebuildVectors)
	}
	if maxMemoryBytes < 64<<20 {
		return 0, 0, fmt.Errorf("%w: MaxMemoryBytes must be at least %d", ErrInvalidArgument, 64<<20)
	}
	return maxVectors, maxMemoryBytes, nil
}

func estimateEmbeddingIndexRebuildBytes(count int, dimension uint16) uint64 {
	perVector := uint64(56<<10) + uint64(dimension)*24
	vectorCount := uint64(count) //nolint:gosec // count is bounded to 20000.
	if perVector != 0 && vectorCount > math.MaxUint64/perVector {
		return math.MaxUint64
	}
	return vectorCount * perVector
}

func (db *DB) countEmbeddings(ctx context.Context, limit int) (int, uint16, string, error) {
	count := 0
	var dimension uint16
	var databasePath string
	var scanErr error
	err := db.View(func(tx *Tx) error {
		dimension = tx.db.eng.EmbeddingDim()
		databasePath = tx.db.eng.Path()
		return tx.AscendRange([]byte(index.EmbedPrefix), index.EmbedKeyEnd(), func(k, _ []byte) bool {
			if err := ctx.Err(); err != nil {
				scanErr = err
				return false
			}
			if !bytes.HasPrefix(k, []byte(index.EmbedPrefix)) {
				return false
			}
			count++
			return count <= limit
		})
	})
	if err == nil {
		err = scanErr
	}
	return count, dimension, databasePath, err
}

func (db *DB) markEmbeddingIndexDirty() error {
	return db.Update(func(tx *Tx) error {
		state, _, err := tx.loadEmbeddingIndexState()
		if err != nil {
			return err
		}
		if err := advanceEmbeddingIndexRevision(&state); err != nil {
			return err
		}
		state.dirty = true
		return tx.putEmbeddingIndexState(state)
	})
}

func (db *DB) snapshotEmbeddingsForRebuild(ctx context.Context, limits embeddingRebuildLimits) ([]hnsw.Embedding, uint64, uint64, error) {
	var candidates []hnsw.Embedding
	var revision, estimate uint64
	var databasePath string
	err := db.View(func(tx *Tx) error {
		state, found, err := tx.loadEmbeddingIndexState()
		if err != nil {
			return err
		}
		if !found || !state.dirty {
			return ErrEmbeddingIndexChanged
		}
		revision = state.revision
		var scanErr error
		dimension := tx.db.eng.EmbeddingDim()
		databasePath = tx.db.eng.Path()
		err = tx.AscendRange([]byte(index.EmbedPrefix), index.EmbedKeyEnd(), func(k, value []byte) bool {
			if err := ctx.Err(); err != nil {
				scanErr = err
				return false
			}
			if !bytes.HasPrefix(k, []byte(index.EmbedPrefix)) {
				return false
			}
			if len(candidates) == limits.vectors {
				scanErr = fmt.Errorf("%w: found more than %d vectors", ErrEmbeddingIndexTooLarge, limits.vectors)
				return false
			}
			estimate = estimateEmbeddingIndexRebuildBytes(len(candidates)+1, dimension)
			if estimate > limits.memory {
				scanErr = fmt.Errorf("%w: estimated peak %d bytes exceeds MaxMemoryBytes %d", ErrEmbeddingIndexTooLarge, estimate, limits.memory)
				return false
			}
			coord, parseErr := index.ParseEmbedKey(k)
			if parseErr != nil {
				scanErr = fmt.Errorf("%w: invalid embedding key: %w", ErrCorruptDatabase, parseErr)
				return false
			}
			vector, decodeErr := decodeEmbedding(value, dimension)
			if decodeErr != nil {
				scanErr = decodeErr
				return false
			}
			candidates = append(candidates, hnsw.Embedding{Coord: coord, Vector: vector})
			return true
		})
		if err != nil {
			return err
		}
		return scanErr
	})
	if err != nil {
		return nil, 0, 0, err
	}
	if err := checkEmbeddingIndexRebuildSpace(databasePath, estimate); err != nil {
		return nil, 0, 0, err
	}
	return candidates, revision, estimate, nil
}

func (tx *Tx) replaceEmbeddingGraph(ctx context.Context, graph hnsw.BuiltGraph) error {
	var nodeKeys [][]byte
	if err := tx.AscendRange([]byte(index.HNSWNodePrefix), index.HNSWNodeKeyEnd(), func(key, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			return false
		}
		if !bytes.HasPrefix(key, []byte(index.HNSWNodePrefix)) {
			return false
		}
		nodeKeys = append(nodeKeys, bytes.Clone(key))
		return true
	}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, key := range nodeKeys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := tx.deleteDirect(key); err != nil {
			return err
		}
	}
	if err := tx.deleteDirect([]byte(index.HNSWMetaKey)); err != nil {
		return err
	}
	if err := tx.deleteDirect([]byte(index.HNSWEntryKey)); err != nil {
		return err
	}
	if graph.Meta == nil {
		return nil
	}
	persisted := &txHNSWStorage{tx: tx}
	for _, node := range graph.Nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := persisted.PutHNSWNode(node); err != nil {
			return err
		}
	}
	if err := persisted.PutHNSWEntry(graph.Entry); err != nil {
		return err
	}
	return persisted.PutHNSWMeta(graph.Meta)
}
