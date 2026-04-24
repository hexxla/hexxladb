package hexxladb

import (
	"time"

	"github.com/hexxla/hexxladb/internal/record"
)

// NewUserMessageCell returns a CellRecord pre-configured for a user message with standard tags.
func NewUserMessageCell(coord PackedCoord, content, sessionID string, confidence float64) record.CellRecord {
	now := time.Now().UTC().UnixNano()
	return record.CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"user-message", "conversation"},
		Provenance: record.ProvenanceWire{
			SourceID:   sessionID + "/user",
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewAssistantResponseCell returns a CellRecord pre-configured for an assistant response with standard tags.
func NewAssistantResponseCell(coord PackedCoord, content, sessionID string, confidence float64) record.CellRecord {
	now := time.Now().UTC().UnixNano()
	return record.CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"assistant-response", "conversation"},
		Provenance: record.ProvenanceWire{
			SourceID:   sessionID + "/assistant",
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewSystemPromptCell returns a CellRecord pre-configured for a system prompt with standard tags.
// System prompts typically have high confidence and a version identifier as source.
func NewSystemPromptCell(coord PackedCoord, content, version string) record.CellRecord {
	now := time.Now().UTC().UnixNano()
	return record.CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"system-prompt"},
		Provenance: record.ProvenanceWire{
			SourceID:   "system/" + version,
			Confidence: 1.0,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}

// NewFactCell returns a CellRecord pre-configured for a factual statement with standard tags.
// Facts are typically extracted from conversation and tagged with their source and category.
func NewFactCell(coord PackedCoord, content, sourceID, factType string, confidence float64) record.CellRecord {
	now := time.Now().UTC().UnixNano()
	return record.CellRecord{
		Key:        coord,
		RawContent: content,
		Tags:       []string{"fact", factType},
		Provenance: record.ProvenanceWire{
			SourceID:   sourceID,
			Confidence: confidence,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}
}
