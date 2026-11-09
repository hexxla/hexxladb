package hexxladb

import (
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

const preferredSpatialPlanProbeLimit = 4096

// QueryCells executes a [CellQuery] against the transaction's snapshot and
// returns matching cells ordered according to [CellQuery.SortBy].
//
// The planner prefers a small spatial disk, then the first required tag or
// source index. Other queries scan the spatial disk or primary cells. Time
// bounds are applied to the visible records and can span the Unix epoch.
//
// Vector queries use candidate retrieval through HNSW or flat scoring. A
// positive [CellQuery.MaxScanRows] with a required tag, source, or radius
// scores the bounded filter scan exactly. With unlimited results and neither
// budget set, the query scores every stored vector exactly.
//
// Lexical scoring sums each token's contributions: exact tag +1.0, tag prefix
// +0.8, a lowercased token found verbatim in content +0.6, otherwise a
// case-insensitive content match +0.5, and exact case-insensitive source +0.3.
// Each distinct case-folded tag contributes at most once per token. Confidence
// adds 0.1 times its value once per cell; embedding similarity is added when
// supplied. A blank query disables lexical filtering.
//
// Results are filtered, sorted, and capped to [CellQuery.MaxResults].
// If the row budget is exhausted, the sorted partial results accompany
// [ErrQueryScanLimit].
func (tx *Tx) QueryCells(ctx context.Context, q CellQuery) ([]CellQueryResult, error) {
	results, _, err := tx.QueryCellsWithStats(ctx, q)
	return results, err
}

// QueryCellsWithStats runs [Tx.QueryCells] and reports the vector search path
// and whether widening stopped at its candidate budget.
func (tx *Tx) QueryCellsWithStats(ctx context.Context, q CellQuery) ([]CellQueryResult, CellQueryStats, error) {
	var stats CellQueryStats
	if err := tx.validateCellQuery(ctx, q); err != nil {
		return nil, stats, err
	}
	queryLow := strings.ToLower(strings.TrimSpace(q.Query))
	collector := queryCollector{order: q.SortBy, limit: q.MaxResults}
	var err error
	switch {
	case len(q.Embedding) == 0:
		err = tx.scanQueryCells(ctx, q, func(rec record.CellRecord) bool {
			if ctx.Err() != nil {
				return false
			}
			collector.add(q, rec, queryLow, 0)
			return true
		})
	case q.MaxScanRows > 0:
		err = tx.queryCellsByExactFilter(ctx, q, queryLow, &stats, &collector)
	default:
		collector.hits = []queryCandidate{}
		err = tx.queryCellsByEmbedding(ctx, q, queryLow, &stats, &collector)
	}
	if err != nil && !errors.Is(err, ErrQueryScanLimit) {
		return nil, stats, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, stats, ctxErr
	}
	return collector.results(q, queryLow), stats, err
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
	if len(q.Embedding) > 0 && q.MaxScanRows > 0 &&
		(q.EmbeddingCandidateLimit != 0 || (q.Radius == 0 && len(q.RequireTags) == 0 && strings.TrimSpace(q.SourceID) == "")) {
		return fmt.Errorf("%w: MaxScanRows with an embedding requires a required tag, source, or radius and no embedding candidate limit", ErrInvalidArgument)
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

func (tx *Tx) scanQueryCells(ctx context.Context, q CellQuery, yield func(record.CellRecord) bool) error {
	indexSourceID := strings.TrimSpace(q.SourceID)
	switch {
	case preferSpatialPlan(q):
		return tx.scanByRadiusFused(ctx, q.Center, q.Radius, q.MaxScanRows, yield)
	case len(q.RequireTags) > 0:
		return tx.scanByTagFused(ctx, q.RequireTags[0], q.MaxScanRows, yield)
	case indexSourceID != "":
		return tx.scanBySourceFused(ctx, indexSourceID, q.MaxScanRows, yield)
	case q.Radius > 0:
		return tx.scanByRadiusFused(ctx, q.Center, q.Radius, q.MaxScanRows, yield)
	default:
		return tx.scanAllCellsFused(ctx, q.MaxScanRows, yield)
	}
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
	readSeq := max(tx.readSeq, tx.writeSeq)
	var coord, pendingCoord lattice.PackedCoord
	var pendingValue []byte
	havePending := false
	scanned := 0
	var decodeErr error

	flush := func() bool {
		havePending = false
		if len(pendingValue) == 0 {
			return true // MVCC tombstone.
		}
		rec, err := record.DecodeCell(pendingValue)
		decodeErr = err
		return err == nil && yield(rec)
	}
	err := tx.ascendRangeSelected(from, to, func(key []byte) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if maxScanRows > 0 && scanned == maxScanRows {
			return false, ErrQueryScanLimit
		}
		scanned++
		parsed, seq, err := index.ParseCellVersionKey(key)
		coord = parsed
		return err == nil && seq <= readSeq, nil
	}, func(_, value []byte) bool {
		if havePending && coord != pendingCoord && !flush() {
			return false
		}
		// Physical versions ascend by commit sequence. The last visible
		// version replaces earlier ones, including when it is a tombstone.
		pendingCoord, pendingValue, havePending = coord, value, true
		return true
	})
	if decodeErr != nil {
		return decodeErr
	}
	if errors.Is(err, ErrQueryScanLimit) {
		// The budget can split a coordinate's versions. Preserve the latest
		// snapshot-visible result for the final coordinate admitted to the scan.
		if havePending {
			rec, found, getErr := tx.GetCell(pendingCoord)
			if getErr != nil {
				return getErr
			}
			if found {
				yield(rec)
			}
		}
		return ErrQueryScanLimit
	}
	if err != nil {
		return fmt.Errorf("hexxladb: QueryCells full scan: %w", err)
	}
	if havePending {
		flush()
	}
	return decodeErr
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

func (tx *Tx) queryCellsByEmbedding(ctx context.Context, q CellQuery, queryLow string, stats *CellQueryStats, collector *queryCollector) error {
	maxResults := q.MaxResults
	if maxResults <= 0 && q.EmbeddingCandidateLimit == 0 {
		return tx.queryCellsByFlatEmbedding(ctx, q, queryLow, stats, collector)
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
	for {
		hits, searchStats, err := tx.searchByEmbedding(ctx, q.Embedding, EmbeddingSearchConfig{MaxResults: fetchK})
		if err != nil {
			return err
		}
		stats.EmbeddingPath = searchStats.Path
		for _, hit := range hits {
			if err := ctx.Err(); err != nil {
				return err
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
				return err
			}
			if !ok {
				continue
			}
			collector.add(q, rec, queryLow, hit.Score)
		}
		stats.EmbeddingCandidates = len(seen)
		if (maxResults > 0 && len(collector.hits) >= maxResults) || len(hits) < fetchK {
			return nil
		}
		if len(seen) == candidateLimit || fetchK == candidateLimit {
			stats.EmbeddingCandidateLimitReached = true
			return nil
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

func (tx *Tx) queryCellsByFlatEmbedding(ctx context.Context, q CellQuery, queryLow string, stats *CellQueryStats, collector *queryCollector) error {
	similarity, err := tx.exactEmbeddingScorer(q.Embedding, stats)
	if err != nil {
		return err
	}
	if similarity == nil {
		return nil
	}
	err = tx.scanEmbeddings(ctx, func(coord lattice.PackedCoord, vec []float32) error {
		stats.EmbeddingCandidates++
		rec, ok, err := tx.GetCell(coord)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		collector.add(q, rec, queryLow, similarity(vec))
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// queryCellsByExactFilter scores only vectors reached by the bounded filter
// scan, bypassing the graph even when one is available.
func (tx *Tx) queryCellsByExactFilter(ctx context.Context, q CellQuery, queryLow string, stats *CellQueryStats, collector *queryCollector) error {
	similarity, err := tx.exactEmbeddingScorer(q.Embedding, stats)
	if err != nil || similarity == nil {
		return err
	}
	var lookupErr error
	err = tx.scanQueryCells(ctx, q, func(rec record.CellRecord) bool {
		vec, found, err := tx.GetEmbedding(rec.Key)
		if err != nil {
			lookupErr = err
			return false
		}
		if found {
			stats.EmbeddingCandidates++
			collector.add(q, rec, queryLow, similarity(vec))
		}
		return true
	})
	if lookupErr != nil {
		return lookupErr
	}
	return err
}

func (tx *Tx) exactEmbeddingScorer(query []float32, stats *CellQueryStats) (func([]float32) float64, error) {
	if err := validateEmbeddingVector(query); err != nil {
		return nil, err
	}
	dim := tx.db.eng.EmbeddingDim()
	if dim == 0 {
		stats.EmbeddingPath = EmbeddingSearchPathNone
		return nil, nil
	}
	if len(query) != int(dim) {
		return nil, fmt.Errorf("%w: want %d, got %d", ErrEmbeddingDimension, dim, len(query))
	}
	stats.EmbeddingPath = EmbeddingSearchPathFlat
	similarity := engine.SimilarityFunc(tx.db.eng.EmbeddingMetric())
	return func(vector []float32) float64 { return similarity(query, vector) }, nil
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
		tagsLow := distinctFoldedTags(rec.Tags)
		for tok := range strings.FieldsSeq(queryLow) {
			for _, tagLow := range tagsLow {
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
