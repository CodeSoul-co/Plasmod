package nodes_test

import (
	"testing"

	"andb/src/internal/dataplane"
	"andb/src/internal/storage"
	baseline "andb/src/internal/worker/cognitive/baseline"
	"andb/src/internal/worker/coordination"
	"andb/src/internal/worker/indexing"
	"andb/src/internal/worker/nodes"
)

func TestManager_RegisterAndTopology(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	m := nodes.CreateManager()

	m.RegisterData(nodes.CreateInMemoryDataNode("data-1", store.Segments()))
	m.RegisterIndex(nodes.CreateInMemoryIndexNode("index-1", store.Indexes()))
	m.RegisterQuery(nodes.CreateInMemoryQueryNode("query-1", nil))
	m.RegisterMemoryExtraction(baseline.CreateInMemoryMemoryExtractionWorker("mem-extract-1", store.Objects(), nil))
	m.RegisterMemoryConsolidation(baseline.CreateInMemoryMemoryConsolidationWorker("mem-consolidate-1", store.Objects(), nil))
	m.RegisterGraphRelation(indexing.CreateInMemoryGraphRelationWorker("graph-1", store.Edges()))
	m.RegisterProofTrace(coordination.CreateInMemoryProofTraceWorker("proof-1", store.Edges(), nil))

	topo := m.Topology()
	if topo == nil {
		t.Fatal("Topology: should not be nil")
	}

	data, idx := 0, 0
	for _, n := range topo {
		switch n.Type {
		case nodes.NodeTypeData:
			data++
		case nodes.NodeTypeIndex:
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
	m := nodes.CreateManager()
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
