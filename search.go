package hexxladb

import (
	"context"
	"slices"
	"strings"
)

// CellSearchConfig controls how [Tx.SearchCells] filters and scores cells.
type CellSearchConfig struct {
	// Query is matched against RawContent (case-insensitive substring),
	// Tags (exact and prefix, case-insensitive), and SourceID (case-insensitive
	// exact match). At least one whitespace-separated token must match.
	// An empty query disables the lexical filter.
	Query string

	// RequireTags filters to cells that carry ALL of these tags (AND semantics).
	RequireTags []string
	// AnyTags filters to cells that carry AT LEAST ONE of these tags (OR semantics).
	// If both RequireTags and AnyTags are set, both conditions must hold.
	AnyTags []string

	// MinConfidence excludes cells with Provenance.Confidence below this value.
	// Zero means no lower bound.
	MinConfidence float64
	// MaxConfidence excludes cells with Provenance.Confidence above this value.
	// Zero means no upper bound.
	MaxConfidence float64

	// SourceID restricts results to cells whose Provenance.SourceID matches exactly.
	// Empty means no restriction.
	SourceID string

	// Center and Radius apply a spatial restriction: only cells within Radius rings
	// of Center are considered. Zero Radius with zero Center means no restriction.
	Center Coord
	Radius int

	// MaxResults caps the number of returned results (default 20; 0 uses default).
	MaxResults int

	// MaxScanRadius controls how far from origin to walk when Radius is zero
	// (default 32). Increase for sparse or geographically wide databases.
	MaxScanRadius int

	// Embedding enables ANN-accelerated seed selection. When non-nil, the
	// query planner uses [Tx.SearchByEmbedding] to narrow the candidate set
	// and boosts scores by embedding similarity. The vector length must match
	// the database's embedding dimension (auto-detected on first [Tx.PutEmbedding]).
	Embedding []float32
}

// CellSearchResult is one entry returned by [Tx.SearchCells].
// The Coord field can be used directly as a seed for [Tx.LoadContext]
// or collected into [LoadContextConfig.Seeds] for multi-seed assembly.
type CellSearchResult struct {
	// Cell is the fully assembled view of the matching cell.
	Cell CellView
	// Score is the composite relevance score (higher is more relevant).
	// See the scoring table in the package documentation for breakdown.
	Score float64
}

// SearchCells scans visible cells and returns results ranked by relevance score.
//
// SearchCells is a convenience wrapper over [Tx.QueryCells]. For full control
// over sort order, temporal filters, ExcludeTags, and Explain mode use
// [Tx.QueryCells] directly.
//
// Multi-word queries are tokenized on whitespace. Each token is scored
// independently and contributions are summed, including when an embedding
// is supplied.
//
// Scoring per token (contributions are additive across all tokens):
//   - Token matches a tag exactly (case-insensitive):           +1.0
//   - Token is a prefix of a tag (case-insensitive):            +0.8
//   - Lowercased token found verbatim in RawContent:            +0.6
//   - Token found case-insensitively in RawContent:             +0.5
//   - Token matches SourceID exactly (case-insensitive):        +0.3
//   - Confidence bonus (once, not per token):                   +0.1 × Confidence
//
// Each tag contributes at most once per token (exact beats prefix).
// Results are sorted descending by Score; ties broken by Confidence descending.
// An empty Query still applies all other filters and scores by Confidence plus
// embedding similarity when supplied.
func (tx *Tx) SearchCells(ctx context.Context, cfg CellSearchConfig) ([]CellSearchResult, error) {
	q := CellQuery{
		Query:         cfg.Query,
		RequireTags:   cfg.RequireTags,
		AnyTags:       cfg.AnyTags,
		SourceID:      cfg.SourceID,
		MinConfidence: cfg.MinConfidence,
		MaxConfidence: cfg.MaxConfidence,
		Center:        cfg.Center,
		Radius:        cfg.Radius,
		MaxResults:    cfg.MaxResults,
		SortBy:        SortByScore,
		Embedding:     cfg.Embedding,
	}
	if q.MaxResults <= 0 {
		q.MaxResults = defaultQueryMaxResults
	}
	if q.Radius <= 0 {
		scanR := cfg.MaxScanRadius
		if scanR <= 0 {
			scanR = defaultQueryScanRadius
		}
		q.Radius = scanR
	}

	qrs, err := tx.QueryCells(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]CellSearchResult, len(qrs))
	for i, r := range qrs {
		out[i] = CellSearchResult{Cell: r.Cell, Score: r.Score}
	}
	return out, nil
}

// scoreCell computes relevance and reports whether the lexical filter matched.
// queryLow is already lowercased. Each whitespace-separated token contributes
// independently to the score.
func scoreCell(queryLow string, tags []string, content, sourceID string, confidence float64) (score float64, matched bool) {
	score = 0.1 * confidence // baseline confidence bonus (applied once)

	if queryLow == "" {
		return score, true
	}

	// Repeated tags count only once per token, regardless of case.
	tagsLow := make([]string, 0, len(tags))
	seenTags := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tagLow := strings.ToLower(tag)
		if _, seen := seenTags[tagLow]; seen {
			continue
		}
		seenTags[tagLow] = struct{}{}
		tagsLow = append(tagsLow, tagLow)
	}
	contentLow := strings.ToLower(content)
	sourceIDLow := strings.ToLower(sourceID)

	for tok := range strings.FieldsSeq(queryLow) {
		// Tag scoring: each tag scores at most once per token (exact beats prefix).
		for _, tagLow := range tagsLow {
			switch {
			case tagLow == tok:
				score += 1.0
				matched = true
			case strings.HasPrefix(tagLow, tok):
				score += 0.8
				matched = true
			}
		}

		// Content scoring: verbatim match scores higher than case-insensitive.
		switch {
		case strings.Contains(content, tok):
			score += 0.6
			matched = true
		case strings.Contains(contentLow, tok):
			score += 0.5
			matched = true
		}

		// Source ID exact match per token.
		if sourceIDLow == tok {
			score += 0.3
			matched = true
		}
	}

	return score, matched
}

// hasAllTags reports whether tags contains every element of required (case-sensitive).
func hasAllTags(tags, required []string) bool {
	for _, r := range required {
		if !slices.Contains(tags, r) {
			return false
		}
	}
	return true
}

// hasAnyTag reports whether tags contains at least one element of candidates (case-sensitive).
func hasAnyTag(tags, candidates []string) bool {
	for _, c := range candidates {
		if slices.Contains(tags, c) {
			return true
		}
	}
	return false
}
