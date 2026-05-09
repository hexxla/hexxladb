package hexxladb

import (
	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
	"github.com/hexxla/hexxladb/internal/views"
)

// ── Coordinate types ──────────────────────────────────────────────────────────

// Coord is an axial hex coordinate (q, r); cube s = -q - r. See [Coord.Cube].
type Coord = lattice.Coord

// Cube holds cube coordinates with q + r + s == 0.
type Cube = lattice.Cube

// PackedCoord is a 128-bit Morton-order key; see internal/lattice/PACKED_COORD.md.
type PackedCoord = lattice.PackedCoord

// MaxAxialAbs is the maximum absolute Q and R allowed by [Pack].
const MaxAxialAbs = lattice.MaxAxialAbs

// ErrCoordOutOfRange means coordinates are outside the range accepted by [Pack].
var ErrCoordOutOfRange = lattice.ErrCoordOutOfRange

// Pack encodes an axial coordinate to a Morton [PackedCoord].
func Pack(c Coord) (PackedCoord, error) { return lattice.Pack(c) }

// Unpack decodes a v1 [PackedCoord] with reserved high word zero.
func Unpack(p PackedCoord) (Coord, error) { return lattice.Unpack(p) }

// Ring returns all cells at hex distance k from center in load_context order.
func Ring(center Coord, k int) []Coord { return lattice.Ring(center, k) }

// WalkRings appends center, then rings 1..maxR, each in [Ring] order.
func WalkRings(dst []Coord, center Coord, maxR int) []Coord {
	return lattice.WalkRings(dst, center, maxR)
}

// ── Record types ──────────────────────────────────────────────────────────────

// ProvenanceWire is provenance stored in v1 payloads (times as Unix nanoseconds UTC).
// Re-exported from internal/record for public API access.
type ProvenanceWire = record.ProvenanceWire

// ValidityWire is an optional validity window (nil = open-ended on that side).
// Re-exported from internal/record for public API access.
type ValidityWire = record.ValidityWire

// CellRecord is the v1 wire shape for cell/<packed_coord> blobs.
// Re-exported from internal/record for public API access.
// Used by [Tx.ScanContextRaw], [Tx.ScanContextAtRaw], and low-level cell operations.
type CellRecord = record.CellRecord

// FacetWalkRecord is an alias of the facet wire record so embedding apps (e.g. MCP adapters) can write
// [Tx.AscendFacetsForCell] callbacks without importing internal packages.
type FacetWalkRecord = record.FacetRecord

// EdgeWalkRecord is an alias of the edge wire record for [Tx.AscendEdgesFrom] callbacks.
type EdgeWalkRecord = record.EdgeRecord

// ── View types ────────────────────────────────────────────────────────────────

// FacetView is a read-oriented facet projection aligned with docs/hexxladb/HEXXLA.md.
type FacetView = views.FacetView

// EdgeView summarises one directed edge from a cell.
type EdgeView = views.EdgeView

// SeamRef is a lightweight seam handle for embedding in [CellView].
type SeamRef = views.SeamRef

// CellView aggregates decoded cell content with optional facets, edges, and seam refs.
type CellView = views.CellView

// ContextPackStats records assembly statistics for debugging and observability.
type ContextPackStats = views.ContextPackStats

// CellExplanation records why a cell was included or excluded during context assembly.
type CellExplanation = views.CellExplanation

// ContextPack matches the neighbourhood summary shape described in HEXXLA.md.
type ContextPack = views.ContextPack

// TokenBudgeter counts tokens for budgeting (e.g. approximate LLM tokens).
// Inject domain-specific logic; the default is [ByteLenBudgeter].
type TokenBudgeter = views.TokenBudgeter

// ByteLenBudgeter counts tokens as UTF-8 byte length (cheap default; not tokenizer-accurate).
type ByteLenBudgeter = views.ByteLenBudgeter

// AssembleCellViewOpts configures [Tx.AssembleCellView].
type AssembleCellViewOpts = views.AssembleCellViewOpts

// LoadContextBudgetConfig configures context assembly options for [Tx.LoadContext].
type LoadContextBudgetConfig = views.LoadContextBudgetConfig

// CellViewPredicate selects [CellView] rows when filtering slices.
type CellViewPredicate = views.CellViewPredicate
