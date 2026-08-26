package hexxladb

import (
	"strings"
	"time"
)

// NewUserMessageCell returns a CellRecord pre-configured for a user message with standard tags.
func NewUserMessageCell(coord PackedCoord, content, sessionID string, confidence float64) CellRecord {
	now := time.Now().UTC().UnixNano()
	return CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"user-message", "conversation"},
		Provenance: ProvenanceWire{
			SourceID:   sessionID + "/user",
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewAssistantResponseCell returns a CellRecord pre-configured for an assistant response with standard tags.
func NewAssistantResponseCell(coord PackedCoord, content, sessionID string, confidence float64) CellRecord {
	now := time.Now().UTC().UnixNano()
	return CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"assistant-response", "conversation"},
		Provenance: ProvenanceWire{
			SourceID:   sessionID + "/assistant",
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewSystemPromptCell returns a CellRecord pre-configured for a system prompt with standard tags.
// System prompts typically have high confidence and a version identifier as source.
func NewSystemPromptCell(coord PackedCoord, content, version string) CellRecord {
	now := time.Now().UTC().UnixNano()
	return CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"system-prompt"},
		Provenance: ProvenanceWire{
			SourceID:   "system/" + version,
			Confidence: 1.0,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewFactCell returns a CellRecord pre-configured for a factual statement with standard tags.
// Facts are typically extracted from conversation and tagged with their source and category.
func NewFactCell(coord PackedCoord, content, sourceID, factType string, confidence float64) CellRecord {
	now := time.Now().UTC().UnixNano()
	tags := []string{"fact"}
	if factType != "" && !strings.EqualFold(factType, "fact") {
		tags = append(tags, factType)
	}
	return CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       tags,
		Provenance: ProvenanceWire{
			SourceID:   sourceID,
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewProvenanceWire builds cell/edge provenance with CreatedAt and UpdatedAt set to now (UTC, Unix nanoseconds).
// Use with [Tx.LinkCells] when the caller lives outside package hexxladb.
func NewProvenanceWire(sourceID string, confidence float64) ProvenanceWire {
	now := time.Now().UTC().UnixNano()
	return ProvenanceWire{
		SourceID:   sourceID,
		Confidence: confidence,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// NewFacetDerived returns a facet record for [Tx.PutFacet] (zero DerivationHash — use PutFacet, not UpdateFacet).
// facetID must be in the range accepted by EncodeFacet (0..5); encoding fails otherwise.
func NewFacetDerived(coord PackedCoord, facetID byte, derivedContent string, lastRotatedUnixNanoUTC int64) FacetWalkRecord {
	return FacetWalkRecord{
		Key:            coord,
		FacetID:        facetID,
		DerivedContent: derivedContent,
		LastRotated:    lastRotatedUnixNanoUTC,
	}
}
