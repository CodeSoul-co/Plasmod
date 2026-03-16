package materialization

import (
	"fmt"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
)

// Service converts events into retrieval-ready projection records.
// In v1 this is intentionally lightweight, but it creates a stable module
// boundary between ingest and retrieval.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) ProjectEvent(ev schemas.Event) dataplane.IngestRecord {
	text := ""
	if msg, ok := ev.Payload["text"]; ok {
		if value, ok := msg.(string); ok {
			text = value
		}
	}

	namespace := ev.WorkspaceID
	if namespace == "" {
		namespace = ev.SessionID
	}
	if namespace == "" {
		namespace = "default"
	}

	return dataplane.IngestRecord{
		ObjectID:    fmt.Sprintf("mem_%s", ev.EventID),
		Text:        text,
		Namespace:   namespace,
		Attributes:  buildAttributes(ev),
		EventUnixTS: parseEventUnixTS(ev),
	}
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
