package main

import (
	"strings"

	"github.com/hexxla/hexxladb/internal/record"
)

// HEXXLA-shaped provenance SourceIDs — many roles you'd see when persisting chat,
// retrieval, tooling, moderation, and durable summaries into one spatial session.
const (
	sourceSystem     = "session/system"
	sourceDeveloper  = "session/developer"
	sourceUser       = "session/user"
	sourceAssistant  = "session/assistant"
	sourceTool       = "session/tool"
	sourceRetrieval  = "retrieval/chunk"
	sourceModeration = "session/moderation"
	sourceMemory     = "memory/summary"

	tagTopicPrefs    = "topic/preferences"
	tagTopicProject  = "topic/project-alpha"
	tagTopicSecurity = "topic/security"
	tagTopicIncident = "topic/incident"
	tagTopicQuota    = "topic/quota"
	tagTopicObs      = "topic/observability"
	tagRetrievalKB   = "retrieval/knowledge-base"
	tagMemorySession = "memory/session"
	tagModeration    = "policy/moderation"
	tagToolMeta      = "tool/metadata"

	tagRoleSystem    = "role/system"
	tagRoleDeveloper = "role/developer"
	tagRoleUser      = "role/user"
	tagRoleAssistant = "role/assistant"
	tagRoleTool      = "role/tool"
	tagRoleRetrieval = "role/retrieval"
)

// sessionTurn is one persisted cell worth of material (what a gateway might write per turn).
type sessionTurn struct {
	SourceID string
	Tags     []string
	Text     string
	ToolName string // optional: structured tool name + JSON args appended to RawContent
	ArgsJSON string
}

func (t sessionTurn) rawContent() string {
	b := strings.TrimSpace(t.Text)
	if t.ToolName == "" {
		return b
	}
	a := strings.TrimSpace(t.ArgsJSON)
	if a == "" {
		a = "{}"
	}
	return b + "\n\n## tool:" + t.ToolName + "\n```json\n" + a + "\n```\n"
}

func provenanceForRole(role string, whenUnix int64) record.ProvenanceWire {
	conf := 1.0
	switch role {
	case sourceSystem:
		conf = 0.99
	case sourceDeveloper:
		conf = 0.97
	case sourceAssistant:
		conf = 0.92
	case sourceTool:
		conf = 0.88
	case sourceRetrieval:
		conf = 0.78
	case sourceModeration:
		conf = 0.85
	case sourceMemory:
		conf = 0.82
	}
	return record.ProvenanceWire{
		SourceID:   role,
		Confidence: conf,
		CreatedAt:  whenUnix,
		UpdatedAt:  whenUnix,
	}
}
