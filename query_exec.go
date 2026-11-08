package hexxladb

import (
	"bytes"
	"cmp"
	"context"
	"errors"
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
const preferredSpatialPlanProbeLimit = 4096

// QueryCells executes a [CellQuery] against the transaction's snapshot and
// returns matching cells ordered according to [CellQuery.SortBy].
//
// The planner uses a bounded cost heuristic:
//   - Embedding set    →  ANN via [Tx.SearchByEmbedding] (HNSW or flat scan)
//   - Small radius     →  ring walk when its exact probe count is cheaper than
//     the configured scan budget or the conservative spatial threshold
//   - RequireTags set  →  first caller-provided tag secondary index
//   - SourceID set     →  source secondary index
//   - After/Before set →  complete primary scan (correct across signed time buckets)
//   - Center+Radius    →  ring walk around Center
//   - fallback         →  complete primary scan
//
// After the primary index narrows the candidate set, all remaining predicates
// are applied as in-memory filters. Results are scored and sorted according to
// [CellQuery.SortBy] then capped to [CellQuery.MaxResults].
func (tx *Tx) QueryCells(ctx context.Context, q CellQuery) ([]CellQueryResult, error) {
	if err := tx.validateCellQuery(ctx, q); err != nil {
		return nil, err
	}

	maxResults := q.MaxResults
	queryLow := strings.ToLower(strings.TrimSpace(q.Query))

	var results []CellQueryResult
	var scanLimitErr error

	if len(q.Embedding) > 0 {
		var err error
		results, err = tx.queryCellsByEmbedding(ctx, q, queryLow, maxResults)
		if err != nil {
			return nil, err
		}
	} else {
		results, scanLimitErr = tx.queryCellsByIndex(ctx, q, queryLow)
		if scanLimitErr != nil && !errors.Is(scanLimitErr, ErrQueryScanLimit) {
			return nil, scanLimitErr
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
	return results, scanLimitErr
}

func (tx *Tx) validateCellQuery(ctx context.Context, q CellQuery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || tx.db == nil {
		return ErrClosed
	}
	if q.Radius < 0 || q.Radius > MaxSpatialScanRadius {
		return fmt.Errorf("%w: radius must be between 0 and %d", ErrInvalidArgument, MaxSpatialScanRadius)
	}
	if q.MaxScanRows < 0 {
		return fmt.Errorf("%w: MaxScanRows must not be negative", ErrInvalidArgument)
	}
	if len(q.Embedding) > 0 {
		if err := tx.checkCurrentData(); err != nil {
			return err
		}
	}
	if len(q.Embedding) > 0 && q.MaxScanRows > 0 {
		return fmt.Errorf("%w: MaxScanRows cannot bound embedding graph reads", ErrInvalidArgument)
	}
	if q.EmbeddingCandidateLimit < 0 || q.EmbeddingCandidateLimit > MaxEmbeddingFilterCandidates {
		return fmt.Errorf("%w: EmbeddingCandidateLimit must be between 0 and %d", ErrInvalidArgument, MaxEmbeddingFilterCandidates)
	}
	if len(q.Embedding) == 0 && q.EmbeddingCandidateLimit != 0 {
		return fmt.Errorf("%w: EmbeddingCandidateLimit requires Embedding", ErrInvalidArgument)
	}
	if q.Radius > 0 {
		if err := validatePackedRadius(q.Center, q.Radius); err != nil {
			return err
		}
	}
	return nil
}

func (tx *Tx) queryCellsByIndex(ctx context.Context, q CellQuery, queryLow string) ([]CellQueryResult, error) {
	var results []CellQueryResult
	yield := func(rec record.CellRecord) bool {
		if ctx.Err() != nil {
			return false
		}
		if r, ok := scoreRecord(q, rec, queryLow, 0); ok {
			results = append(results, r)
		}
		return true
	}

	var err error
	indexSourceID := strings.TrimSpace(q.SourceID)
	switch {
	case preferSpatialPlan(q):
		err = tx.scanByRadiusFused(ctx, q.Center, q.Radius, q.MaxScanRows, yield)
	case len(q.RequireTags) > 0:
		err = tx.scanByTagFused(ctx, q.RequireTags[0], q.MaxScanRows, yield)
	case indexSourceID != "":
		err = tx.scanBySourceFused(ctx, indexSourceID, q.MaxScanRows, yield)
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
	return results, err
}

func preferSpatialPlan(q CellQuery) bool {
	if q.Radius <= 0 {
		return false
	}
	probes := hexDiskCellCount(q.Radius)
	if q.MaxScanRows > 0 {
		return probes <= q.MaxScanRows
	}
	return probes <= preferredSpatialPlanProbeLimit
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
		if maxScanRows > 0 && scanned >= maxScanRows {
			scanErr = ErrQueryScanLimit
			return false
		}
		scanned++
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
		if !yield(rec) {
			return false
		}
		return true
	})
	if err != nil {
		return fmt.Errorf("hexxladb: QueryCells full scan: %w", err)
	}
	if scanErr != nil {
		return scanErr
	}
	return nil
}

// scoreRecord applies predicates, scores, and builds a CellQueryResult.
// Returns (result, true) if the record passes all filters; (zero, false) otherwise.
func scoreRecord(q CellQuery, rec record.CellRecord, queryLow string, embeddingScore float64) (CellQueryResult, bool) {
	coord, err := lattice.Unpack(rec.Key)
	if err != nil {
		return CellQueryResult{}, false
	}
	c := Coord(coord)

	if !applyPredicates(q, rec, c) {
		return CellQueryResult{}, false
	}

	score, matched := scoreCell(queryLow, rec.Tags, rec.RawContent, rec.Provenance.SourceID, rec.Provenance.Confidence)
	if !matched {
		return CellQueryResult{}, false
	}
	score += embeddingScore

	var explanation string
	if q.Explain {
		explanation = buildExplanation(q, rec, c, score, queryLow, embeddingScore)
	}

	cell := CellView{
		Coord: c, RawContent: rec.RawContent, Tags: rec.Tags,
		Provenance: rec.Provenance, Validity: rec.Validity,
	}
	if rec.ClusterHint != nil {
		if hint, err := lattice.Unpack(*rec.ClusterHint); err == nil {
			cell.ClusterHint = new(hint)
		}
	}
	return CellQueryResult{
		Cell:        cell,
		Score:       score,
		Explanation: explanation,
	}, true
}

// ── scanners (fused: filter+score called inline, no intermediate slice) ───────

// scanByTagFused walks the tag index and calls yield for each decoded record.
// yield returning false stops the walk early.
func (tx *Tx) scanByTagFused(ctx context.Context, tag string, maxScanRows int, yield func(record.CellRecord) bool) error {
	limitReached, err := tx.ascendCellsByTag(ctx, tag, maxScanRows, yield)
	if err != nil {
		return fmt.Errorf("hexxladb: QueryCells tag scan %q: %w", tag, err)
	}
	if limitReached {
		return ErrQueryScanLimit
	}
	return nil
}

// scanBySourceFused walks the source index and calls yield for each decoded record.
func (tx *Tx) scanBySourceFused(ctx context.Context, sourceID string, maxScanRows int, yield func(record.CellRecord) bool) error {
	limitReached, err := tx.ascendCellsBySource(ctx, sourceID, maxScanRows, yield)
	if err != nil {
		return fmt.Errorf("hexxladb: QueryCells source scan %q: %w", sourceID, err)
	}
	if limitReached {
		return ErrQueryScanLimit
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
		if maxScanRows > 0 && scanned >= maxScanRows {
			return ErrQueryScanLimit
		}
		scanned++
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !yield(rec) {
			return nil
		}
	}
	return nil
}

func (tx *Tx) queryCellsByEmbedding(ctx context.Context, q CellQuery, queryLow string, maxResults int) ([]CellQueryResult, error) {
	if maxResults <= 0 && q.EmbeddingCandidateLimit == 0 {
		return tx.queryCellsByFlatEmbedding(ctx, q, queryLow)
	}

	candidateLimit := q.EmbeddingCandidateLimit
	if candidateLimit == 0 {
		candidateLimit = min(MaxEmbeddingFilterCandidates, max(100, multiplyCapped(maxResults, 16, MaxEmbeddingFilterCandidates)))
	}
	fetchK := candidateLimit
	if maxResults > 0 {
		fetchK = min(candidateLimit, max(20, multiplyCapped(maxResults, 2, candidateLimit)))
	}
	seen := make(map[lattice.PackedCoord]struct{}, fetchK)
	results := make([]CellQueryResult, 0, min(fetchK, max(maxResults, 0)))
	for {
		hits, _, err := tx.searchByEmbedding(ctx, q.Embedding, EmbeddingSearchConfig{MaxResults: fetchK})
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Approximate result sets from successive searches need not be nested.
			if len(seen) == candidateLimit {
				break
			}
			if _, duplicate := seen[hit.Coord]; duplicate {
				continue
			}
			seen[hit.Coord] = struct{}{}
			rec, ok, err := tx.GetCell(hit.Coord)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if result, matched := scoreRecord(q, rec, queryLow, hit.Score); matched {
				results = append(results, result)
			}
		}
		if (maxResults > 0 && len(results) >= maxResults) || len(hits) < fetchK || len(seen) == candidateLimit || fetchK == candidateLimit {
			return results, nil
		}
		fetchK = min(candidateLimit, fetchK*2)
	}
}

func multiplyCapped(value, factor, limit int) int {
	if value >= limit/factor {
		return limit
	}
	return value * factor
}

func (tx *Tx) queryCellsByFlatEmbedding(ctx context.Context, q CellQuery, queryLow string) ([]CellQueryResult, error) {
	if err := validateEmbeddingVector(q.Embedding); err != nil {
		return nil, err
	}
	dim := tx.db.eng.EmbeddingDim()
	results := []CellQueryResult{}
	if dim == 0 {
		return results, nil
	}
	if len(q.Embedding) != int(dim) {
		return nil, fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(q.Embedding))
	}
	similarity := engine.SimilarityFunc(tx.db.eng.EmbeddingMetric())
	err := tx.scanEmbeddings(ctx, func(coord lattice.PackedCoord, vec []float32) error {
		rec, ok, err := tx.GetCell(coord)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if result, matched := scoreRecord(q, rec, queryLow, similarity(q.Embedding, vec)); matched {
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// ── predicate pipeline ────────────────────────────────────────────────────────

func applyPredicates(q CellQuery, rec record.CellRecord, c Coord) bool {
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
		if c.Distance(q.Center) > q.Radius {
			return false
		}
	}
	return true
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
			at, bt := a.Cell.Validity.ValidFrom, b.Cell.Validity.ValidFrom
			switch {
			case at == nil && bt == nil:
				return 0
			case at == nil:
				return 1
			case bt == nil:
				return -1
			default:
				return cmp.Compare(*bt, *at)
			}
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

// ── explain ───────────────────────────────────────────────────────────────────

func buildExplanation(q CellQuery, rec record.CellRecord, c Coord, score float64, queryLow string, embeddingScore float64) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "coord=(%d,%d) score=%.2f conf=%.2f", c.Q, c.R, score, rec.Provenance.Confidence)
	if len(q.Embedding) > 0 {
		fmt.Fprintf(&sb, " embedding=%.2f", embeddingScore)
	}
	if queryLow != "" {
		fmt.Fprintf(&sb, " query=%q", queryLow)
		contentLow := strings.ToLower(rec.RawContent)
		sourceIDLow := strings.ToLower(rec.Provenance.SourceID)
		for tok := range strings.FieldsSeq(queryLow) {
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
