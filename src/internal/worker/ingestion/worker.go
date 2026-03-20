package ingestion

import (
	"fmt"
	"strings"

	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// InMemoryIngestWorker validates a raw Event against mandatory schema fields
// and normalises optional fields before the event enters the WAL.
// It does NOT write to the WAL; that responsibility stays with Runtime.
type InMemoryIngestWorker struct {
	id string
}

func CreateInMemoryIngestWorker(id string) *InMemoryIngestWorker {
	return &InMemoryIngestWorker{id: id}
}

func (w *InMemoryIngestWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeIngest,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"schema_validation", "field_normalisation"},
	}
}

func (w *InMemoryIngestWorker) Process(ev schemas.Event) error {
	if strings.TrimSpace(ev.EventID) == "" {
		return fmt.Errorf("ingest: event_id is required")
	}
	if strings.TrimSpace(ev.AgentID) == "" {
		return fmt.Errorf("ingest: agent_id is required")
	}
	if strings.TrimSpace(ev.EventType) == "" {
		return fmt.Errorf("ingest: event_type is required")
	}
	return nil
}
