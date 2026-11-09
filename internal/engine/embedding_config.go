package engine

// EmbeddingDim returns the fixed vector dimension (0 = not yet configured).
func (e *Engine) EmbeddingDim() uint16 {
	if e.wtxn != nil {
		return e.wtxn.hdr.EmbeddingDim
	}
	return e.embeddingDim
}

// EmbeddingMetric returns the distance function for embedding search.
func (e *Engine) EmbeddingMetric() DistanceMetric {
	if e.wtxn != nil {
		return e.wtxn.hdr.EmbeddingMetric
	}
	return e.embeddingMetric
}

// SetEmbeddingConfig sets the embedding dimension and metric, persisting them to the
// file header. This is called exactly once — on the first PutEmbedding — when no
// dimension was configured at Open time. Subsequent calls with a different dimension
// return ErrInvalidEmbeddingConfig.
func (e *Engine) SetEmbeddingConfig(dim uint16, metric DistanceMetric) error {
	if dim == 0 {
		return ErrInvalidEmbeddingConfig
	}
	current := e.EmbeddingDim()
	if current != 0 && current != dim {
		return ErrInvalidEmbeddingConfig
	}
	if current == dim {
		return nil // already set to same value
	}
	return e.UpdateHeader(func(h *Header) {
		h.EmbeddingDim = dim
		h.EmbeddingMetric = metric
	})
}
