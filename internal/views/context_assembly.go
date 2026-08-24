package views

import (
	"context"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// SeamTypeSupersedes is the canonical seam type written by MarkSupersedes.
// Defined here so context assembly can walk supersession chains without importing
// the root hexxladb package.
const SeamTypeSupersedes = "supersedes"

const maxSupersessionDepth = 16

// ContextAssemblyConfig configures context assembly for [Tx.LoadContext].
type ContextAssemblyConfig struct {
	// Assemble controls enrichment of each returned CellView.
	Assemble AssembleCellViewOpts
	// IncludeSeams attaches a deduplicated regional seam set to ContextPack.Seams.
	// It is distinct from Assemble.IncludeSeams, which enriches each CellView.
	IncludeSeams bool
	// SeamRadius is the regional seam search radius. Zero uses MaxRing;
	// negative values return ErrInvalidArgument.
	SeamRadius int
	// Explain populates ContextPack.Explanations when true.
	Explain bool
	// FilterSuperseded walks [SeamTypeSupersedes] chains for each candidate cell
	// and replaces superseded cells with their current-truth successor (if the
	// successor exists in the DB). Cells with no live successor are excluded.
	// Cycles are detected and broken after maxSupersessionDepth hops (default 16).
	FilterSuperseded bool
}

// contextCandidate pairs a CellView with the ring it was found in.
type contextCandidate struct {
	ring int
	view CellView
}

// resolveSupersession walks SeamTypeSupersedes chains from coord and returns
// the current-truth coord.
//   - Returns (coord, false, nil) when the cell is not superseded.
//   - Returns (successor, true, nil) when a live successor is found within maxSupersessionDepth.
//   - Returns (Coord{}, true, nil) when the cell is superseded but the chain
//     terminus has no live cell (caller should exclude the original).
func resolveSupersession(ctx context.Context, tx TxReader, coord lattice.Coord) (resolved lattice.Coord, wasSuperseded bool, err error) {
	visited := make(map[lattice.Coord]struct{})
	current := coord
	superseded := false
	for range maxSupersessionDepth {
		if err := ctx.Err(); err != nil {
			return coord, false, err
		}
		visited[current] = struct{}{}
		seams, err := tx.FindSeams(ctx, current, 0, false)
		if err != nil {
			return coord, false, err
		}
		next, cycle := findSupersessionTarget(seams, current, visited)
		if cycle {
			return lattice.Coord{}, true, nil
		}
		if next == nil {
			return resolveChainTerminus(tx, coord, current, superseded)
		}
		superseded = true
		current = *next
	}
	// Depth exceeded: treat as superseded with no live successor.
	return lattice.Coord{}, true, nil
}

// findSupersessionTarget scans seams for a SeamTypeSupersedes link from current.
// Returns (target, false) on success, (nil, false) when no link exists, or (nil, true) on cycle.
func findSupersessionTarget(seams []record.SeamRecord, current lattice.Coord, visited map[lattice.Coord]struct{}) (target *lattice.Coord, cycle bool) {
	for _, s := range seams {
		if s.SeamType != SeamTypeSupersedes {
			continue
		}
		cellA, errA := lattice.Unpack(s.CellA)
		if errA != nil || cellA != current {
			continue
		}
		cellB, errB := lattice.Unpack(s.CellB)
		if errB != nil {
			continue
		}
		if _, seen := visited[cellB]; seen {
			return nil, true
		}
		return &cellB, false
	}
	return nil, false
}

// resolveChainTerminus handles the end of a supersession chain: if never superseded
// returns the original coord unchanged; otherwise checks if the terminal cell exists.
func resolveChainTerminus(tx TxReader, orig, current lattice.Coord, superseded bool) (lattice.Coord, bool, error) {
	if !superseded {
		return orig, false, nil
	}
	pk, packErr := lattice.Pack(current)
	if packErr != nil {
		return lattice.Coord{}, true, nil
	}
	_, ok, getErr := tx.GetCell(pk)
	if getErr != nil || !ok {
		return lattice.Coord{}, true, nil
	}
	return current, true, nil
}

// collectCandidates scans rings outward from center, assembling up to capCells
// CellView candidates. If cfg.FilterSuperseded is set, superseded cells are
// replaced by their current-truth successor (or excluded if none exists).
func collectCandidates(ctx context.Context, tx TxReader, center lattice.Coord, maxR, maxCells int, asOf *time.Time, opts AssembleCellViewOpts, cfg ContextAssemblyConfig) ([]contextCandidate, []CellExplanation, int, error) {
	// Keep speculative capacity bounded even when a caller supplies a very large
	// result limit; slices grow only as actual cells are found.
	initialCapacity := min(maxCells, 256)
	coll := newCandidateCollector(initialCapacity, asOf, opts, cfg)
	ringBuf := make([]lattice.Coord, 0, min(maxCells, 6*min(maxR, 64)+1))
	for ring := range maxR + 1 {
		ringBuf = lattice.RingInto(ringBuf[:0], center, ring)
		for _, c := range ringBuf {
			if len(coll.items) >= maxCells {
				return coll.items, coll.explanations, coll.scanned, nil
			}
			coll.scanned++
			if err := coll.processCoord(ctx, tx, c, ring); err != nil {
				return nil, nil, coll.scanned, err
			}
		}
	}
	return coll.items, coll.explanations, coll.scanned, nil
}

// candidateCollector accumulates unique assembled candidates.
type candidateCollector struct {
	items        []contextCandidate
	explanations []CellExplanation
	seen         map[lattice.Coord]struct{}
	cfg          ContextAssemblyConfig
	opts         AssembleCellViewOpts
	asOf         *time.Time
	scanned      int
}

func newCandidateCollector(capacity int, asOf *time.Time, opts AssembleCellViewOpts, cfg ContextAssemblyConfig) candidateCollector {
	return candidateCollector{
		items: make([]contextCandidate, 0, capacity),
		seen:  make(map[lattice.Coord]struct{}, capacity),
		cfg:   cfg,
		opts:  opts,
		asOf:  asOf,
	}
}

// processCoord resolves supersession, assembles the cell view, and adds it.
func (cc *candidateCollector) processCoord(ctx context.Context, tx TxReader, c lattice.Coord, ring int) error {
	target, originalCoord, skip, err := cc.resolveTarget(ctx, tx, c, ring)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}
	if _, already := cc.seen[target]; already {
		return nil
	}
	v, err := AssembleCellView(ctx, tx, target, cc.asOf, cc.opts)
	if err != nil {
		if errors.Is(err, ErrCellNotFound) {
			return nil
		}
		return err
	}
	if originalCoord != nil {
		v.SupersededFrom = originalCoord
		if cc.cfg.Explain {
			cc.explanations = append(cc.explanations, CellExplanation{
				Coord:        *originalCoord,
				Ring:         ring,
				Reason:       "superseded",
				SupersededBy: &target,
			})
		}
	}
	cc.seen[target] = struct{}{}
	cc.items = append(cc.items, contextCandidate{ring: ring, view: v})
	return nil
}

