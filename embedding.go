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

// EmbeddingDimension returns the fixed vector dimension for this database (0 = embeddings disabled).
func (db *DB) EmbeddingDimension() uint16 {
	return db.eng.EmbeddingDim()
}

// EmbeddingMetric returns the distance metric configured for this database.
func (db *DB) EmbeddingMetric() DistanceMetric {
	return db.eng.EmbeddingMetric()
}
