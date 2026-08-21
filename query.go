package hexxladb

import "time"

// SortOrder controls how [Tx.QueryCells] orders its results.
type SortOrder int

const (
	// SortByScore sorts results by composite relevance score descending (default).
	SortByScore SortOrder = iota
	// SortByConfidence sorts results by Provenance.Confidence descending.
	SortByConfidence
	// SortByRecency sorts results by ValidFrom descending (most recently written first).
	// Cells with no ValidFrom are sorted after cells that have one.
	SortByRecency
	// SortByCoord sorts results by Coord lexicographically ascending (Q then R).
	SortByCoord
)

// CellQuery is a composable predicate specification for [Tx.QueryCells].
//
// All non-zero fields are combined as AND conditions. Fields that are zero
// values (empty strings, nil slices, zero times, etc.) are ignored.
//
// Typical usage:
//
//	results, err := tx.QueryCells(ctx, hexxladb.CellQuery{
//	    RequireTags:   []string{"fact"},
//	    After:         time.Now().Add(-7 * 24 * time.Hour),
//	    MinConfidence: 0.8,
//	    MaxResults:    20,
//	    SortBy:        hexxladb.SortByRecency,
//	})
type CellQuery struct {
	// --- Lexical ---

	// Query is matched against RawContent (substring), Tags (exact or prefix),
	// and SourceID (exact). Empty string disables lexical matching.
	Query string

	// RequireTags requires ALL listed tags to be present on a cell (AND).
	RequireTags []string

	// AnyTags requires AT LEAST ONE of the listed tags to be present (OR).
	// Ignored when empty.
	AnyTags []string

	// ExcludeTags rejects cells that carry ANY of the listed tags (NOT).
	ExcludeTags []string

	// --- Provenance ---

	// SourceID restricts results to cells with exactly this source identifier.
	SourceID string

	// MinConfidence rejects cells with Confidence < MinConfidence.
	// Zero means no lower bound.
	MinConfidence float64

	// MaxConfidence rejects cells with Confidence > MaxConfidence.
	// Zero means no upper bound.
	MaxConfidence float64

	// --- Temporal ---

	// After rejects cells whose ValidFrom is at or before this time.
	// Uses the cell-level validity window (set when PutCell is called with
	// a non-nil ValidFrom), not the MVCC commit timestamp.
	// Zero time means no lower bound.
	After time.Time

	// Before rejects cells whose ValidFrom is at or after this time.
	// Zero time means no upper bound.
	Before time.Time

	// --- Spatial ---

	// Center and Radius together restrict results to cells within Radius rings
	// of Center (using hex distance). Radius=0 disables spatial filtering.
	Center Coord
	Radius int

	// --- Output ---

	// MaxResults caps the number of returned results. Non-positive means unlimited.
	MaxResults int

	// SortBy controls result ordering. Defaults to SortByScore.
	SortBy SortOrder

	// Explain populates CellQueryResult.Explanation with a human-readable
	// description of why each cell was included and its score breakdown.
	Explain bool

	// --- Scan safety ---

	// MaxScanRows caps the number of index rows examined during the primary scan.
	// Zero means unlimited (current behaviour). Set this to protect against
	// accidentally unbounded full-index walks when SourceID is set but MaxResults
	// is not. When the limit is hit, results collected so far are returned;
	// no error is raised and no truncation flag is set (conservative).
	MaxScanRows int

	// --- Embedding / vector ---

	// Embedding triggers ANN-accelerated seed selection via [Tx.SearchByEmbedding].
	// When non-nil, the planner uses the embedding index to narrow the candidate
	// set and boosts scores by embedding similarity. The vector length must match
	// [DB.EmbeddingDimension] (auto-detected on first [Tx.PutEmbedding]).
	// Returns empty results if no embeddings have been stored yet.
	// All other predicate fields (tags, temporal, spatial, etc.) are applied as
	// post-filters on the embedding results.
	Embedding []float32
}

// CellQueryResult is one result from [Tx.QueryCells].
type CellQueryResult struct {
	// Cell is the fully assembled view of the matching cell, including Coord.
	Cell CellView

	// Score is the composite relevance score used for SortByScore ordering.
	// Zero for queries that use SortByConfidence, SortByRecency, or SortByCoord.
	Score float64

	// Explanation is a human-readable breakdown of the score and filter decisions.
	// Only populated when CellQuery.Explain is true.
	Explanation string
}
