package engine

// EmbeddingDim returns the fixed vector dimension (0 = not yet configured).
func (e *Engine) EmbeddingDim() uint16 { return e.embeddingDim }

// EmbeddingMetric returns the distance function for embedding search.
func (e *Engine) EmbeddingMetric() DistanceMetric { return e.embeddingMetric }

// SetEmbeddingConfig sets the embedding dimension and metric, persisting them to the
// file header. This is called exactly once — on the first PutEmbedding — when no
// dimension was configured at Open time. Subsequent calls with a different dimension
// return ErrInvalidEmbeddingConfig.
func (e *Engine) SetEmbeddingConfig(dim uint16, metric DistanceMetric) error {
	if dim == 0 {
		return ErrInvalidEmbeddingConfig
	}
	if e.embeddingDim != 0 && e.embeddingDim != dim {
		return ErrInvalidEmbeddingConfig
	}
	if e.embeddingDim == dim {
		return nil // already set to same value
	}
	if err := e.UpdateHeader(func(h *Header) {
		h.EmbeddingDim = dim
		h.EmbeddingMetric = metric
	}); err != nil {
		return err
	}
	e.embeddingDim = dim
	e.embeddingMetric = metric
	return nil
}
