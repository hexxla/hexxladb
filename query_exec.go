package hexxladb

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hexxla/hexxladb/internal/engine"
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
//   - After/Before set →  complete primary scan (correct across signed time buckets)
//   - Center+Radius    →  ring walk around Center
//   - fallback         →  complete primary scan
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
	if q.Radius < 0 {
		return nil, fmt.Errorf("%w: radius must be non-negative", ErrInvalidArgument)
	}
	if q.Radius > 0 {
		if err := validatePackedRadius(q.Center, q.Radius); err != nil {
			return nil, err
		}
	}

	maxResults := q.MaxResults

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
			// A signed week bucket is not lexicographically ordered by its
			// on-disk uint64 representation. Scan primary cells so ranges that
			// cross the Unix epoch remain complete without changing the format.
			err = tx.scanAllCellsFused(ctx, q.MaxScanRows, yield)
		case q.Radius > 0:
			err = tx.scanByRadiusFused(ctx, q.Center, q.Radius, q.MaxScanRows, yield)
		default:
			err = tx.scanAllCellsFused(ctx, q.MaxScanRows, yield)
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
	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// scanAllCellsFused walks the complete cell/ primary keyspace and yields each
// snapshot-visible logical cell once. maxScanRows=0 means unlimited.
func (tx *Tx) scanAllCellsFused(ctx context.Context, maxScanRows int, yield func(record.CellRecord) bool) error {
	from := []byte(index.CellPrefix)
	to := []byte("cell0") // sorts immediately after every cell/ key
	v1Len := len(index.CellPrefix) + index.PackedCoordKeyLen
	var previous lattice.PackedCoord
	havePrevious := false
	scanned := 0
	var scanErr error

	err := tx.AscendRange(from, to, func(k, _ []byte) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		if !bytes.HasPrefix(k, from) || len(k) < v1Len {
			return true
		}
		coord, err := index.ParseCellKey(k[:v1Len])
		if err != nil {
			return true
		}
		if havePrevious && coord == previous {
			return true
		}
		previous = coord
		havePrevious = true
		rec, ok, err := tx.GetCell(coord)
		if err != nil {
			scanErr = err
			return false
		}
		if !ok {
			return true
		}
		scanned++
		if !yield(rec) {
			return false
		}
		return maxScanRows <= 0 || scanned < maxScanRows
	})
	if err != nil {
		return fmt.Errorf("hexxladb: QueryCells full scan: %w", err)
	}
	return scanErr
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

	// Pre-lowercase tags once per record so scoreCell does not allocate per-call.
	tagsLow := make([]string, len(rec.Tags))
	for i, t := range rec.Tags {
		tagsLow[i] = strings.ToLower(t)
	}

	score := scoreCell(queryLow, tagsLow, rec.RawContent, rec.Provenance.SourceID, rec.Provenance.Confidence)

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
	if maxResults <= 0 {
		return tx.scanAllEmbeddings(vec)
	}
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

func (tx *Tx) scanAllEmbeddings(vec []float32) ([]record.CellRecord, map[lattice.PackedCoord]float64, error) {
	if err := validateEmbeddingVector(vec); err != nil {
		return nil, nil, err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		return nil, nil, nil
	}
	if len(vec) != int(dim) {
		return nil, nil, fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(vec))
	}
	candidates, err := tx.collectEmbeddingCandidates()
	if err != nil {
		return nil, nil, err
	}
	recs := make([]record.CellRecord, 0, len(candidates))
	scores := make(map[lattice.PackedCoord]float64, len(candidates))
	metric := tx.db.eng.EmbeddingMetric()
	for _, candidate := range candidates {
		rec, ok, err := tx.GetCell(candidate.coord)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		recs = append(recs, rec)
		scores[candidate.coord] = engine.Similarity(vec, candidate.vec, metric)
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
		slices.SortFunc(results, func(a, b CellQueryResult) int {
			ac, bc := a.Cell.Provenance.Confidence, b.Cell.Provenance.Confidence
			if ac > bc {
				return -1
			}
			if ac < bc {
				return 1
			}
			return 0
		})
	case SortByRecency:
		slices.SortFunc(results, func(a, b CellQueryResult) int {
			vi, vj := validFromNanos(a.Cell), validFromNanos(b.Cell)
			if vi > vj {
				return -1
			}
			if vi < vj {
				return 1
			}
			return 0
		})
	case SortByCoord:
		slices.SortFunc(results, func(a, b CellQueryResult) int {
			ci, cj := a.Cell.Coord, b.Cell.Coord
			if ci.Q != cj.Q {
				return ci.Q - cj.Q
			}
			return ci.R - cj.R
		})
	default: // SortByScore
		slices.SortFunc(results, func(a, b CellQueryResult) int {
			if a.Score != b.Score {
				if a.Score > b.Score {
					return -1
				}
				return 1
			}
			ac, bc := a.Cell.Provenance.Confidence, b.Cell.Provenance.Confidence
			if ac > bc {
				return -1
			}
			if ac < bc {
				return 1
			}
			return 0
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
