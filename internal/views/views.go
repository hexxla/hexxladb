// Package views provides read-oriented view types and cell-neighbourhood
// assembly logic for HexxlaDB.
//
// All functions in this package accept [TxReader] rather than a concrete *Tx,
// keeping the package free of any import from the root hexxladb package and
// avoiding circular dependencies.
package views

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

// ── View types ────────────────────────────────────────────────────────────────

// FacetView is a read-oriented facet projection aligned with docs/hexxladb/HEXXLA.md.
type FacetView struct {
	ID             byte
	DerivedContent string
	LastRotated    time.Time
	DerivationHash string // hex-encoded SHA-256 from wire record
}

// EdgeView summarises one directed edge from a cell.
type EdgeView struct {
	To           lattice.Coord
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
	Coord       lattice.Coord
	RawContent  string
	Provenance  record.ProvenanceWire
	Validity    record.ValidityWire
	Tags        []string
	ClusterHint *lattice.Coord
	Facets      []FacetView
	ActiveFacet int
	Edges       []EdgeView
	Seams       []SeamRef
	// SupersededFrom is set when this cell was substituted for a superseded cell
	// during context assembly with [LoadContextBudgetConfig.FilterSuperseded].
	// It holds the coordinate of the original (stale) cell that was replaced.
	SupersededFrom *lattice.Coord
}

// ContextPackStats records assembly statistics for debugging and observability.
type ContextPackStats struct {
	CandidatesScanned int // cells examined before eviction
	CellsEvicted      int // cells dropped during budget trimming
	MaxRingUsed       int // outermost ring that contributed at least one cell
}

// CellExplanation records why a cell was included or excluded during context assembly.
type CellExplanation struct {
	Coord lattice.Coord
	Ring  int
	// Reason is one of: "included", "evicted_low_confidence", "cap_exceeded", "superseded".
	Reason string
	Tokens int // token count at time of decision
	// SupersededBy is set when Reason == "superseded": the coord of the successor
	// cell that replaced this one (if a live successor exists), or nil if excluded.
	SupersededBy *lattice.Coord
}

// ContextPack matches the neighbourhood summary shape described in HEXXLA.md
// (Cells + TotalTokens + Seams).
type ContextPack struct {
	Cells        []CellView
	TotalTokens  int
	Seams        []record.SeamRecord
	Stats        ContextPackStats  // assembly statistics (zero when built outside LoadContextWithBudgeting)
	Explanations []CellExplanation // per-cell decisions (only when LoadContextBudgetConfig.Explain is true)
}

// TokenBudgeter counts tokens for budgeting (e.g. approximate LLM tokens).
// Inject domain-specific logic; the default is [ByteLenBudgeter].
type TokenBudgeter interface {
	CountTokens(content string) int
}

// ByteLenBudgeter counts tokens as UTF-8 byte length (cheap default; not tokenizer-accurate).
type ByteLenBudgeter struct{}

// CountTokens implements [TokenBudgeter].
func (ByteLenBudgeter) CountTokens(content string) int { return len(content) }

// AssembleCellViewOpts configures [AssembleCellView].
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

// ── CellView predicates / utilities ──────────────────────────────────────────

// CellViewPredicate selects [CellView] rows when filtering slices produced from
// walks or budgeting helpers.
type CellViewPredicate func(CellView) bool

// FilterCellViews returns only views for which pred reports true.
// If pred is nil, returns a copy of views.
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

// TruncateCellViewsToTokenBudget returns a prefix of views (order preserved)
// such that the summed token count is at most maxTokens.
func TruncateCellViewsToTokenBudget(vs []CellView, budgeter TokenBudgeter, maxTokens int, includeFacetText bool) (out []CellView, total int) {
	if budgeter == nil {
		budgeter = ByteLenBudgeter{}
	}
	if maxTokens <= 0 || len(vs) == 0 {
		return nil, 0
	}
	for _, v := range vs {
		n := CellViewTokens(budgeter, v, includeFacetText)
		if total+n > maxTokens {
			break
		}
		out = append(out, v)
		total += n
	}
	return out, total
}

