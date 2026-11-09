package hexxladb

import "time"

// MaxEmbeddingFilterCandidates is the hard candidate ceiling for filtered
// embedding queries. Lower per-query limits trade filtered recall for work.
const MaxEmbeddingFilterCandidates = 10_000

// SortOrder controls how [Tx.QueryCells] orders its results.
type SortOrder int

const (
	// SortByScore sorts results by composite relevance score descending (default).
	SortByScore SortOrder = iota
	// SortByConfidence sorts results by Provenance.Confidence descending.
	SortByConfidence
	// SortByRecency sorts results by ValidFrom descending.
	// Cells with no ValidFrom are sorted after cells that have one.
	SortByRecency
	// SortByCoord sorts results by Coord lexicographically ascending (Q then R).
	SortByCoord
)

// CellQuery is a composable predicate specification for [Tx.QueryCells].
//
// Active predicates are combined with AND. Each field documents its own
// default, including the OR within AnyTags and the bounds on result and scan size.
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

	// Query requires at least one whitespace-separated token to match RawContent
	// (substring), Tags (exact or prefix), or SourceID (exact), case-insensitively.
	// A blank query disables lexical matching.
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

	// MaxScanRows caps physical rows examined by cell/tag/source scans or coordinate
	// probes attempted by a radius scan. Zero means unlimited. If additional work
	// exists beyond the budget, QueryCells returns the partial sorted results and
	// [ErrQueryScanLimit]. With an embedding and a required tag, source, or
	// radius, a positive MaxScanRows selects exact scoring over the bounded
	// filter scan. It cannot be combined with EmbeddingCandidateLimit.
	MaxScanRows int

	// --- Embedding / vector ---

	// Embedding selects candidates and adds embedding similarity to their scores.
	// The vector length must match [DB.EmbeddingDimension] (auto-detected on
	// first [Tx.PutEmbedding]).
	// Returns empty results if no embeddings have been stored yet.
	// All other predicates, including Query, are applied to embedding candidates.
	// With a positive MaxResults, candidate retrieval widens until enough matches
	// are found, the search is exhausted, or EmbeddingCandidateLimit is reached.
	// With unlimited MaxResults and both budgets unset, all embeddings are
	// scanned exactly. A positive MaxScanRows selects bounded exact scoring.
	// Historical views return [ErrHistoricalDataUnavailable] when Embedding is set.
	Embedding []float32

	// EmbeddingCandidateLimit bounds the number of distinct embedding candidates
	// checked against the other predicates, even when MaxResults is unlimited.
	// Zero chooses a bounded adaptive default when MaxResults is positive.
	// Values above [MaxEmbeddingFilterCandidates] are rejected. A low limit can
	// exclude matches beyond the candidate window.
	EmbeddingCandidateLimit int
}

// CellQueryResult is one result from [Tx.QueryCells].
type CellQueryResult struct {
	// Cell contains the primary fields of the matching cell. Use [Tx.AssembleCellView]
	// or [Tx.LoadContext] to include related records.
	Cell CellView

	// Score is the composite relevance score used for SortByScore ordering.
	// The score is populated even when the result order uses a different field.
	Score float64

	// Explanation is a human-readable breakdown of the score and filter decisions.
	// Only populated when CellQuery.Explain is true.
	Explanation string
}

// CellQueryStats reports how an embedding query was evaluated.
type CellQueryStats struct {
	// EmbeddingPath is empty for non-vector queries. Flat means exact scoring,
	// which can still be limited by a row or candidate budget.
	EmbeddingPath EmbeddingSearchPath
	// EmbeddingCandidates is the number of distinct vector candidates examined.
	EmbeddingCandidates int
	// EmbeddingCandidateLimitReached means the candidate ceiling stopped
	// widening before the result quota was filled or the search was exhausted.
	EmbeddingCandidateLimitReached bool
}
