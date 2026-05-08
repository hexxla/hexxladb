package hexxladb

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

const defaultQueryMaxResults = 20
const defaultQueryScanRadius = 32

// QueryCells executes a [CellQuery] against the transaction's snapshot and
// returns matching cells ordered according to [CellQuery.SortBy].
//
// The planner picks the cheapest available index:
//   - Embedding set    →  ANN via [Tx.SearchByEmbedding] (HNSW or flat scan)
//   - RequireTags set  →  tag secondary index (most selective tag first)
//   - SourceID set     →  source secondary index
//   - After/Before set →  time/ week-bucket index
//   - Center+Radius    →  ring walk around Center
//   - fallback         →  full scan via ring walk from origin
//
// After the primary index narrows the candidate set, all remaining predicates
// are applied as in-memory filters. Results are scored and sorted according to
// [CellQuery.SortBy] then capped to [CellQuery.MaxResults].
func (tx *Tx) QueryCells(ctx context.Context, q CellQuery) ([]CellQueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tx == nil || tx.db == nil {
		return nil, ErrClosed
	}

	maxResults := q.MaxResults
	if maxResults <= 0 {
		maxResults = defaultQueryMaxResults
	}

	queryLow := strings.ToLower(strings.TrimSpace(q.Query))

	// ── plan: choose primary scan strategy ───────────────────────────────────
	// The embedding path needs a scores map, so it materialises a small bounded
	// candidate set (MaxResults×2) and falls through to the fused loop below.
	// All other paths use a fused yield callback: filter+score happen inside the
	// scan callback itself, so no intermediate []CellRecord slice is allocated.
	var results []CellQueryResult
	var embScores map[lattice.PackedCoord]float64

	if len(q.Embedding) > 0 {
		// Embedding: bounded ANN fetch, then fused filter+score below.
		candidates, scores, err := tx.scanByEmbedding(q.Embedding, maxResults)
		if err != nil {
			return nil, err
		}
		embScores = scores
		for _, rec := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if r, ok := tx.scoreRecord(q, rec, queryLow, embScores); ok {
				results = append(results, r)
			}
		}
	} else {
		// All other paths: fused scan — filter+score inline, no candidates slice.
		yield := func(rec record.CellRecord) bool {
			if ctx.Err() != nil {
				return false
			}
			if r, ok := tx.scoreRecord(q, rec, queryLow, nil); ok {
				results = append(results, r)
			}
			return true
		}

		var err error
		switch {
		case len(q.RequireTags) > 0:
			err = tx.scanByTagFused(ctx, q.RequireTags[0], q.MaxScanRows, yield)
		case q.SourceID != "":
			err = tx.scanBySourceFused(ctx, q.SourceID, q.MaxScanRows, yield)
		case !q.After.IsZero() || !q.Before.IsZero():
			err = tx.scanByTimeRangeFused(ctx, q.After, q.Before, yield)
		case q.Radius > 0:
			err = tx.scanByRadiusFused(ctx, q.Center, q.Radius, q.MaxScanRows, yield)
		default:
			err = tx.scanByRadiusFused(ctx, Coord{}, defaultQueryScanRadius, q.MaxScanRows, yield)
		}
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	// ── sort (required: caller expects ordered results) ────────────────────────
	sortResults(results, q.SortBy)

	// ── limit ─────────────────────────────────────────────────────────────────
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// scoreRecord applies predicates, scores, and builds a CellQueryResult.
// Returns (result, true) if the record passes all filters; (zero, false) otherwise.
func (tx *Tx) scoreRecord(q CellQuery, rec record.CellRecord, queryLow string, embScores map[lattice.PackedCoord]float64) (CellQueryResult, bool) {
	coord, err := lattice.Unpack(rec.Key)
	if err != nil {
		return CellQueryResult{}, false
	}
	c := Coord(coord)

	if !applyPredicates(q, rec, c, queryLow) {
		return CellQueryResult{}, false
	}

	score := scoreCell(queryLow, rec.Tags, rec.RawContent, rec.Provenance.SourceID, rec.Provenance.Confidence)

	if embScores != nil {
		if es, ok := embScores[rec.Key]; ok {
			score += es
		}
	}

	if queryLow != "" && embScores == nil && score <= 0.1*rec.Provenance.Confidence {
		return CellQueryResult{}, false
	}

	var explanation string
	if q.Explain {
		explanation = buildExplanation(q, rec, c, score, queryLow)
	}

	return CellQueryResult{
		Cell: CellView{
			Coord:      c,
			RawContent: rec.RawContent,
			Tags:       rec.Tags,
			Provenance: rec.Provenance,
			Validity:   rec.Validity,
		},
		Score:       score,
		Explanation: explanation,
	}, true
}

// ── scanners (fused: filter+score called inline, no intermediate slice) ───────

// scanByTagFused walks the tag index and calls yield for each decoded record.
// yield returning false stops the walk early.
func (tx *Tx) scanByTagFused(ctx context.Context, tag string, maxScanRows int, yield func(record.CellRecord) bool) error {
	scanned := 0
	if err := tx.AscendCellsByTag(ctx, tag, func(r record.CellRecord) bool {
		scanned++
		if !yield(r) {
			return false
		}
		return maxScanRows <= 0 || scanned < maxScanRows
	}); err != nil {
		return fmt.Errorf("hexxladb: QueryCells tag scan %q: %w", tag, err)
	}
	return nil
}

// scanBySourceFused walks the source index and calls yield for each decoded record.
func (tx *Tx) scanBySourceFused(ctx context.Context, sourceID string, maxScanRows int, yield func(record.CellRecord) bool) error {
	scanned := 0
	if err := tx.AscendCellsBySource(ctx, sourceID, func(r record.CellRecord) bool {
		scanned++
		if !yield(r) {
			return false
		}
		return maxScanRows <= 0 || scanned < maxScanRows
	}); err != nil {
		return fmt.Errorf("hexxladb: QueryCells source scan %q: %w", sourceID, err)
	}
	return nil
}

// scanByTimeRangeFused walks the time index and calls yield for each record in [after, before).
func (tx *Tx) scanByTimeRangeFused(ctx context.Context, after, before time.Time, yield func(record.CellRecord) bool) error {
	var bucketFrom, bucketTo int64
	if !after.IsZero() {
		bucketFrom = after.UnixNano() / index.WeekNanos
	}
	if !before.IsZero() {
		bucketTo = before.UnixNano()/index.WeekNanos + 1
	} else {
		bucketTo = 1<<62 - 1
	}

	from, _ := index.TimeRangePrefix(bucketFrom)
	_, to := index.TimeRangePrefix(bucketTo)

	seen := make(map[lattice.PackedCoord]struct{})

	if err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if ctx.Err() != nil {
			return false
		}
		_, pc, err := index.ParseTimeKey(k)
		if err != nil {
			return true
		}
		if _, dup := seen[pc]; dup {
			return true
		}
		seen[pc] = struct{}{}
		rec, ok, e := tx.GetCell(pc)
		if e != nil || !ok {
			return true
		}
		if rec.Validity.ValidFrom == nil {
			return true
		}
		vf := time.Unix(0, *rec.Validity.ValidFrom).UTC()
		if !after.IsZero() && !vf.After(after) {
			return true
		}
		if !before.IsZero() && !vf.Before(before) {
			return true
		}
		return yield(rec)
	}); err != nil {
		return fmt.Errorf("hexxladb: QueryCells time scan: %w", err)
	}
	return ctx.Err()
}

