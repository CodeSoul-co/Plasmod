package materialization

import (
	"fmt"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
)

// MaterializationResult is the full output of a single event materialization.
// It bundles:
//   - IngestRecord  — the retrieval-plane projection (goes to TieredDataPlane)
//   - Memory        — the canonical Memory object derived from the event
//   - Version       — the ObjectVersion record for the new Memory
//   - Edges         — any typed edges inferred from the event (causal, session, agent)
//
// The Runtime writes Memory + Version + Edges to their respective stores so
// the canonical object layer stays consistent with what the retrieval plane sees.
type MaterializationResult struct {
	Record  dataplane.IngestRecord
	Memory  schemas.Memory
	Version schemas.ObjectVersion
	Edges   []schemas.Edge
}

// Service converts events into canonical object mutations and retrieval-ready
// projection records.  In v1 the derivation logic is intentionally lightweight,
// but the module boundary is stable and the output shape is production-aligned.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

// MaterializeEvent is the primary ingest-time entry-point.  It returns a full
// MaterializationResult so the Runtime can persist the canonical objects in the
// same transaction as the retrieval ingest.
func (s *Service) MaterializeEvent(ev schemas.Event) MaterializationResult {
	text := extractText(ev)
	namespace := resolveNamespace(ev)
	memoryID := fmt.Sprintf("mem_%s", ev.EventID)
	now := time.Now().UTC().Format(time.RFC3339)

	record := dataplane.IngestRecord{
		ObjectID:    memoryID,
		Text:        text,
		Namespace:   namespace,
		Attributes:  buildAttributes(ev),
		EventUnixTS: parseEventUnixTS(ev),
	}

	mem := schemas.Memory{
		MemoryID:       memoryID,
		MemoryType:     resolveMemoryType(ev),
		AgentID:        ev.AgentID,
		SessionID:      ev.SessionID,
		Scope:          namespace,
		Level:          0,
		Content:        text,
		Summary:        text,
		SourceEventIDs: []string{ev.EventID},
		Confidence:     1.0,
		Importance:     ev.Importance,
		FreshnessScore: 1.0,
		ValidFrom:      now,
		ProvenanceRef:  ev.EventID,
		Version:        ev.LogicalTS,
		IsActive:       true,
	}

	version := schemas.ObjectVersion{
		ObjectID:        memoryID,
		ObjectType:      "memory",
		Version:         ev.LogicalTS,
		MutationEventID: ev.EventID,
		ValidFrom:       now,
		SnapshotTag:     fmt.Sprintf("ingest:%s", ev.EventType),
	}

	edges := deriveEdges(ev, memoryID)

	return MaterializationResult{
		Record:  record,
		Memory:  mem,
		Version: version,
		Edges:   edges,
	}
}

// ProjectEvent is kept for backward-compatibility.  New code should call
// MaterializeEvent and use MaterializationResult.Record.
func (s *Service) ProjectEvent(ev schemas.Event) dataplane.IngestRecord {
	return s.MaterializeEvent(ev).Record
}

func extractText(ev schemas.Event) string {
	if msg, ok := ev.Payload["text"]; ok {
		if value, ok := msg.(string); ok {
			return value
		}
	}
	return ""
}

func resolveNamespace(ev schemas.Event) string {
	if ev.WorkspaceID != "" {
		return ev.WorkspaceID
	}
	if ev.SessionID != "" {
		return ev.SessionID
	}
	return "default"
}

func resolveMemoryType(ev schemas.Event) string {
	switch ev.EventType {
	case "user_message", "assistant_message":
		return "episodic"
	case "critique_generated", "reflection":
		return "reflective"
	case "plan_updated":
		return "procedural"
	case "tool_result_returned":
		return "factual"
	default:
		return "episodic"
	}
}

// deriveEdges infers typed edges from the event relationships.
// In v1 we emit:
//   - memory → session  ("belongs_to_session")
//   - memory → agent    ("owned_by_agent")
//   - memory → causal   ("derived_from")  for each causal ref
func deriveEdges(ev schemas.Event, memoryID string) []schemas.Edge {
	now := time.Now().UTC().Format(time.RFC3339)
	edges := []schemas.Edge{}

	if ev.SessionID != "" {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("edge_%s_session", memoryID),
			SrcObjectID:   memoryID,
			SrcType:       "memory",
			EdgeType:      "belongs_to_session",
			DstObjectID:   ev.SessionID,
			DstType:       "session",
			Weight:        1.0,
			ProvenanceRef: ev.EventID,
			CreatedTS:     now,
		})
	}
	if ev.AgentID != "" {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("edge_%s_agent", memoryID),
			SrcObjectID:   memoryID,
			SrcType:       "memory",
			EdgeType:      "owned_by_agent",
			DstObjectID:   ev.AgentID,
			DstType:       "agent",
			Weight:        1.0,
			ProvenanceRef: ev.EventID,
			CreatedTS:     now,
		})
	}
	for i, ref := range ev.CausalRefs {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("edge_%s_causal_%d", memoryID, i),
			SrcObjectID:   memoryID,
			SrcType:       "memory",
			EdgeType:      "derived_from",
			DstObjectID:   ref,
			DstType:       "event",
			Weight:        0.8,
			ProvenanceRef: ev.EventID,
			CreatedTS:     now,
		})
	}
	return edges
}

func buildAttributes(ev schemas.Event) map[string]string {
	return map[string]string{
		"tenant_id":    ev.TenantID,
		"workspace_id": ev.WorkspaceID,
		"agent_id":     ev.AgentID,
		"session_id":   ev.SessionID,
		"event_type":   ev.EventType,
		"visibility":   ev.Visibility,
	}
}

func parseEventUnixTS(ev schemas.Event) int64 {
	if ts, ok := parseRFC3339ToUnix(ev.EventTime); ok {
		return ts
	}
	if ts, ok := parseRFC3339ToUnix(ev.IngestTime); ok {
		return ts
	}
	return time.Now().Unix()
}

func parseRFC3339ToUnix(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0, false
	}
	return ts.Unix(), true
}
