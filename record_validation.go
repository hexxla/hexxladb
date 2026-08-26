package hexxladb

import (
	"fmt"
	"math"

	"github.com/hexxla/hexxladb/internal/lattice"
	"github.com/hexxla/hexxladb/internal/record"
)

func validatePackedCoordInvariant(field string, coord lattice.PackedCoord) error {
	if _, err := lattice.Unpack(coord); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func validateFiniteInvariant(field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s must be finite", field)
	}
	return nil
}

func validateProvenanceInvariants(provenance record.ProvenanceWire) error {
	return validateFiniteInvariant("provenance confidence", provenance.Confidence)
}

func validateCellInvariants(rec record.CellRecord) error {
	if err := validatePackedCoordInvariant("cell key", rec.Key); err != nil {
		return err
	}
	if rec.ClusterHint != nil {
		if err := validatePackedCoordInvariant("cell cluster hint", *rec.ClusterHint); err != nil {
			return err
		}
	}
	return validateProvenanceInvariants(rec.Provenance)
}

func validateFacetInvariants(rec record.FacetRecord) error {
	return validatePackedCoordInvariant("facet key", rec.Key)
}

func validateEdgeInvariants(rec record.EdgeRecord) error {
	if err := validatePackedCoordInvariant("edge from", rec.From); err != nil {
		return err
	}
	if err := validatePackedCoordInvariant("edge to", rec.To); err != nil {
		return err
	}
	if err := validateFiniteInvariant("edge weight", rec.Weight); err != nil {
		return err
	}
	return validateProvenanceInvariants(rec.Provenance)
}

func validateSeamInvariants(rec record.SeamRecord) error {
	if err := validatePackedCoordInvariant("seam cell A", rec.CellA); err != nil {
		return err
	}
	if err := validatePackedCoordInvariant("seam cell B", rec.CellB); err != nil {
		return err
	}
	if err := validateFiniteInvariant("seam confidence delta", rec.ConfidenceDelta); err != nil {
		return err
	}
	return validateProvenanceInvariants(rec.Provenance)
}

func invalidTypedRecord(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
}
