package hexxladb

import (
	"context"
	"sort"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// TagCount holds a tag string and the number of visible cells carrying it.
type TagCount struct {
	Tag   string
	Count int
}

// TagCounts returns per-tag cell counts for all distinct tags, sorted by count descending.
// It uses the tag/ secondary index with MVCC-aware visibility filtering.
func (tx *Tx) TagCounts(ctx context.Context) ([]TagCount, error) {
	tags, err := tx.ListExistingTopics(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TagCount, 0, len(tags))
	for _, tag := range tags {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n := 0
		if err := tx.AscendCellsByTag(ctx, tag, func(_ record.CellRecord) bool {
			n++
			return true
		}); err != nil {
			return nil, err
		}
		out = append(out, TagCount{Tag: tag, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

// TagPair represents two tags that appear together on the same cell.
type TagPair struct {
	A, B  string
	Count int
}

// TagCooccurrences returns pairs of tags that appear together on visible cells,
// sorted by co-occurrence count descending. Only pairs with count ≥ minCount are returned.
func (tx *Tx) TagCooccurrences(ctx context.Context, minCount int) ([]TagPair, error) {
	counts := make(map[[2]string]int)
	processed := make(map[lattice.PackedCoord]struct{})
	tags, err := tx.ListExistingTopics(ctx)
	if err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := tx.AscendCellsByTag(ctx, tag, func(rec record.CellRecord) bool {
			if _, ok := processed[rec.Key]; ok {
				return true
			}
			processed[rec.Key] = struct{}{}
			sorted := record.UniqueSortedTags(rec.Tags)
			for i := range sorted {
				for j := i + 1; j < len(sorted); j++ {
					counts[[2]string{sorted[i], sorted[j]}]++
				}
			}
			return true
		}); err != nil {
			return nil, err
		}
	}
	var out []TagPair
	for pair, n := range counts {
		if n >= minCount {
			out = append(out, TagPair{A: pair[0], B: pair[1], Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out, nil
}

// UntaggedCells returns coordinates of visible cells that have no tags.
// It scans all cells within the given ring radius from center.
func (tx *Tx) UntaggedCells(ctx context.Context, center Coord, maxR int) ([]Coord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePackedRadius(center, maxR); err != nil {
		return nil, err
	}
	var out []Coord
	for cp := range lattice.WalkRingsPackedSeq(center, maxR) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, ok, err := tx.GetCell(cp.Packed)
		if err != nil {
			return nil, err
		}
		if ok && len(rec.Tags) == 0 {
			out = append(out, cp.Coord)
		}
	}
	return out, nil
}
