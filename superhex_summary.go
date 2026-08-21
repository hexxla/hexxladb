package hexxladb

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/hexxla/hexxladb/internal/index"
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// SuperHexSummary is a materialized occupancy summary for one aperture-7
// super-hex. Parent identifies the cell at Level; Center is its corresponding
// level-0 coordinate. Capacity is 7^Level.
type SuperHexSummary struct {
	Level          int
	Parent         Coord
	Center         Coord
	OccupiedCells  int
	Capacity       int
	LastUpdatedSeq uint64
}

// SuperHexSummaryIndex is a rebuildable, in-memory derived index over cells.
// It reads existing cells once in [SuperHexSummaryIndex.Rebuild], then consumes
// the database changelog incrementally through [SuperHexSummaryIndex.Sync]. It
// does not alter PackedCoord or the database file format.
//
// A zero value is not usable; construct an index with [NewSuperHexSummaryIndex].
type SuperHexSummaryIndex struct {
	mu         sync.RWMutex
	level      int
	capacity   int
	source     *DB
	lastSeq    uint64
	cellParent map[PackedCoord]Coord
	summaries  map[Coord]SuperHexSummary
}

// NewSuperHexSummaryIndex constructs an aperture-7 summary index at level >= 1.
// It returns [ErrInvalidArgument] if 7^level cannot be represented by int.
func NewSuperHexSummaryIndex(level int) (*SuperHexSummaryIndex, error) {
	if level < 1 {
		return nil, fmt.Errorf("%w: super-hex level must be at least 1", ErrInvalidArgument)
	}
	capacity := 1
	maxInt := int(^uint(0) >> 1)
	for range level {
		if capacity > maxInt/7 {
			return nil, fmt.Errorf("%w: super-hex level is too large", ErrInvalidArgument)
		}
		capacity *= 7
	}
	return &SuperHexSummaryIndex{
		level:      level,
		capacity:   capacity,
		cellParent: make(map[PackedCoord]Coord),
		summaries:  make(map[Coord]SuperHexSummary),
	}, nil
}

