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
// The vector length must equal [DB.EmbeddingDimension]; the database must have been
// opened with a non-zero [Options.EmbeddingDimension].
// Only allowed inside [DB.Update].
func (tx *Tx) PutEmbedding(coord lattice.PackedCoord, vec []float32) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return ErrEmbeddingsDisabled
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

// GetEmbedding returns the vector embedding for the cell at coord, or (nil, false, nil) if none stored.
func (tx *Tx) GetEmbedding(coord lattice.PackedCoord) (vec []float32, ok bool, err error) {
	if tx == nil || tx.db == nil {
		return nil, false, ErrClosed
	}
	if tx.db.activeEng() == nil {
		return nil, false, ErrDatabaseClosed
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, false, ErrEmbeddingsDisabled
	}
	key := index.EmbedKey(coord)
	val, found, getErr := tx.getDirect(key)
	if getErr != nil || !found {
		return nil, false, getErr
	}
	return decodeFloat32s(val), true, nil
}

// DeleteEmbedding removes the vector embedding for the cell at coord. Idempotent.
// Only allowed inside [DB.Update].
func (tx *Tx) DeleteEmbedding(coord lattice.PackedCoord) error {
	if err := tx.requireWritable(); err != nil {
		return err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return ErrEmbeddingsDisabled
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
