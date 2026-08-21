package hexxladb

import "github.com/hexxla/hexxladb/internal/engine"

// DistanceMetric identifies the similarity function used for embedding search.
// See [Options.DistanceMetric].
type DistanceMetric = engine.DistanceMetric

const (
	// DistanceCosine computes cosine similarity. Range [-1, 1]; higher = more similar.
	DistanceCosine DistanceMetric = engine.DistanceCosine
	// DistanceDotProduct computes the raw dot product. Assumes normalized vectors.
	DistanceDotProduct DistanceMetric = engine.DistanceDotProduct
	// DistanceL2 computes Euclidean distance. Lower = more similar; inverted for ranking.
	DistanceL2 DistanceMetric = engine.DistanceL2
)

// EmbeddingDimension returns the fixed vector dimension for this database.
// Returns 0 if no embeddings have been stored yet (dimension auto-detected on first [Tx.PutEmbedding]).
func (db *DB) EmbeddingDimension() uint16 {
	if db == nil {
		return 0
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return 0
	}
	return db.eng.EmbeddingDim()
}

// EmbeddingMetric returns the distance metric configured for this database.
func (db *DB) EmbeddingMetric() DistanceMetric {
	if db == nil {
		return DistanceCosine
	}
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.eng == nil {
		return DistanceCosine
	}
	return db.eng.EmbeddingMetric()
}
