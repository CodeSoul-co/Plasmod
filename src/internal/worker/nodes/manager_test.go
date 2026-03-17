package nodes

import (
	"testing"

	"andb/src/internal/dataplane"
	"andb/src/internal/storage"
)

func TestManager_RegisterAndTopology(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	m := NewManager()

	m.RegisterData(NewInMemoryDataNode("data-1", store.Segments()))
	m.RegisterIndex(NewInMemoryIndexNode("index-1", store.Indexes()))
	m.RegisterQuery(NewInMemoryQueryNode("query-1", nil))
	m.RegisterMemoryExtraction(NewInMemoryMemoryExtractionWorker("mem-extract-1", store.Objects()))
	m.RegisterMemoryConsolidation(NewInMemoryMemoryConsolidationWorker("mem-consolidate-1", store.Objects()))
	m.RegisterGraphRelation(NewInMemoryGraphRelationWorker("graph-1", store.Edges()))
	m.RegisterProofTrace(NewInMemoryProofTraceWorker("proof-1", store.Edges()))

	nodes := m.Topology()
	if nodes == nil {
		t.Fatal("Topology: should not be nil")
	}

	data, idx := 0, 0
	for _, n := range nodes {
		switch n.Type {
		case NodeTypeData:
			data++
		case NodeTypeIndex:
			idx++
		}
	}
	if data != 1 {
		t.Errorf("Topology: data node count: want 1, got %d", data)
	}
	if idx != 1 {
		t.Errorf("Topology: index node count: want 1, got %d", idx)
	}
}

func TestManager_DispatchIngest_NoWorkers_NoError(t *testing.T) {
	m := NewManager()
	// Should not panic with an empty manager.
	m.DispatchIngest(newTestIngestRecord())
}

func newTestIngestRecord() dataplane.IngestRecord {
	return dataplane.IngestRecord{
		ObjectID:  "mem_test",
		Text:      "test text",
		Namespace: "default",
	}
}
