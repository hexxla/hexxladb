package hexxladb

import (
	"context"
	"slices"
	"sort"
	"strings"
)

// CellSearchConfig controls how [Tx.SearchCells] filters and scores cells.
//
// The API is intentionally forward-compatible: the Query field handles lexical
// search today. A future Embedding []float32 field will be added here for
// ANN-accelerated seed selection without breaking existing callers.
type CellSearchConfig struct {
	// Query is matched against RawContent (case-insensitive substring),
	// Tags (exact and prefix, case-insensitive), and SourceID (exact).
	// Empty query matches all cells (useful for pure filter/tag queries).
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
}

// CellSearchResult is one entry returned by [Tx.SearchCells].
// The Coord field can be used directly as a seed for [Tx.LoadContextPack]
// or collected into [MultiContextConfig.Centers] for multi-seed assembly.
type CellSearchResult struct {
	// Cell is the fully assembled view of the matching cell.
	Cell CellView
	// Score is the composite relevance score (higher is more relevant).
	// See the scoring table in the package documentation for breakdown.
	Score float64
}

const defaultSearchMaxResults = 20
const defaultSearchScanRadius = 32

// SearchCells scans visible cells and returns results ranked by relevance score.
//
// Scoring (contributions are additive):
//   - Query matches a tag exactly (case-insensitive):           +1.0
//   - Query is a prefix of a tag (case-insensitive):            +0.8
//   - Query found verbatim in RawContent:                       +0.6
//   - Query found case-insensitively in RawContent:             +0.5
//   - Query matches SourceID exactly:                           +0.3
//   - Confidence bonus:                                         +0.1 × Confidence
//
// Results are sorted descending by Score; ties broken by Confidence descending.
// An empty Query still applies all filter fields and scores by Confidence only.
func (tx *Tx) SearchCells(ctx context.Context, cfg CellSearchConfig) ([]CellSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}

	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultSearchMaxResults
	}
	scanRadius := cfg.MaxScanRadius
	if scanRadius <= 0 {
		scanRadius = defaultSearchScanRadius
	}

	// Determine the set of coordinates to scan.
	var coords []Coord
	if cfg.Radius > 0 {
		coords = WalkRings(nil, cfg.Center, cfg.Radius)
	} else {
		coords = WalkRings(nil, Coord{}, scanRadius)
	}

	queryLow := strings.ToLower(strings.TrimSpace(cfg.Query))

	var results []CellSearchResult
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		rec, ok, err := tx.GetCell(mustPack(c))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		// ── filter: RequireTags (AND) ────────────────────────────────────────
		if len(cfg.RequireTags) > 0 && !hasAllTags(rec.Tags, cfg.RequireTags) {
			continue
		}

		// ── filter: AnyTags (OR) ────────────────────────────────────────────
		if len(cfg.AnyTags) > 0 && !hasAnyTag(rec.Tags, cfg.AnyTags) {
			continue
		}

		// ── filter: confidence range ─────────────────────────────────────────
		conf := rec.Provenance.Confidence
		if cfg.MinConfidence > 0 && conf < cfg.MinConfidence {
			continue
		}
		if cfg.MaxConfidence > 0 && conf > cfg.MaxConfidence {
			continue
		}

		// ── filter: source ID ────────────────────────────────────────────────
		if cfg.SourceID != "" && rec.Provenance.SourceID != cfg.SourceID {
			continue
		}

		// ── scoring ──────────────────────────────────────────────────────────
		score := scoreCell(queryLow, rec.Tags, rec.RawContent, rec.Provenance.SourceID, conf)

		// If a non-empty query is set and the cell scores nothing, skip it.
		if queryLow != "" && score <= 0.1*conf {
			continue
		}

		results = append(results, CellSearchResult{
			Cell: CellView{
				Coord:      c,
				RawContent: rec.RawContent,
				Tags:       rec.Tags,
				Provenance: rec.Provenance,
				Validity:   rec.Validity,
			},
			Score: score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Cell.Provenance.Confidence > results[j].Cell.Provenance.Confidence
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// scoreCell computes a composite relevance score for a cell given a lower-cased query.
func scoreCell(queryLow string, tags []string, content, sourceID string, confidence float64) float64 {
	score := 0.1 * confidence // baseline confidence bonus

	if queryLow == "" {
		return score
	}

	contentLow := strings.ToLower(content)

	// Tag scoring.
	for _, tag := range tags {
		tagLow := strings.ToLower(tag)
		switch {
		case tagLow == queryLow:
			score += 1.0
		case strings.HasPrefix(tagLow, queryLow):
			score += 0.8
		}
	}

	// Content scoring: verbatim first, then case-insensitive.
	switch {
	case strings.Contains(content, queryLow):
		score += 0.6
	case strings.Contains(contentLow, queryLow):
		score += 0.5
	}

	// Source ID exact match.
	if sourceID == queryLow {
		score += 0.3
	}

	return score
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
