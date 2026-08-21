package hexxladb

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/engine"
	"github.com/hexxla/hexxladb/internal/hnsw"
	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
)

// PutEmbedding stores a vector embedding for the cell at coord.
// The vector length must equal [DB.EmbeddingDimension]. If no dimension was configured
// at [Open] time, it is auto-detected from the first vector and persisted in the file header.
// All subsequent vectors must match that dimension.
// Only allowed inside [DB.Update].
func (tx *Tx) PutEmbedding(coord lattice.PackedCoord, vec []float32) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	if err := validateEmbeddingVector(vec); err != nil {
		return err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		// Auto-detect: first PutEmbedding sets the dimension for this database.
		newDim := uint16(len(vec)) //nolint:gosec // bounded by uint16 max
		metric := tx.db.eng.EmbeddingMetric()
		if metric == 0 {
			metric = engine.DistanceCosine // default
		}
		if err := tx.db.eng.SetEmbeddingConfig(newDim, metric); err != nil {
			return fmt.Errorf("auto-detect embedding dimension: %w", err)
		}
		dim = newDim
	}
	if uint16(len(vec)) != dim { //nolint:gosec // len(vec) bounded by uint16 max (65535 dimensions)
		return fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(vec))
	}
	key := index.EmbedKey(coord)
	val := encodeFloat32s(vec)
	if err := tx.putDirect(key, val); err != nil {
		return err
	}
	// Update HNSW graph.
	g := hnsw.NewGraph(&txHNSWStorage{tx: tx}, engine.DistanceMetric(tx.db.eng.EmbeddingMetric()))
	return g.Insert(coord, vec)
}

func validateEmbeddingVector(vec []float32) error {
	if len(vec) == 0 || len(vec) > math.MaxUint16 {
		return fmt.Errorf("%w: dimension must be between 1 and %d", ErrEmbeddingDimension, uint16(math.MaxUint16))
	}
	for _, value := range vec {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%w: embedding components must be finite", ErrInvalidArgument)
		}
	}
	return nil
}

// GetEmbedding returns the vector embedding for the cell at coord, or (nil, false, nil) if none stored.
// Returns (nil, false, nil) if no embeddings have been stored yet (dimension not configured).
func (tx *Tx) GetEmbedding(coord lattice.PackedCoord) (vec []float32, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, false, ErrDatabaseClosed
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, false, nil // no embeddings stored yet
	}
	key := index.EmbedKey(coord)
	val, found, getErr := tx.getDirect(key)
	if getErr != nil || !found {
		return nil, false, getErr
	}
	return decodeFloat32s(val), true, nil
}

// DeleteEmbedding removes the vector embedding for the cell at coord. Idempotent.
// No-op if no embeddings have been stored yet (dimension not configured).
// Only allowed inside [DB.Update].
func (tx *Tx) DeleteEmbedding(coord lattice.PackedCoord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil // no embeddings stored yet — nothing to delete
	}
	key := index.EmbedKey(coord)
	if err := tx.deleteDirect(key); err != nil {
		return err
	}
	g := hnsw.NewGraph(&txHNSWStorage{tx: tx}, engine.DistanceMetric(tx.db.eng.EmbeddingMetric()))
	return g.Delete(coord)
}

// deleteEmbeddingIfEnabled removes the embedding for coord if embeddings are enabled. No-op otherwise.
// Used by [Tx.DeleteCell] cascade.
func (tx *Tx) deleteEmbeddingIfEnabled(coord lattice.PackedCoord) error {
	if tx.db.eng.EmbeddingDim() == 0 {
		return nil // embeddings not configured — nothing to cascade
	}
	key := index.EmbedKey(coord)
	if err := tx.deleteDirect(key); err != nil {
		return err
	}
	g := hnsw.NewGraph(&txHNSWStorage{tx: tx}, engine.DistanceMetric(tx.db.eng.EmbeddingMetric()))
	return g.Delete(coord)
}

// encodeFloat32s encodes a float32 slice as raw little-endian bytes.
func encodeFloat32s(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// decodeFloat32s decodes raw little-endian bytes into a float32 slice.
func decodeFloat32s(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := range n {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