// resolveTarget determines the effective target coord for a candidate, handling
// supersession. Returns the target, optional original coord pointer, whether to
// skip this candidate, and any error.
func (cc *candidateCollector) resolveTarget(ctx context.Context, tx TxReader, c lattice.Coord, ring int) (target lattice.Coord, original *lattice.Coord, skip bool, err error) {
	if !cc.cfg.FilterSuperseded {
		return c, nil, false, nil
	}
	resolved, wasSuperseded, err := resolveSupersession(ctx, tx, c)
	if err != nil {
		return lattice.Coord{}, nil, false, err
	}
	if !wasSuperseded {
		return c, nil, false, nil
	}
	if resolved == (lattice.Coord{}) {
		if cc.cfg.Explain {
			cc.explanations = append(cc.explanations, CellExplanation{
				Coord:  c,
				Ring:   ring,
				Reason: "superseded",
			})
		}
		return lattice.Coord{}, nil, true, nil
	}
	origC := c
	return resolved, &origC, false, nil
}

// loadContextByRings walks rings from center and returns at most maxCells
// candidates in deterministic nearest-first order. Model-specific ranking,
// prompt rendering, and token accounting belong to the embedding application.
func loadContextByRings(ctx context.Context, tx TxReader, center lattice.Coord, maxR, maxCells int, asOf *time.Time, cfg ContextAssemblyConfig) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if maxCells <= 0 || maxR < 0 {
		return ContextPack{}, ErrInvalidArgument
	}
	assembleOpts := cfg.Assemble
	if assembleOpts == (AssembleCellViewOpts{}) {
		assembleOpts = DefaultAssembleCellViewOpts()
	}

	items, explanations, scanned, err := collectCandidates(ctx, tx, center, maxR, maxCells, asOf, assembleOpts, cfg)
	if err != nil {
		return ContextPack{}, err
	}
	if len(items) == 0 {
		return ContextPack{}, nil
	}

	pack := buildContextPack(items, explanations, scanned, maxCells, cfg)

	if cfg.IncludeSeams {
		if err := attachSeams(ctx, tx, []lattice.Coord{center}, maxR, cfg, &pack); err != nil {
			return ContextPack{}, err
		}
	}
	return pack, nil
}

