package storage

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func TestMemoryRuntimeStorage_Stores(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	if store.Objects() == nil {
		t.Fatal("Objects() should not be nil")
	}
	if store.Segments() == nil {
		t.Fatal("Segments() should not be nil")
	}
	if store.Indexes() == nil {
		t.Fatal("Indexes() should not be nil")
	}
	if store.Edges() == nil {
		t.Fatal("Edges() should not be nil")
	}
	if store.Versions() == nil {
		t.Fatal("Versions() should not be nil")
	}
	if store.Policies() == nil {
		t.Fatal("Policies() should not be nil")
	}
	if store.HotCache() == nil {
		t.Fatal("HotCache() should not be nil")
	}
	if store.Audits() == nil {
		t.Fatal("Audits() should not be nil")
	}
	if store.AlgorithmStates() == nil {
		t.Fatal("AlgorithmStates() should not be nil")
	}
}

func TestMemoryObjectStore_PutAndGet(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	mem := schemas.Memory{
		MemoryID:   "mem_test_1",
		MemoryType: "episodic",
		AgentID:    "agent_1",
		Content:    "test content",
		IsActive:   true,
	}
	store.Objects().PutMemory(mem)

	got, ok := store.Objects().GetMemory("mem_test_1")
	if !ok {
		t.Fatal("GetMemory: expected to find mem_test_1")
	}
	if got.Content != "test content" {
		t.Errorf("Content: want %q, got %q", "test content", got.Content)
	}
}

func TestMemoryGraphEdgeStore_BulkEdges(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	es := store.Edges()

	e1 := schemas.Edge{
		EdgeID:      "edge_1",
		SrcObjectID: "mem_1",
		DstObjectID: "agent_1",
		EdgeType:    "owned_by_agent",
	}
	e2 := schemas.Edge{
		EdgeID:      "edge_2",
		SrcObjectID: "mem_2",
		DstObjectID: "mem_1",
		EdgeType:    "derived_from",
	}
	es.PutEdge(e1)
	es.PutEdge(e2)

	bulk := es.BulkEdges([]string{"mem_1"})
	if len(bulk) != 2 {
		t.Errorf("BulkEdges: want 2 edges incident to mem_1, got %d", len(bulk))
	}
}

func TestMemoryGraphEdgeStore_DeleteEdge(t *testing.T) {
	store := NewMemoryRuntimeStorage()
	es := store.Edges()

	es.PutEdge(schemas.Edge{EdgeID: "edge_del", SrcObjectID: "a", DstObjectID: "b", EdgeType: "x"})
	es.DeleteEdge("edge_del")

	edges := es.BulkEdges([]string{"a"})
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after DeleteEdge, got %d", len(edges))
	}
}

// R2: secondary-index EdgesFrom/EdgesTo
func TestMemoryGraphEdgeStore_EdgesFrom_Indexed(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "src1", DstObjectID: "dst1", EdgeType: "x"})
	es.PutEdge(schemas.Edge{EdgeID: "e2", SrcObjectID: "src1", DstObjectID: "dst2", EdgeType: "y"})
	es.PutEdge(schemas.Edge{EdgeID: "e3", SrcObjectID: "src2", DstObjectID: "dst1", EdgeType: "z"})

	got := es.EdgesFrom("src1")
	if len(got) != 2 {
		t.Errorf("EdgesFrom src1: want 2, got %d", len(got))
	}
	got2 := es.EdgesFrom("nonexistent")
	if len(got2) != 0 {
		t.Errorf("EdgesFrom nonexistent: want 0, got %d", len(got2))
	}
}

func TestMemoryGraphEdgeStore_EdgesTo_Indexed(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "s1", DstObjectID: "dst1", EdgeType: "a"})
	es.PutEdge(schemas.Edge{EdgeID: "e2", SrcObjectID: "s2", DstObjectID: "dst1", EdgeType: "b"})
	es.PutEdge(schemas.Edge{EdgeID: "e3", SrcObjectID: "s3", DstObjectID: "dst2", EdgeType: "c"})

	got := es.EdgesTo("dst1")
	if len(got) != 2 {
		t.Errorf("EdgesTo dst1: want 2, got %d", len(got))
	}
}

func TestMemoryGraphEdgeStore_IndexConsistencyAfterDelete(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "e1", SrcObjectID: "src1", DstObjectID: "dst1"})
	es.DeleteEdge("e1")

	if len(es.EdgesFrom("src1")) != 0 {
		t.Error("index not cleaned after DeleteEdge from srcIdx")
	}
	if len(es.EdgesTo("dst1")) != 0 {
		t.Error("index not cleaned after DeleteEdge from dstIdx")
	}
}