// CellViewTokens counts tokens for a single CellView, optionally including facet text.
func CellViewTokens(b TokenBudgeter, v CellView, facets bool) int {
	n := b.CountTokens(v.RawContent)
	if facets {
		for _, f := range v.Facets {
			n += b.CountTokens(f.DerivedContent)
		}
	}
	return n
}

// ── AssembleCellView ──────────────────────────────────────────────────────────

// AssembleCellView builds a [CellView] for coord using the transaction snapshot
// provided via the [TxReader] port.
func AssembleCellView(ctx context.Context, tx TxReader, coord lattice.Coord, asOf *time.Time, opts AssembleCellViewOpts) (CellView, error) {
	if err := ctx.Err(); err != nil {
		return CellView{}, err
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
		Coord:      coord,
		RawContent: rec.RawContent,
		Provenance: rec.Provenance,
		Validity:   rec.Validity,
		Tags:       rec.Tags,
	}
	if rec.ClusterHint != nil {
		ch, err := lattice.Unpack(*rec.ClusterHint)
		if err == nil {
			out.ClusterHint = &ch
		}
	}
	if opts.IncludeFacets {
		if err := assembleFacets(tx, packed, &out); err != nil {
			return CellView{}, err
		}
	}
	if opts.IncludeEdges {
		if err := assembleEdges(tx, packed, &out); err != nil {
			return CellView{}, err
		}
	}
	if opts.IncludeSeams {
		if err := assembleSeams(ctx, tx, coord, opts, &out); err != nil {
			return CellView{}, err
		}
	}
	return out, nil
}

// assembleFacets populates out.Facets from the transaction.
func assembleFacets(tx TxReader, packed lattice.PackedCoord, out *CellView) error {
	return tx.AscendFacetsForCell(packed, func(fr record.FacetRecord) bool {
		out.Facets = append(out.Facets, facetViewFromRecord(fr))
		return true
	})
}

// assembleEdges populates out.Edges from the transaction.
func assembleEdges(tx TxReader, packed lattice.PackedCoord, out *CellView) error {
	return tx.AscendEdgesFrom(packed, func(er record.EdgeRecord) bool {
		to, err := lattice.Unpack(er.To)
		if err != nil {
			return true
		}
		out.Edges = append(out.Edges, EdgeView{To: to, RelationType: er.RelationType, Weight: er.Weight})
		return true
	})
}

// assembleSeams populates out.Seams with seams incident to coord.
func assembleSeams(ctx context.Context, tx TxReader, coord lattice.Coord, opts AssembleCellViewOpts, out *CellView) error {
	radius := opts.SeamSearchRadius
	if radius < 0 {
		return ErrInvalidArgument
	}
	seams, err := tx.FindSeams(ctx, coord, radius, opts.UnresolvedSeamsOnly)
	if err != nil {
		return err
	}
	for _, s := range seams {
		a, aErr := lattice.Unpack(s.CellA)
		if aErr != nil {
			continue
		}
		b, bErr := lattice.Unpack(s.CellB)
		if bErr != nil {
			continue
		}
		if a != coord && b != coord {
			continue
		}
		out.Seams = append(out.Seams, SeamRef{
			ID:               s.ID,
			SeamType:         s.SeamType,
			ResolutionStatus: s.ResolutionStatus,
		})
	}
	return nil
}

func facetViewFromRecord(fr record.FacetRecord) FacetView {
	return FacetView{
		ID:             fr.FacetID,
		DerivedContent: fr.DerivedContent,
		LastRotated:    time.Unix(0, fr.LastRotated).UTC(),
		DerivationHash: hex.EncodeToString(fr.DerivationHash[:]),
	}
}

// ── sentinel errors ───────────────────────────────────────────────────────────

// ErrCellNotFound is returned by [AssembleCellView] when the cell does not
// exist or its validity does not cover asOf.
// The root package maps this to its own ErrCellNotFound sentinel.
var ErrCellNotFound = errors.New("views: cell not found")

// ErrInvalidArgument is returned when a configuration argument is out of range.
var ErrInvalidArgument = errors.New("views: invalid argument")
