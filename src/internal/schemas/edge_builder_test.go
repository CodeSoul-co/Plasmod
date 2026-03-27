package schemas

import "testing"

func TestBuildMemoryBaseEdges(t *testing.T) {
	m := Memory{
		MemoryID:       "mem_1",
		AgentID:        "agent_1",
		SessionID:      "sess_1",
		ProvenanceRef:  "evt_1",
		SourceEventIDs: []string{"evt_1", "evt_2"},
	}

	edges := BuildMemoryBaseEdges(m)

	if len(edges) != 4 {
		t.Fatalf("expected 4 edges, got %d", len(edges))
	}

	var hasSession, hasAgent, hasEvt1, hasEvt2 bool
	for _, e := range edges {
		switch {
		case e.EdgeType == string(EdgeTypeBelongsToSession) && e.DstObjectID == "sess_1":
			hasSession = true
			if e.SrcType != string(ObjectTypeMemory) || e.DstType != string(ObjectTypeSession) {
				t.Fatalf("unexpected session edge types: %+v", e)
			}
		case e.EdgeType == string(EdgeTypeOwnedByAgent) && e.DstObjectID == "agent_1":
			hasAgent = true
			if e.SrcType != string(ObjectTypeMemory) || e.DstType != string(ObjectTypeAgent) {
				t.Fatalf("unexpected agent edge types: %+v", e)
			}
		case e.EdgeType == string(EdgeTypeDerivedFrom) && e.DstObjectID == "evt_1":
			hasEvt1 = true
			if e.ProvenanceRef != "evt_1" {
				t.Fatalf("expected provenance_ref evt_1, got %s", e.ProvenanceRef)
			}
		case e.EdgeType == string(EdgeTypeDerivedFrom) && e.DstObjectID == "evt_2":
			hasEvt2 = true
		}
	}

	if !hasSession || !hasAgent || !hasEvt1 || !hasEvt2 {
		t.Fatalf("missing expected memory edges: %+v", edges)
	}
}

func TestBuildArtifactBaseEdges(t *testing.T) {
	a := Artifact{
		ArtifactID:        "art_1",
		SessionID:         "sess_1",
		OwnerAgentID:      "agent_1",
		ProducedByEventID: "evt_1",
	}

	edges := BuildArtifactBaseEdges(a)

	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(edges))
	}

	var hasSession, hasAgent, hasProduced bool
	for _, e := range edges {
		switch {
		case e.EdgeType == string(EdgeTypeBelongsToSession) && e.DstObjectID == "sess_1":
			hasSession = true
		case e.EdgeType == string(EdgeTypeOwnedByAgent) && e.DstObjectID == "agent_1":
			hasAgent = true
		case e.EdgeType == string(EdgeTypeToolProduces) && e.DstObjectID == "evt_1":
			hasProduced = true
			if e.SrcType != string(ObjectTypeArtifact) || e.DstType != string(ObjectTypeEvent) {
				t.Fatalf("unexpected produced-by edge types: %+v", e)
			}
		}
	}

	if !hasSession || !hasAgent || !hasProduced {
		t.Fatalf("missing expected artifact edges: %+v", edges)
	}
}

func TestBuildEventBaseEdges(t *testing.T) {
	e := Event{
		EventID:       "evt_2",
		AgentID:       "agent_1",
		SessionID:     "sess_1",
		ParentEventID: "evt_parent",
		CausalRefs:    []string{"evt_ref_1", "evt_ref_2"},
	}

	edges := BuildEventBaseEdges(e)

	if len(edges) != 5 {
		t.Fatalf("expected 5 edges, got %d", len(edges))
	}

	var hasSession, hasAgent, hasParent, hasRef1, hasRef2 bool
	for _, edge := range edges {
		switch {
		case edge.EdgeType == string(EdgeTypeBelongsToSession) && edge.DstObjectID == "sess_1":
			hasSession = true
		case edge.EdgeType == string(EdgeTypeOwnedByAgent) && edge.DstObjectID == "agent_1":
			hasAgent = true
		case edge.EdgeType == string(EdgeTypeCausedBy) && edge.DstObjectID == "evt_parent":
			hasParent = true
			if edge.Weight != DefaultCausalWeight {
				t.Fatalf("expected DefaultCausalWeight, got %v", edge.Weight)
			}
		case edge.EdgeType == string(EdgeTypeCausedBy) && edge.DstObjectID == "evt_ref_1":
			hasRef1 = true
		case edge.EdgeType == string(EdgeTypeCausedBy) && edge.DstObjectID == "evt_ref_2":
			hasRef2 = true
		}
	}

	if !hasSession || !hasAgent || !hasParent || !hasRef1 || !hasRef2 {
		t.Fatalf("missing expected event edges: %+v", edges)
	}
}

func TestBuildBaseEdges_IgnoreEmptyFields(t *testing.T) {
	m := Memory{MemoryID: "mem_empty"}
	a := Artifact{ArtifactID: "art_empty"}
	e := Event{EventID: "evt_empty"}

	if got := len(BuildMemoryBaseEdges(m)); got != 0 {
		t.Fatalf("expected 0 memory edges, got %d", got)
	}
	if got := len(BuildArtifactBaseEdges(a)); got != 0 {
		t.Fatalf("expected 0 artifact edges, got %d", got)
	}
	if got := len(BuildEventBaseEdges(e)); got != 0 {
		t.Fatalf("expected 0 event edges, got %d", got)
	}
}