// R7: ExpiresAt field + PruneExpiredEdges
func TestMemoryGraphEdgeStore_PruneExpiredEdges(t *testing.T) {
	es := newMemoryGraphEdgeStore()
	es.PutEdge(schemas.Edge{EdgeID: "live", SrcObjectID: "a", DstObjectID: "b", ExpiresAt: "2099-01-01T00:00:00Z"})
	es.PutEdge(schemas.Edge{EdgeID: "dead", SrcObjectID: "c", DstObjectID: "d", ExpiresAt: "2000-01-01T00:00:00Z"})
	es.PutEdge(schemas.Edge{EdgeID: "eternal", SrcObjectID: "e", DstObjectID: "f"}) // no expiry

	pruned := es.PruneExpiredEdges("2026-01-01T00:00:00Z")
	if pruned != 1 {
		t.Errorf("PruneExpiredEdges: want 1 pruned, got %d", pruned)
	}
	if _, ok := es.GetEdge("dead"); ok {
		t.Error("expired edge 'dead' should have been removed")
	}
	if _, ok := es.GetEdge("live"); !ok {
		t.Error("non-expired edge 'live' should still exist")
	}
	if _, ok := es.GetEdge("eternal"); !ok {
		t.Error("no-expiry edge 'eternal' should still exist")
	}
	// verify index was cleaned for pruned edge
	if len(es.EdgesFrom("c")) != 0 {
		t.Error("srcIdx not cleaned for pruned edge")
	}
}

// R6: InMemoryColdStore edge methods
func TestInMemoryColdStore_EdgeRoundtrip(t *testing.T) {
	cold := NewInMemoryColdStore()

	e := schemas.Edge{EdgeID: "cold_e1", SrcObjectID: "mem_1", DstObjectID: "evt_1", EdgeType: "derived_from", Weight: 1.0}
	cold.PutEdge(e)

	got, ok := cold.GetEdge("cold_e1")
	if !ok {
		t.Fatal("GetEdge: expected to find cold_e1")
	}
	if got.EdgeType != "derived_from" {
		t.Errorf("EdgeType: want derived_from, got %s", got.EdgeType)
	}

	list := cold.ListEdges()
	if len(list) != 1 {
		t.Errorf("ListEdges: want 1, got %d", len(list))
	}
}

func TestInMemoryColdStore_ArtifactRoundtrip(t *testing.T) {
	cold := NewInMemoryColdStore()
	art := schemas.Artifact{
		ArtifactID:   "art_cold_1",
		SessionID:    "sess_1",
		OwnerAgentID: "agent_1",
		URI:          "s3://bucket/a.bin",
	}
	cold.PutArtifact(art)
	got, ok := cold.GetArtifact("art_cold_1")
	if !ok {
		t.Fatal("GetArtifact: expected to find art_cold_1")
	}
	if got.URI != "s3://bucket/a.bin" {
		t.Fatalf("artifact URI: got %q", got.URI)
	}
}

// PC: AuditStore
func TestInMemoryAuditStore_AppendAndGet(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	r1 := schemas.AuditRecord{
		RecordID:       "audit_1",
		TargetMemoryID: "mem_audit_1",
		OperationType:  string(schemas.AuditOpRead),
		ActorType:      "agent",
		ActorID:        "agent_1",
		Decision:       "allow",
		Timestamp:      "2026-01-01T00:00:00Z",
	}
	r2 := schemas.AuditRecord{
		RecordID:       "audit_2",
		TargetMemoryID: "mem_audit_1",
		OperationType:  string(schemas.AuditOpShare),
		ActorType:      "agent",
		ActorID:        "agent_2",
		Decision:       "deny",
		Timestamp:      "2026-01-02T00:00:00Z",
	}
	store.Audits().AppendAudit(r1)
	store.Audits().AppendAudit(r2)

	got := store.Audits().GetAudits("mem_audit_1")
	if len(got) != 2 {
		t.Errorf("GetAudits: want 2 records, got %d", len(got))
	}
	all := store.Audits().ListAudits()
	if len(all) != 2 {
		t.Errorf("ListAudits: want 2 total, got %d", len(all))
	}
	// unrelated memory should return empty
	none := store.Audits().GetAudits("mem_nonexistent")
	if len(none) != 0 {
		t.Errorf("GetAudits nonexistent: want 0, got %d", len(none))
	}
}

