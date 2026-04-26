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
	var candidates []record.CellRecord
	var err error

	switch {
	case len(q.RequireTags) > 0:
		candidates, err = tx.scanByTag(ctx, q.RequireTags[0])
	case q.SourceID != "":
		candidates, err = tx.scanBySource(ctx, q.SourceID)
	case !q.After.IsZero() || !q.Before.IsZero():
		candidates, err = tx.scanByTimeRange(ctx, q.After, q.Before)
	case q.Radius > 0:
		candidates, err = tx.scanByRadius(ctx, q.Center, q.Radius)
	default:
		candidates, err = tx.scanByRadius(ctx, Coord{}, defaultQueryScanRadius)
	}
	if err != nil {
		return nil, err
	}

	// ── filter + score ────────────────────────────────────────────────────────
	var results []CellQueryResult
	for _, rec := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		coord, err := lattice.Unpack(rec.Key)
		if err != nil {
			continue
		}
		c := Coord(coord)

		if !applyPredicates(q, rec, c, queryLow) {
			continue
		}

		score := scoreCell(queryLow, rec.Tags, rec.RawContent, rec.Provenance.SourceID, rec.Provenance.Confidence)

		// With a non-empty query, skip cells that scored no signal beyond the confidence bonus.
		if queryLow != "" && score <= 0.1*rec.Provenance.Confidence {
			continue
		}

		var explanation string
		if q.Explain {
			explanation = buildExplanation(q, rec, c, score, queryLow)
		}

		results = append(results, CellQueryResult{
			Cell: CellView{
				Coord:      c,
				RawContent: rec.RawContent,
				Tags:       rec.Tags,
				Provenance: rec.Provenance,
				Validity:   rec.Validity,
			},
			Score:       score,
			Explanation: explanation,
		})
	}

	// ── sort ──────────────────────────────────────────────────────────────────
	sortResults(results, q.SortBy)

	// ── limit ─────────────────────────────────────────────────────────────────
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// ── scanners ─────────────────────────────────────────────────────────────────

func (tx *Tx) scanByTag(ctx context.Context, tag string) ([]record.CellRecord, error) {
	var recs []record.CellRecord
	if err := tx.AscendCellsByTag(ctx, tag, func(r record.CellRecord) bool {
		recs = append(recs, r)
		return true
	}); err != nil {
		return nil, fmt.Errorf("hexxladb: QueryCells tag scan %q: %w", tag, err)
	}
	return recs, nil
}

func (tx *Tx) scanBySource(ctx context.Context, sourceID string) ([]record.CellRecord, error) {
	var recs []record.CellRecord
	if err := tx.AscendCellsBySource(ctx, sourceID, func(r record.CellRecord) bool {
		recs = append(recs, r)
		return true
	}); err != nil {
		return nil, fmt.Errorf("hexxladb: QueryCells source scan %q: %w", sourceID, err)
	}
	return recs, nil
}

func (tx *Tx) scanByTimeRange(ctx context.Context, after, before time.Time) ([]record.CellRecord, error) {
	// Compute week-bucket bounds for the AscendRange key span.
	// The time/ index keys are: "time/" + int64be(bucket) + "/" + packed_coord
	// We scan a single contiguous key range rather than iterating bucket-by-bucket.
	var bucketFrom, bucketTo int64
	if !after.IsZero() {
		// Start from the bucket containing 'after' (may hold entries just before it too —
		// fine-grained ValidFrom check below will filter those out).
		bucketFrom = after.UnixNano() / index.WeekNanos
	}
	if !before.IsZero() {
		bucketTo = before.UnixNano()/index.WeekNanos + 1 // inclusive upper bucket
	} else {
		bucketTo = 1<<62 - 1
	}

	from, _ := index.TimeRangePrefix(bucketFrom)
	_, to := index.TimeRangePrefix(bucketTo)

	seen := make(map[lattice.PackedCoord]struct{})
	var recs []record.CellRecord

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
		// Fine-grained ValidFrom check: bucket boundaries are weekly, exact bounds below.
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
		recs = append(recs, rec)
		return true
	}); err != nil {
		return nil, fmt.Errorf("hexxladb: QueryCells time scan: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return recs, nil
}

func (tx *Tx) scanByRadius(ctx context.Context, center Coord, radius int) ([]record.CellRecord, error) {
	coords := WalkRings(nil, center, radius)
	recs := make([]record.CellRecord, 0, len(coords))
	for _, c := range coords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, ok, err := tx.GetCell(mustPack(c))
		if err != nil {
			return nil, err
		}
		if ok {
			recs = append(recs, rec)
		}
	}
	return recs, nil
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

func buildExplanation(q CellQuery, rec record.CellRecord, c Coord, score float64, queryLow string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "coord=(%d,%d) score=%.2f conf=%.2f", c.Q, c.R, score, rec.Provenance.Confidence)
	if queryLow != "" {
		fmt.Fprintf(&sb, " query=%q", queryLow)
		for _, tag := range rec.Tags {
			tagLow := strings.ToLower(tag)
			if tagLow == queryLow {
				sb.WriteString(" [tag:exact]")
			} else if strings.HasPrefix(tagLow, queryLow) {
				sb.WriteString(" [tag:prefix]")
			}
		}
		if strings.Contains(rec.RawContent, q.Query) {
			sb.WriteString(" [content:verbatim]")
		} else if strings.Contains(strings.ToLower(rec.RawContent), queryLow) {
			sb.WriteString(" [content:icase]")
		}
		if rec.Provenance.SourceID == queryLow {
			sb.WriteString(" [source:exact]")
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
