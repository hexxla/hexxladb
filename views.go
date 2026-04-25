package hexxladb

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// FacetView is a read-oriented facet projection aligned with docs/hexxladb/HEXXLA.md (FacetView).
type FacetView struct {
	ID             byte
	DerivedContent string
	LastRotated    time.Time
	DerivationHash string // hex-encoded SHA-256 from wire record
}

// EdgeView summarizes one directed edge from a cell.
type EdgeView struct {
	To           Coord
	RelationType string
	Weight       float64
}

// SeamRef is a lightweight seam handle for embedding in [CellView].
type SeamRef struct {
	ID               string
	SeamType         string
	ResolutionStatus string
}

// CellView aggregates decoded cell content with optional facets, edges, and seam refs.
type CellView struct {
	Coord       Coord
	RawContent  string
	Provenance  record.ProvenanceWire
	Validity    record.ValidityWire
	Tags        []string
	ClusterHint *Coord
	Facets      []FacetView
	ActiveFacet int
	Edges       []EdgeView
	Seams       []SeamRef
}

// ContextPackStats records assembly statistics for debugging and observability.
type ContextPackStats struct {
	CandidatesScanned int // cells examined before eviction
	CellsEvicted      int // cells dropped during budget trimming
	MaxRingUsed       int // outermost ring that contributed at least one cell
}

// CellExplanation records why a cell was included or excluded during context assembly.
type CellExplanation struct {
	Coord  Coord
	Ring   int
	Reason string // "included", "evicted_low_confidence", "cap_exceeded"
	Tokens int    // token count at time of decision
}

// ContextPack matches the neighborhood summary shape described in HEXXLA.md (Cells + TotalTokens + Seams).
type ContextPack struct {
	Cells        []CellView
	TotalTokens  int
	Seams        []record.SeamRecord
	Stats        ContextPackStats  // assembly statistics (zero when built outside LoadContextWithBudgeting)
	Explanations []CellExplanation // per-cell decisions (only when LoadContextBudgetConfig.Explain is true)
}

// TokenBudgeter counts tokens for budgeting (e.g. approximate LLM tokens); inject domain-specific logic.
type TokenBudgeter interface {
	CountTokens(content string) int
}

// ByteLenBudgeter counts tokens as UTF-8 byte length (cheap default; not tokenizer-accurate).
type ByteLenBudgeter struct{}

// CountTokens implements [TokenBudgeter].
func (ByteLenBudgeter) CountTokens(content string) int { return len(content) }

// AssembleCellViewOpts configures [Tx.AssembleCellView].
type AssembleCellViewOpts struct {
	IncludeFacets       bool
	IncludeEdges        bool
	IncludeSeams        bool
	SeamSearchRadius    int
	UnresolvedSeamsOnly bool
}

// DefaultAssembleCellViewOpts returns defaults: facets on; edges and seams off.
func DefaultAssembleCellViewOpts() AssembleCellViewOpts {
	return AssembleCellViewOpts{
		IncludeFacets:    true,
		SeamSearchRadius: 1,
	}
}

// AssembleCellView builds a [CellView] for coord using the transaction snapshot.
func (tx *Tx) AssembleCellView(ctx context.Context, coord Coord, asOf *time.Time, opts AssembleCellViewOpts) (CellView, error) {
	if err := ctx.Err(); err != nil {
		return CellView{}, err
	}
	if tx == nil || tx.db == nil {
		return CellView{}, ErrClosed
	}
	packed, err := lattice.Pack(coord)
	if err != nil {
		return CellView{}, err
	}
	rec, ok, err := tx.GetCell(packed)
	if err != nil {
		return CellView{}, err
	}
	if !ok {
		return CellView{}, ErrCellNotFound
	}
	if asOf != nil {
		if !record.ValidAt(rec.Validity, asOf.UTC().UnixNano()) {
			return CellView{}, ErrCellNotFound
		}
	}
	out := CellView{
		Coord:       coord,
		RawContent:  rec.RawContent,
		Provenance:  rec.Provenance,
		Validity:    rec.Validity,
		Tags:        append([]string(nil), rec.Tags...),
		ClusterHint: nil,
		ActiveFacet: 0,
		Facets:      nil,
		Edges:       nil,
		Seams:       nil,
	}
	if rec.ClusterHint != nil {
		ch, err := lattice.Unpack(*rec.ClusterHint)
		if err == nil {
			out.ClusterHint = &ch
		}
	}
	if opts.IncludeFacets {
		err := tx.AscendFacetsForCell(packed, func(fr record.FacetRecord) bool {
			out.Facets = append(out.Facets, facetViewFromRecord(fr))
			return true
		})
		if err != nil {
			return CellView{}, err
		}
	}
	if opts.IncludeEdges {
		err := tx.AscendEdgesFrom(packed, func(er record.EdgeRecord) bool {
			to, err := lattice.Unpack(er.To)
			if err != nil {
				return true
			}
			out.Edges = append(out.Edges, EdgeView{To: to, RelationType: er.RelationType, Weight: er.Weight})
			return true
		})
		if err != nil {
			return CellView{}, err
		}
	}
	if opts.IncludeSeams {
		radius := opts.SeamSearchRadius
		if radius < 0 {
			return CellView{}, ErrInvalidArgument
		}
		seams, err := tx.FindSeams(ctx, coord, radius, opts.UnresolvedSeamsOnly)
		if err != nil {
			return CellView{}, err
		}
		for _, s := range seams {
			a, err := lattice.Unpack(s.CellA)
			if err != nil {
				continue
			}
			b, err := lattice.Unpack(s.CellB)
			if err != nil {
				continue
			}
			if a != coord && b != coord {
				continue
			}
			out.Seams = append(out.Seams, SeamRef{ID: s.ID, SeamType: s.SeamType, ResolutionStatus: s.ResolutionStatus})
		}
	}
	return out, nil
}