// scanByRadiusFused walks rings from center up to radius using a lazy iterator
// and calls yield for each present cell. maxScanRows=0 means unlimited.
func (tx *Tx) scanByRadiusFused(ctx context.Context, center Coord, radius, maxScanRows int, yield func(record.CellRecord) bool) error {
	scanned := 0
	for cp := range lattice.WalkRingsPackedSeq(center, radius) {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		scanned++
		if !yield(rec) {
			return nil
		}
		if maxScanRows > 0 && scanned >= maxScanRows {
			return nil
		}
	}
	return nil
}

// scanByEmbedding fetches ANN candidates (bounded by MaxResults×2).
// The embedding path is inherently bounded and needs the scores map,
// so it keeps the small intermediate slice.
func (tx *Tx) scanByEmbedding(vec []float32, maxResults int) ([]record.CellRecord, map[lattice.PackedCoord]float64, error) {
	fetchK := max(maxResults*2, 20)
	hits, err := tx.SearchByEmbedding(vec, EmbeddingSearchConfig{MaxResults: fetchK})
	if err != nil {
		return nil, nil, err
	}
	recs := make([]record.CellRecord, 0, len(hits))
	scores := make(map[lattice.PackedCoord]float64, len(hits))
	for _, h := range hits {
		rec, ok, getErr := tx.GetCell(h.Coord)
		if getErr != nil || !ok {
			continue
		}
		recs = append(recs, rec)
		scores[rec.Key] = h.Score
	}
	return recs, scores, nil
}

// ── predicate pipeline ────────────────────────────────────────────────────────