// Rebuild replaces the derived index with a consistent snapshot of db and pins
// its changelog cursor to that snapshot. Changelog must be enabled so later
// calls to [SuperHexSummaryIndex.Sync] cannot miss committed changes.
func (s *SuperHexSummaryIndex) Rebuild(ctx context.Context, db *DB) error {
	if s == nil {
		return fmt.Errorf("%w: nil super-hex summary index", ErrInvalidArgument)
	}
	if s.level < 1 || s.capacity < 7 {
		return fmt.Errorf("%w: construct the super-hex index with NewSuperHexSummaryIndex", ErrInvalidArgument)
	}
	if db == nil {
		return ErrDatabaseClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cells := make(map[PackedCoord]Coord)
	summaries := make(map[Coord]SuperHexSummary)
	var cursor uint64
	var scanErr error
	err := db.View(func(tx *Tx) error {
		if db.changelog == nil {
			return ErrChangelogDisabled
		}
		if err := tx.scanAllCellsFused(ctx, 0, func(rec record.CellRecord) bool {
			coord, unpackErr := lattice.Unpack(rec.Key)
			if unpackErr != nil {
				scanErr = fmt.Errorf("%w: super-hex cell coordinate: %w", ErrCorruptDatabase, unpackErr)
				return false
			}
			parent := lattice.SuperHexParentAtLevel(coord, s.level)
			cells[rec.Key] = parent
			summary := summaries[parent]
			if summary.Capacity == 0 {
				summary = s.emptySummary(parent, 0)
			}
			summary.OccupiedCells++
			summaries[parent] = summary
			return true
		}); err != nil {
			return err
		}
		if scanErr != nil {
			return scanErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		cursor = db.changelog.MaxSeq()
		return nil
	})
	if err != nil {
		return err
	}
	for parent, summary := range summaries {
		summary.LastUpdatedSeq = cursor
		summaries[parent] = summary
	}
	s.source = db
	s.lastSeq = cursor
	s.cellParent = cells
	s.summaries = summaries
	return nil
}

// Sync applies up to limit changelog records after the last processed sequence.
// Non-cell records advance the cursor but do not affect summaries. A non-positive
// limit uses 256. Call until processed is zero to catch up completely.
func (s *SuperHexSummaryIndex) Sync(ctx context.Context, db *DB, limit int) (processed int, err error) {
	if s == nil {
		return 0, fmt.Errorf("%w: nil super-hex summary index", ErrInvalidArgument)
	}
	if db == nil {
		return 0, ErrDatabaseClosed
	}
	if s.level < 1 || s.capacity < 7 {
		return 0, fmt.Errorf("%w: construct the super-hex index with NewSuperHexSummaryIndex", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 256
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source == nil || s.source != db {
		return 0, fmt.Errorf("%w: rebuild the super-hex index from this database before syncing", ErrInvalidArgument)
	}
	records, err := db.ReadChangelogSince(s.lastSeq, limit)
	if err != nil {
		return 0, err
	}
	for i := range records {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		rec := &records[i]
		switch rec.Op {
		case ChangelogOpPutCell, ChangelogOpDeleteCell:
			packed, parseErr := index.ParseCellKey(rec.Key)
			if parseErr != nil {
				return processed, fmt.Errorf("%w: super-hex changelog cell key: %w", ErrCorruptDatabase, parseErr)
			}
			if _, unpackErr := lattice.Unpack(packed); unpackErr != nil {
				return processed, fmt.Errorf("%w: super-hex changelog coordinate: %w", ErrCorruptDatabase, unpackErr)
			}
			if rec.Op == ChangelogOpPutCell {
				s.applyPut(packed, rec.Seq)
			} else {
				s.applyDelete(packed, rec.Seq)
			}
		}
		s.lastSeq = rec.Seq
		processed++
	}
	return processed, nil
}

// Summary returns the materialized summary for parent.
func (s *SuperHexSummaryIndex) Summary(parent Coord) (SuperHexSummary, bool) {
	if s == nil {
		return SuperHexSummary{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	summary, ok := s.summaries[parent]
	return summary, ok
}

// SummaryForCoord maps a level-0 coordinate to this index's hierarchy level
// and returns its materialized summary.
func (s *SuperHexSummaryIndex) SummaryForCoord(coord Coord) (SuperHexSummary, bool, error) {
	if s == nil || s.level < 1 {
		return SuperHexSummary{}, false, fmt.Errorf("%w: uninitialized super-hex summary index", ErrInvalidArgument)
	}
	if _, err := lattice.Pack(coord); err != nil {
		return SuperHexSummary{}, false, fmt.Errorf("%w: super-hex coordinate: %w", ErrInvalidArgument, err)
	}
	parent := lattice.SuperHexParentAtLevel(coord, s.level)
	summary, ok := s.Summary(parent)
	return summary, ok, nil
}

// Summaries returns all non-empty materialized summaries in parent-coordinate
// order, making exports and tests deterministic.
func (s *SuperHexSummaryIndex) Summaries() []SuperHexSummary {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SuperHexSummary, 0, len(s.summaries))
	for _, summary := range s.summaries {
		out = append(out, summary)
	}
	slices.SortFunc(out, func(a, b SuperHexSummary) int {
		switch {
		case a.Parent.Q < b.Parent.Q:
			return -1
		case a.Parent.Q > b.Parent.Q:
			return 1
		case a.Parent.R < b.Parent.R:
			return -1
		case a.Parent.R > b.Parent.R:
			return 1
		default:
			return 0
		}
	})
	return out
}

// LastSeq returns the highest changelog sequence incorporated by the index.
func (s *SuperHexSummaryIndex) LastSeq() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastSeq
}

func (s *SuperHexSummaryIndex) applyPut(packed PackedCoord, seq uint64) {
	if parent, exists := s.cellParent[packed]; exists {
		summary := s.summaries[parent]
		summary.LastUpdatedSeq = seq
		s.summaries[parent] = summary
		return
	}
	coord, err := lattice.Unpack(packed)
	if err != nil {
		return
	}
	parent := lattice.SuperHexParentAtLevel(coord, s.level)
	s.cellParent[packed] = parent
	summary := s.summaries[parent]
	if summary.Capacity == 0 {
		summary = s.emptySummary(parent, seq)
	}
	summary.OccupiedCells++
	summary.LastUpdatedSeq = seq
	s.summaries[parent] = summary
}

func (s *SuperHexSummaryIndex) applyDelete(packed PackedCoord, seq uint64) {
	parent, exists := s.cellParent[packed]
	if !exists {
		return
	}
	delete(s.cellParent, packed)
	summary := s.summaries[parent]
	summary.OccupiedCells--
	if summary.OccupiedCells == 0 {
		delete(s.summaries, parent)
		return
	}
	summary.LastUpdatedSeq = seq
	s.summaries[parent] = summary
}

func (s *SuperHexSummaryIndex) emptySummary(parent Coord, seq uint64) SuperHexSummary {
	return SuperHexSummary{
		Level:          s.level,
		Parent:         parent,
		Center:         lattice.SuperHexCenterAtLevel(parent, s.level),
		Capacity:       s.capacity,
		LastUpdatedSeq: seq,
	}
}