// assembleCoordsIntoContextPack assembles an ordered coordinate list through
// the same validity, supersession, deduplication, and result-limit path used by
// ring retrieval.
func assembleCoordsIntoContextPack(ctx context.Context, tx TxReader, coords, seeds []lattice.Coord, maxCells, defaultSeamRadius int, asOf *time.Time, cfg ContextAssemblyConfig) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if maxCells <= 0 {
		return ContextPack{}, ErrInvalidArgument
	}
	opts := cfg.Assemble
	if opts == (AssembleCellViewOpts{}) {
		opts = DefaultAssembleCellViewOpts()
	}
	coll := newCandidateCollector(min(len(coords), maxCells), asOf, opts, cfg)
	for _, coord := range coords {
		if len(coll.items) >= maxCells {
			break
		}
		coll.scanned++
		if err := coll.processCoord(ctx, tx, coord, nearestSeedDistance(coord, seeds)); err != nil {
			return ContextPack{}, err
		}
	}
	pack := buildContextPack(coll.items, coll.explanations, coll.scanned, maxCells, cfg)
	if cfg.IncludeSeams {
		if err := attachSeams(ctx, tx, seeds, defaultSeamRadius, cfg, &pack); err != nil {
			return ContextPack{}, err
		}
	}
	return pack, nil
}

func nearestSeedDistance(coord lattice.Coord, seeds []lattice.Coord) int {
	if len(seeds) == 0 {
		return 0
	}
	distance := coord.Distance(seeds[0])
	for _, seed := range seeds[1:] {
		distance = min(distance, coord.Distance(seed))
	}
	return distance
}

// buildContextPack assembles the final ContextPack from selected items.
func buildContextPack(items []contextCandidate, explanations []CellExplanation, candidatesScanned, maxCells int, cfg ContextAssemblyConfig) ContextPack {
	maxRingUsed := 0
	for i := range items {
		if items[i].ring > maxRingUsed {
			maxRingUsed = items[i].ring
		}
	}
	if cfg.Explain {
		for i := range items {
			explanations = append(explanations, CellExplanation{
				Coord:  items[i].view.Coord,
				Ring:   items[i].ring,
				Reason: "included",
			})
		}
	}

	out := make([]CellView, len(items))
	for i := range items {
		out[i] = items[i].view
	}
	return ContextPack{
		Cells: out,
		Stats: ContextPackStats{
			CandidatesScanned:  candidatesScanned,
			ResultLimitReached: len(out) == maxCells,
			MaxRingUsed:        maxRingUsed,
		},
		Explanations: explanations,
	}
}

// attachSeams loads seams for the context pack.
func attachSeams(ctx context.Context, tx TxReader, centers []lattice.Coord, maxR int, cfg ContextAssemblyConfig, pack *ContextPack) error {
	r := cfg.SeamRadius
	if r < 0 {
		return ErrInvalidArgument
	}
	if r == 0 {
		r = maxR
	}
	seen := make(map[string]struct{})
	for _, center := range centers {
		seams, err := tx.FindSeams(ctx, center, r, false)
		if err != nil {
			return err
		}
		for _, seam := range seams {
			if _, duplicate := seen[seam.ID]; duplicate {
				continue
			}
			seen[seam.ID] = struct{}{}
			pack.Seams = append(pack.Seams, seam)
		}
	}
	return nil
}