func applyPredicates(q CellQuery, rec record.CellRecord, c Coord, _ string) bool {
	// RequireTags AND
	if len(q.RequireTags) > 0 && !hasAllTags(rec.Tags, q.RequireTags) {
		return false
	}
	// AnyTags OR
	if len(q.AnyTags) > 0 && !hasAnyTag(rec.Tags, q.AnyTags) {
		return false
	}
	// ExcludeTags NOT
	if len(q.ExcludeTags) > 0 {
		for _, ex := range q.ExcludeTags {
			if slices.Contains(rec.Tags, ex) {
				return false
			}
		}
	}
	// SourceID
	if q.SourceID != "" && rec.Provenance.SourceID != q.SourceID {
		return false
	}
	// Confidence range
	conf := rec.Provenance.Confidence
	if q.MinConfidence > 0 && conf < q.MinConfidence {
		return false
	}
	if q.MaxConfidence > 0 && conf > q.MaxConfidence {
		return false
	}
	// Temporal (re-checked for non-time-index scan paths)
	if rec.Validity.ValidFrom != nil {
		vf := time.Unix(0, *rec.Validity.ValidFrom).UTC()
		if !q.After.IsZero() && !vf.After(q.After) {
			return false
		}
		if !q.Before.IsZero() && !vf.Before(q.Before) {
			return false
		}
	} else if !q.After.IsZero() || !q.Before.IsZero() {
		// Cell has no ValidFrom: excluded from temporal queries.
		return false
	}
	// Spatial (re-checked for non-radius scan paths)
	if q.Radius > 0 {
		if hexDistance(c, q.Center) > q.Radius {
			return false
		}
	}
	return true
}

// hexDistance returns the hex grid distance between two axial coordinates.
func hexDistance(a, b Coord) int {
	dq := a.Q - b.Q
	dr := a.R - b.R
	ds := (-a.Q - a.R) - (-b.Q - b.R)
	d := dq
	if dq < 0 {
		d = -dq
	}
	dr2 := dr
	if dr < 0 {
		dr2 = -dr
	}
	ds2 := ds
	if ds < 0 {
		ds2 = -ds
	}
	if dr2 > d {
		d = dr2
	}
	if ds2 > d {
		d = ds2
	}
	return d
}

// ── sorting ───────────────────────────────────────────────────────────────────

func sortResults(results []CellQueryResult, order SortOrder) {
	switch order {
	case SortByConfidence:
		sort.Slice(results, func(i, j int) bool {
			return results[i].Cell.Provenance.Confidence > results[j].Cell.Provenance.Confidence
		})
	case SortByRecency:
		sort.Slice(results, func(i, j int) bool {
			vi := validFromNanos(results[i].Cell)
			vj := validFromNanos(results[j].Cell)
			return vi > vj
		})
	case SortByCoord:
		sort.Slice(results, func(i, j int) bool {
			ci, cj := results[i].Cell.Coord, results[j].Cell.Coord
			if ci.Q != cj.Q {
				return ci.Q < cj.Q
			}
			return ci.R < cj.R
		})
	default: // SortByScore
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			return results[i].Cell.Provenance.Confidence > results[j].Cell.Provenance.Confidence
		})
	}
}

func validFromNanos(cv CellView) int64 {
	if cv.Validity.ValidFrom != nil {
		return *cv.Validity.ValidFrom
	}
	return 0
}

// ── explain ───────────────────────────────────────────────────────────────────

func buildExplanation(_ CellQuery, rec record.CellRecord, c Coord, score float64, queryLow string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "coord=(%d,%d) score=%.2f conf=%.2f", c.Q, c.R, score, rec.Provenance.Confidence)
	if queryLow != "" {
		fmt.Fprintf(&sb, " query=%q", queryLow)
		tokens := strings.Fields(queryLow)
		contentLow := strings.ToLower(rec.RawContent)
		sourceIDLow := strings.ToLower(rec.Provenance.SourceID)
		for _, tok := range tokens {
			for _, tag := range rec.Tags {
				tagLow := strings.ToLower(tag)
				if tagLow == tok {
					fmt.Fprintf(&sb, " [tag:exact:%s]", tok)
				} else if strings.HasPrefix(tagLow, tok) {
					fmt.Fprintf(&sb, " [tag:prefix:%s]", tok)
				}
			}
			switch {
			case strings.Contains(rec.RawContent, tok):
				fmt.Fprintf(&sb, " [content:verbatim:%s]", tok)
			case strings.Contains(contentLow, tok):
				fmt.Fprintf(&sb, " [content:icase:%s]", tok)
			}
			if sourceIDLow == tok {
				fmt.Fprintf(&sb, " [source:exact:%s]", tok)
			}
		}
	}
	if len(rec.Tags) > 0 {
		fmt.Fprintf(&sb, " tags=%v", rec.Tags)
	}
	if rec.Validity.ValidFrom != nil {
		vf := time.Unix(0, *rec.Validity.ValidFrom).UTC()
		fmt.Fprintf(&sb, " validFrom=%s", vf.Format(time.RFC3339))
	}
	return sb.String()
}
