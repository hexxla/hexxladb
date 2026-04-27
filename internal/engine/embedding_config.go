package engine

// EmbeddingDim returns the fixed vector dimension (0 = embeddings disabled).
func (e *Engine) EmbeddingDim() uint16 { return e.embeddingDim }

// EmbeddingMetric returns the distance function for embedding search.
func (e *Engine) EmbeddingMetric() DistanceMetric { return e.embeddingMetric }