func facetViewFromRecord(fr record.FacetRecord) FacetView {
	return FacetView{
		ID:             fr.FacetID,
		DerivedContent: fr.DerivedContent,
		LastRotated:    time.Unix(0, fr.LastRotated).UTC(),
		DerivationHash: hex.EncodeToString(fr.DerivationHash[:]),
	}
}

// LoadContextBudgetConfig configures [Tx.LoadContextWithBudgeting].
type LoadContextBudgetConfig struct {
	Assemble          AssembleCellViewOpts
	MaxCandidateCells int // upper bound on cells considered before eviction; default 256
	IncludeFacetText  bool
	IncludeSeams      bool
	SeamRadius        int
	Explain           bool // when true, populate ContextPack.Explanations
	// FilterSuperseded walks [SeamTypeSupersedes] chains for each candidate cell and
	// replaces superseded cells with their current-truth successor (if the successor
	// exists in the DB). Cells with no live successor are excluded from the pack.
	// Cycles are detected and broken after maxSupersessionDepth hops (default 16).
	FilterSuperseded bool
}

// scoredCandidate pairs a CellView with the ring it was found in, used during context budgeting eviction.
type scoredCandidate struct {
	ring int
	view CellView
}

const maxSupersessionDepth = 16

// resolveSupersession walks SeamTypeSupersedes chains from coord and returns the
// current-truth coord. Returns (coord, false) if the cell is not superseded.
// Returns (successor, true) if a live successor is found within maxSupersessionDepth hops.
// Returns (Coord{}, true) if the cell is superseded but the chain terminus has no live cell
// (caller should exclude the original cell). Cycles are detected and broken.
func (tx *Tx) resolveSupersession(ctx context.Context, coord Coord) (resolved Coord, wasSuperseded bool, err error) {
	visited := make(map[Coord]struct{})
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
		var next *Coord
		for _, s := range seams {
			if s.SeamType != SeamTypeSupersedes {
				continue
			}
			cellA, errA := lattice.Unpack(s.CellA)
			if errA != nil {
				continue
			}
			if cellA != current {
				continue
			}
			cellB, errB := lattice.Unpack(s.CellB)
			if errB != nil {
				continue
			}
			if _, seen := visited[cellB]; seen {
				// Cycle: treat original as superseded with no live successor.
				return Coord{}, true, nil
			}
			next = &cellB
			superseded = true
			break
		}
		if next == nil {
			if !superseded {
				return coord, false, nil
			}
			// Chain terminus: check if current has a live cell.
			pk, packErr := lattice.Pack(current)
			if packErr != nil {
				return Coord{}, true, nil
			}
			_, ok, getErr := tx.GetCell(pk)
			if getErr != nil || !ok {
				return Coord{}, true, nil
			}
			return current, true, nil
		}
		current = *next
	}
	// Depth exceeded: treat as superseded with no live successor.
	return Coord{}, true, nil
}

// collectCandidates scans rings outward from center, assembling up to capCells CellView candidates.
// If cfg.FilterSuperseded is set, superseded cells are replaced by their current-truth successor
// (or excluded if no live successor exists).
func (tx *Tx) collectCandidates(ctx context.Context, center Coord, maxR, capCells int, opts AssembleCellViewOpts, cfg LoadContextBudgetConfig) ([]scoredCandidate, error) {
	var items []scoredCandidate
	seen := make(map[Coord]struct{})
	for ring := 0; ring <= maxR; ring++ {
		for _, c := range lattice.Ring(center, ring) {
			if len(items) >= capCells {
				return items, nil
			}
			target := c
			if cfg.FilterSuperseded {
				resolved, wasSuperseded, err := tx.resolveSupersession(ctx, c)
				if err != nil {
					return nil, err
				}
				if wasSuperseded {
					if resolved == (Coord{}) {
						// Superseded with no live successor: exclude entirely.
						continue
					}
					target = resolved
				}
			}
			if _, already := seen[target]; already {
				continue
			}
			v, err := tx.AssembleCellView(ctx, target, nil, opts)
			if err != nil {
				if errors.Is(err, ErrCellNotFound) {
					continue
				}
				return nil, err
			}
			seen[target] = struct{}{}
			items = append(items, scoredCandidate{ring: ring, view: v})
		}
	}
	return items, nil
}