// PC: MemoryAlgorithmStateStore
func TestInMemoryAlgorithmStateStore_PutAndGet(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	st := schemas.MemoryAlgorithmState{
		MemoryID:       "mem_1",
		AlgorithmID:    "memorybank_v1",
		Strength:       0.85,
		RetentionScore: 0.72,
		RecallCount:    3,
		UpdatedAt:      "2026-01-01T00:00:00Z",
	}
	store.AlgorithmStates().PutAlgorithmState(st)

	got, ok := store.AlgorithmStates().GetAlgorithmState("mem_1", "memorybank_v1")
	if !ok {
		t.Fatal("GetAlgorithmState: expected to find mem_1/memorybank_v1")
	}
	if got.Strength != 0.85 {
		t.Errorf("Strength: want 0.85, got %f", got.Strength)
	}

	list := store.AlgorithmStates().ListAlgorithmStates("mem_1")
	if len(list) != 1 {
		t.Errorf("ListAlgorithmStates: want 1, got %d", len(list))
	}
	_, miss := store.AlgorithmStates().GetAlgorithmState("mem_1", "other_algo")
	if miss {
		t.Error("GetAlgorithmState: should not find state for unknown algorithmID")
	}
}

func TestTieredObjectStore_ArchiveEdge(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewInMemoryColdStore()

	tiered := NewTieredObjectStore(hot, warm, warmEdges, cold)

	warmEdges.PutEdge(schemas.Edge{
		EdgeID:      "e_arc",
		SrcObjectID: "m1",
		DstObjectID: "m2",
		EdgeType:    "derived_from",
	})

	tiered.ArchiveEdge(warmEdges, "e_arc")

	if _, ok := warmEdges.GetEdge("e_arc"); ok {
		t.Error("ArchiveEdge: edge should be removed from warm store")
	}
	if _, ok := cold.GetEdge("e_arc"); !ok {
		t.Error("ArchiveEdge: edge should exist in cold store")
	}
}

func TestTieredObjectStore_ArchiveStateAndArtifact(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	cold := NewInMemoryColdStore()
	tiered := NewTieredObjectStore(hot, warm, newMemoryGraphEdgeStore(), cold)

	st := schemas.State{StateID: "state_arch_1", SessionID: "sess_1", AgentID: "agent_1"}
	art := schemas.Artifact{ArtifactID: "art_arch_1", SessionID: "sess_1", OwnerAgentID: "agent_1"}
	warm.PutState(st)
	warm.PutArtifact(art)

	tiered.ArchiveState(st.StateID)
	tiered.ArchiveArtifact(art.ArtifactID)

	if _, ok := cold.GetState(st.StateID); !ok {
		t.Fatal("ArchiveState: expected state in cold store")
	}
	if _, ok := cold.GetArtifact(art.ArtifactID); !ok {
		t.Fatal("ArchiveArtifact: expected artifact in cold store")
	}
}

func TestTieredObjectStore_GetStateAndArtifactActivated_FromCold(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	cold := NewInMemoryColdStore()
	tiered := NewTieredObjectStore(hot, warm, newMemoryGraphEdgeStore(), cold)

	cold.PutState(schemas.State{StateID: "state_cold_1", AgentID: "agent_1", SessionID: "sess_1"})
	cold.PutArtifact(schemas.Artifact{ArtifactID: "art_cold_1", SessionID: "sess_1", OwnerAgentID: "agent_1"})

	st, ok := tiered.GetStateActivated("state_cold_1")
	if !ok || st.StateID != "state_cold_1" {
		t.Fatalf("GetStateActivated cold hit failed: ok=%v state=%+v", ok, st)
	}
	if _, stateOk := warm.GetState("state_cold_1"); !stateOk {
		t.Fatal("GetStateActivated: expected state promoted to warm")
	}

	art, ok := tiered.GetArtifactActivated("art_cold_1")
	if !ok || art.ArtifactID != "art_cold_1" {
		t.Fatalf("GetArtifactActivated cold hit failed: ok=%v art=%+v", ok, art)
	}
	if _, artOk := warm.GetArtifact("art_cold_1"); !artOk {
		t.Fatal("GetArtifactActivated: expected artifact promoted to warm")
	}
}

