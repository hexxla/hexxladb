package hexxladb

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/index"
)

const (
	embeddingIndexStateVersion = 1
	embeddingIndexStateSize    = 10
	embeddingIndexDirtyFlag    = 1
)

type embeddingIndexState struct {
	revision uint64
	dirty    bool
}

func advanceEmbeddingIndexRevision(state *embeddingIndexState) error {
	if state.revision == math.MaxUint64 {
		return fmt.Errorf("%w: embedding index revision overflow", ErrCorruptDatabase)
	}
	state.revision++
	return nil
}

func (tx *Tx) loadEmbeddingIndexState() (embeddingIndexState, bool, error) {
	encoded, found, err := tx.getDirect([]byte(index.HNSWStateKey))
	if err != nil || !found {
		return embeddingIndexState{}, found, err
	}
	if len(encoded) != embeddingIndexStateSize || encoded[0] != embeddingIndexStateVersion || encoded[1]&^byte(embeddingIndexDirtyFlag) != 0 {
		return embeddingIndexState{}, false, fmt.Errorf("%w: invalid embedding index state", ErrCorruptDatabase)
	}
	return embeddingIndexState{
		revision: binary.BigEndian.Uint64(encoded[2:]),
		dirty:    encoded[1]&embeddingIndexDirtyFlag != 0,
	}, true, nil
}

func (tx *Tx) putEmbeddingIndexState(state embeddingIndexState) error {
	encoded := make([]byte, embeddingIndexStateSize)
	encoded[0] = embeddingIndexStateVersion
	if state.dirty {
		encoded[1] = embeddingIndexDirtyFlag
	}
	binary.BigEndian.PutUint64(encoded[2:], state.revision)
	return tx.putDirect([]byte(index.HNSWStateKey), encoded)
}

// recordEmbeddingMutation advances the persisted revision whenever a rebuild
// may be observing embeddings. It reports whether the active graph should be
// maintained synchronously for this mutation.
func (tx *Tx) recordEmbeddingMutation(deferIndex bool) (bool, error) {
	state, found, err := tx.loadEmbeddingIndexState()
	if err != nil {
		return false, err
	}
	if !found && !deferIndex {
		return true, nil
	}
	if err := advanceEmbeddingIndexRevision(&state); err != nil {
		return false, err
	}
	state.dirty = state.dirty || deferIndex
	if err := tx.putEmbeddingIndexState(state); err != nil {
		return false, err
	}
	return !state.dirty, nil
}
