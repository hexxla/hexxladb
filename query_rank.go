package hexxladb

import (
	"cmp"
	"container/heap"
	"slices"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

type queryCandidate struct {
	record         record.CellRecord
	coord          Coord
	score          float64
	embeddingScore float64
	ordinal        int
}

// queryCollector keeps the worst retained candidate at the root when capped.
type queryCollector struct {
	hits  []queryCandidate
	order SortOrder
	limit int
	seen  int
}

func (c *queryCollector) add(q CellQuery, rec record.CellRecord, queryLow string, embeddingScore float64) {
	coord, err := lattice.Unpack(rec.Key)
	if err != nil || !applyPredicates(q, rec, coord) {
		return
	}
	score, matched := scoreCell(queryLow, rec.Tags, rec.RawContent, rec.Provenance.SourceID, rec.Provenance.Confidence)
	if !matched {
		return
	}
	candidate := queryCandidate{record: rec, coord: coord, score: score + embeddingScore, embeddingScore: embeddingScore, ordinal: c.seen}
	c.seen++
	switch {
	case c.limit <= 0:
		c.hits = append(c.hits, candidate)
	case len(c.hits) < c.limit:
		c.hits = append(c.hits, candidate)
		if len(c.hits) == c.limit {
			heap.Init(c)
		}
	case c.compare(&candidate, &c.hits[0]) < 0:
		c.hits[0] = candidate
		heap.Fix(c, 0)
	}
}

func (c *queryCollector) results(q CellQuery, queryLow string) []CellQueryResult {
	if c.hits == nil {
		return nil
	}
	slices.SortFunc(c.hits, func(a, b queryCandidate) int { return c.compare(&a, &b) })
	results := make([]CellQueryResult, 0, len(c.hits))
	for _, hit := range c.hits {
		rec := hit.record
		cell := CellView{
			Coord: hit.coord, RawContent: rec.RawContent, Tags: rec.Tags,
			Provenance: rec.Provenance, Validity: rec.Validity,
		}
		if rec.ClusterHint != nil {
			if hint, err := lattice.Unpack(*rec.ClusterHint); err == nil {
				cell.ClusterHint = new(hint)
			}
		}
		result := CellQueryResult{Cell: cell, Score: hit.score}
		if q.Explain {
			result.Explanation = buildExplanation(q, rec, hit.coord, hit.score, queryLow, hit.embeddingScore)
		}
		results = append(results, result)
	}
	return results
}

func (c *queryCollector) compare(a, b *queryCandidate) int {
	var order int
	switch c.order {
	case SortByConfidence:
		order = cmp.Compare(b.record.Provenance.Confidence, a.record.Provenance.Confidence)
	case SortByRecency:
		at, bt := a.record.Validity.ValidFrom, b.record.Validity.ValidFrom
		switch {
		case at == nil && bt != nil:
			order = 1
		case at != nil && bt == nil:
			order = -1
		case at != nil && bt != nil:
			order = cmp.Compare(*bt, *at)
		}
	case SortByCoord:
		order = cmp.Compare(a.coord.Q, b.coord.Q)
		if order == 0 {
			order = cmp.Compare(a.coord.R, b.coord.R)
		}
	default:
		order = cmp.Compare(b.score, a.score)
		if order == 0 {
			order = cmp.Compare(b.record.Provenance.Confidence, a.record.Provenance.Confidence)
		}
	}
	if order != 0 {
		return order
	}
	return cmp.Compare(a.ordinal, b.ordinal)
}

func (c *queryCollector) Len() int { return len(c.hits) }
func (c *queryCollector) Less(i, j int) bool {
	return c.compare(&c.hits[i], &c.hits[j]) > 0
}
func (c *queryCollector) Swap(i, j int) { c.hits[i], c.hits[j] = c.hits[j], c.hits[i] }
func (c *queryCollector) Push(x any)    { c.hits = append(c.hits, x.(queryCandidate)) }
func (c *queryCollector) Pop() any {
	last := len(c.hits) - 1
	hit := c.hits[last]
	c.hits[last] = queryCandidate{}
	c.hits = c.hits[:last]
	return hit
}