func TestTieredObjectStore_ArchiveColdRecord_PreservesCanonicalMemory(t *testing.T) {
	hot := NewHotObjectCache(100)
	warm := newMemoryObjectStore()
	warmEdges := newMemoryGraphEdgeStore()
	cold := NewInMemoryColdStore()

	tiered := NewTieredObjectStore(hot, warm, warmEdges, cold)

	orig := schemas.Memory{
		MemoryID:       "mem_cold_roundtrip_1",
		AgentID:        "agent_rt",
		SessionID:      "session_rt",
		Content:        "NVDA Q3 revenue reached $35.1B",
		Summary:        "Q3 revenue summary",
		MemoryType:     string(schemas.MemoryTypeSemantic),
		SourceEventIDs: []string{"evt_rt_1", "evt_rt_2"},
		ProvenanceRef:  "evt_rt_1",
		Scope:          "workspace_shared",
		OwnerType:      "tool_result",
		Version:        42,
		IsActive:       true,
	}

	warm.PutMemory(orig)

	attrs := map[string]string{
		"agent_id":   "agent_rt",
		"session_id": "session_rt",
		"visibility": "workspace_shared",
		"event_type": "tool_result",
	}

	tiered.ArchiveColdRecord(orig.MemoryID, orig.Content, attrs, "ws_rt", 42)

	// Force a cold-path reactivation by using a fresh warm store.
	freshWarm := newMemoryObjectStore()
	tieredColdRead := NewTieredObjectStore(hot, freshWarm, warmEdges, cold)

	got, ok := tieredColdRead.GetMemoryActivated(orig.MemoryID, 0.8)
	if !ok {
		t.Fatalf("expected cold archived memory to be re-activated")
	}

	if got.MemoryID != orig.MemoryID {
		t.Fatalf("MemoryID mismatch: want %q, got %q", orig.MemoryID, got.MemoryID)
	}
	if got.Content != orig.Content {
		t.Errorf("Content mismatch: want %q, got %q", orig.Content, got.Content)
	}
	if got.Summary != orig.Summary {
		t.Errorf("Summary mismatch: want %q, got %q", orig.Summary, got.Summary)
	}
	if got.MemoryType != orig.MemoryType {
		t.Errorf("MemoryType mismatch: want %q, got %q", orig.MemoryType, got.MemoryType)
	}
	if got.ProvenanceRef != orig.ProvenanceRef {
		t.Errorf("ProvenanceRef mismatch: want %q, got %q", orig.ProvenanceRef, got.ProvenanceRef)
	}
	if got.AgentID != orig.AgentID {
		t.Errorf("AgentID mismatch: want %q, got %q", orig.AgentID, got.AgentID)
	}
	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID mismatch: want %q, got %q", orig.SessionID, got.SessionID)
	}
	if got.Scope != orig.Scope {
		t.Errorf("Scope mismatch: want %q, got %q", orig.Scope, got.Scope)
	}
	if got.OwnerType != orig.OwnerType {
		t.Errorf("OwnerType mismatch: want %q, got %q", orig.OwnerType, got.OwnerType)
	}
	if got.Version != orig.Version {
		t.Errorf("Version mismatch: want %d, got %d", orig.Version, got.Version)
	}
	if len(got.SourceEventIDs) != len(orig.SourceEventIDs) {
		t.Fatalf("SourceEventIDs length mismatch: want %d, got %d", len(orig.SourceEventIDs), len(got.SourceEventIDs))
	}
	for i := range orig.SourceEventIDs {
		if got.SourceEventIDs[i] != orig.SourceEventIDs[i] {
			t.Errorf("SourceEventIDs[%d] mismatch: want %q, got %q", i, orig.SourceEventIDs[i], got.SourceEventIDs[i])
		}
	}
}

func TestMemoryRuntimeStorage_PutMemoryWithBaseEdges(t *testing.T) {
	store := NewMemoryRuntimeStorage()

	mem := schemas.Memory{
		MemoryID:       "mem_auto_1",
		AgentID:        "agent_1",
		SessionID:      "sess_1",
		ProvenanceRef:  "evt_1",
		SourceEventIDs: []string{"evt_1"},
	}

	store.PutMemoryWithBaseEdges(mem)

	_, ok := store.Objects().GetMemory("mem_auto_1")
	if !ok {
		t.Fatal("expected memory to be stored")
	}

	edges := store.Edges().EdgesFrom("mem_auto_1")
	if len(edges) != 3 {
		t.Fatalf("expected 3 auto-built edges, got %d", len(edges))
	}

	var hasSession, hasAgent, hasDerived bool
	for _, e := range edges {
		switch e.EdgeType {
		case string(schemas.EdgeTypeBelongsToSession):
			if e.DstObjectID == "sess_1" {
				hasSession = true
			}
		case string(schemas.EdgeTypeOwnedByAgent):
			if e.DstObjectID == "agent_1" {
				hasAgent = true
			}
		case string(schemas.EdgeTypeDerivedFrom):
			if e.DstObjectID == "evt_1" {
				hasDerived = true
			}
		}
	}

	if !hasSession || !hasAgent || !hasDerived {
		t.Fatalf("missing expected auto-built edges: %+v", edges)
	}

}