// LoadContextWithBudgeting walks rings like [Tx.LoadContext], builds [CellView] values, then applies
// HEXXLA.md-style eviction: drop lowest [record.ProvenanceWire.Confidence] from the outermost ring first
// until within maxTokens (or no progress). Token counts sum RawContent and optionally facet text.
func (tx *Tx) LoadContextWithBudgeting(ctx context.Context, center Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	if err := ctx.Err(); err != nil {
		return ContextPack{}, err
	}
	if tx == nil || tx.db == nil {
		return ContextPack{}, ErrClosed
	}
	if maxTokens <= 0 || maxR < 0 {
		return ContextPack{}, ErrInvalidArgument
	}
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	capCells := cfg.MaxCandidateCells
	if capCells <= 0 {
		capCells = 256
	}
	assembleOpts := cfg.Assemble
	if assembleOpts == (AssembleCellViewOpts{}) {
		assembleOpts = DefaultAssembleCellViewOpts()
	}
	items, err := tx.collectCandidates(ctx, center, maxR, capCells, assembleOpts, cfg)
	if err != nil {
		return ContextPack{}, err
	}
	if len(items) == 0 {
		return ContextPack{}, nil
	}
	candidatesScanned := len(items)
	evicted := 0
	var explanations []CellExplanation
	total := 0
	for i := range items {
		total += cellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText)
	}
	for total > maxTokens && len(items) > 0 {
		maxRing := -1
		for i := range items {
			if items[i].ring > maxRing {
				maxRing = items[i].ring
			}
		}
		drop := -1
		var minConf float64
		for i := range items {
			if items[i].ring != maxRing {
				continue
			}
			c := items[i].view.Provenance.Confidence
			if drop < 0 || c < minConf {
				drop = i
				minConf = c
			}
		}
		if drop < 0 {
			break
		}
		if cfg.Explain {
			explanations = append(explanations, CellExplanation{
				Coord:  items[drop].view.Coord,
				Ring:   items[drop].ring,
				Reason: "evicted_low_confidence",
				Tokens: cellViewTokens(budgeter, items[drop].view, cfg.IncludeFacetText),
			})
		}
		items = append(items[:drop], items[drop+1:]...)
		evicted++
		total = 0
		for i := range items {
			total += cellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText)
		}
	}
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
				Tokens: cellViewTokens(budgeter, items[i].view, cfg.IncludeFacetText),
			})
		}
	}
	out := make([]CellView, len(items))
	for i := range items {
		out[i] = items[i].view
	}
	pack := ContextPack{
		Cells: out,
		Stats: ContextPackStats{
			CandidatesScanned: candidatesScanned,
			CellsEvicted:      evicted,
			MaxRingUsed:       maxRingUsed,
		},
		Explanations: explanations,
	}
	for i := range out {
		pack.TotalTokens += cellViewTokens(budgeter, out[i], cfg.IncludeFacetText)
	}
	if cfg.IncludeSeams {
		r := cfg.SeamRadius
		if r < 0 {
			return ContextPack{}, ErrInvalidArgument
		}
		if r == 0 {
			r = maxR
		}
		seams, err := tx.FindSeams(ctx, center, r, false)
		if err != nil {
			return ContextPack{}, err
		}
		pack.Seams = seams
	}
	return pack, nil
}

// LoadContextPack matches HEXXLA.md naming for token-capped neighborhoods; it forwards to [Tx.LoadContextWithBudgeting].
func (tx *Tx) LoadContextPack(ctx context.Context, center Coord, maxR, maxTokens int, budgeter TokenBudgeter, cfg LoadContextBudgetConfig) (ContextPack, error) {
	return tx.LoadContextWithBudgeting(ctx, center, maxR, maxTokens, budgeter, cfg)
}

// CellViewPredicate selects [CellView] rows when filtering slices produced from walks or budgeting helpers.
type CellViewPredicate func(CellView) bool

// FilterCellViews returns only views for which pred reports true. If pred is nil, returns a copy of views.
func FilterCellViews(views []CellView, pred CellViewPredicate) []CellView {
	if pred == nil {
		out := make([]CellView, len(views))
		copy(out, views)
		return out
	}
	var out []CellView
	for _, v := range views {
		if pred(v) {
			out = append(out, v)
		}
	}
	return out
}

// TruncateCellViewsToTokenBudget returns a prefix of views (order preserved) such that the summed token count
// for each kept cell is at most maxTokens (same counting rules as [Tx.LoadContextWithBudgeting] via cellViewTokens).
func TruncateCellViewsToTokenBudget(views []CellView, budgeter TokenBudgeter, maxTokens int, includeFacetText bool) (out []CellView, total int) {
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	if maxTokens <= 0 || len(views) == 0 {
		return nil, 0
	}
	for _, v := range views {
		n := cellViewTokens(budgeter, v, includeFacetText)
		if total+n > maxTokens {
			break
		}
		out = append(out, v)
		total += n
	}
	return out, total
}

func cellViewTokens(b TokenBudgeter, v CellView, facets bool) int {
	n := b.CountTokens(v.RawContent)
	if facets {
		for _, f := range v.Facets {
			n += b.CountTokens(f.DerivedContent)
		}
	}
	return n
}
