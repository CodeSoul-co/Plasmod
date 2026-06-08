package schemas

import "fmt"

func BuildMemoryBaseEdges(m Memory) []Edge {
	edges := make([]Edge, 0, 4)

	if m.SessionID != "" {
		edges = append(edges, Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, m.MemoryID, EdgeTypeBelongsToSession, m.SessionID),
			SrcObjectID:   m.MemoryID,
			SrcType:       string(ObjectTypeMemory),
			EdgeType:      string(EdgeTypeBelongsToSession),
			DstObjectID:   m.SessionID,
			DstType:       string(ObjectTypeSession),
			Weight:        DefaultEdgeWeight,
			ProvenanceRef: m.ProvenanceRef,
			Properties: map[string]any{
				"src_join_key": "mem:" + m.MemoryID,
				"dst_join_key": "sess:" + m.SessionID,
			},
		})
	}

	if m.AgentID != "" {
		edges = append(edges, Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, m.MemoryID, EdgeTypeOwnedByAgent, m.AgentID),
			SrcObjectID:   m.MemoryID,
			SrcType:       string(ObjectTypeMemory),
			EdgeType:      string(EdgeTypeOwnedByAgent),
			DstObjectID:   m.AgentID,
			DstType:       string(ObjectTypeAgent),
			Weight:        DefaultEdgeWeight,
			ProvenanceRef: m.ProvenanceRef,
			Properties: map[string]any{
				"src_join_key": "mem:" + m.MemoryID,
				"dst_join_key": "agent:" + m.AgentID,
			},
		})
	}

	for _, eid := range m.SourceEventIDs {
		if eid == "" {
			continue
		}
		edges = append(edges, Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, m.MemoryID, EdgeTypeDerivedFrom, eid),
			SrcObjectID:   m.MemoryID,
			SrcType:       string(ObjectTypeMemory),
			EdgeType:      string(EdgeTypeDerivedFrom),
			DstObjectID:   eid,
			DstType:       string(ObjectTypeEvent),
			Weight:        DefaultEdgeWeight,
			ProvenanceRef: m.ProvenanceRef,
			Properties: map[string]any{
				"src_join_key": "mem:" + m.MemoryID,
				"dst_join_key": "evt:" + eid,
			},
		})
	}

	return edges
}

func BuildArtifactBaseEdges(a Artifact) []Edge {
	edges := make([]Edge, 0, 3)

	if a.SessionID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, a.ArtifactID, EdgeTypeBelongsToSession, a.SessionID),
			SrcObjectID: a.ArtifactID,
			SrcType:     string(ObjectTypeArtifact),
			EdgeType:    string(EdgeTypeBelongsToSession),
			DstObjectID: a.SessionID,
			DstType:     string(ObjectTypeSession),
			Weight:      DefaultEdgeWeight,
		})
	}

	if a.OwnerAgentID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, a.ArtifactID, EdgeTypeOwnedByAgent, a.OwnerAgentID),
			SrcObjectID: a.ArtifactID,
			SrcType:     string(ObjectTypeArtifact),
			EdgeType:    string(EdgeTypeOwnedByAgent),
			DstObjectID: a.OwnerAgentID,
			DstType:     string(ObjectTypeAgent),
			Weight:      DefaultEdgeWeight,
		})
	}

	if a.ProducedByEventID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, a.ArtifactID, EdgeTypeToolProduces, a.ProducedByEventID),
			SrcObjectID: a.ArtifactID,
			SrcType:     string(ObjectTypeArtifact),
			EdgeType:    string(EdgeTypeToolProduces),
			DstObjectID: a.ProducedByEventID,
			DstType:     string(ObjectTypeEvent),
			Weight:      DefaultEdgeWeight,
		})
	}

	return edges
}

func BuildEventBaseEdges(e Event) []Edge {
	e = e.NormalizeDynamicEventV04()
	edges := make([]Edge, 0, 4)

	if e.Actor.SessionID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, e.Identity.EventID, EdgeTypeBelongsToSession, e.Actor.SessionID),
			SrcObjectID: e.Identity.EventID,
			SrcType:     string(ObjectTypeEvent),
			EdgeType:    string(EdgeTypeBelongsToSession),
			DstObjectID: e.Actor.SessionID,
			DstType:     string(ObjectTypeSession),
			Weight:      DefaultEdgeWeight,
		})
	}

	if e.Actor.AgentID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, e.Identity.EventID, EdgeTypeOwnedByAgent, e.Actor.AgentID),
			SrcObjectID: e.Identity.EventID,
			SrcType:     string(ObjectTypeEvent),
			EdgeType:    string(EdgeTypeOwnedByAgent),
			DstObjectID: e.Actor.AgentID,
			DstType:     string(ObjectTypeAgent),
			Weight:      DefaultEdgeWeight,
		})
	}

	if e.Causality.ParentEventID != "" {
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, e.Identity.EventID, EdgeTypeCausedBy, e.Causality.ParentEventID),
			SrcObjectID: e.Identity.EventID,
			SrcType:     string(ObjectTypeEvent),
			EdgeType:    string(EdgeTypeCausedBy),
			DstObjectID: e.Causality.ParentEventID,
			DstType:     string(ObjectTypeEvent),
			Weight:      DefaultCausalWeight,
		})
	}

	for _, ref := range e.Causality.CausalRefs {
		if ref == "" {
			continue
		}
		edges = append(edges, Edge{
			EdgeID:      fmt.Sprintf("%s%s_%s_%s", IDPrefixEdge, e.Identity.EventID, EdgeTypeCausedBy, ref),
			SrcObjectID: e.Identity.EventID,
			SrcType:     string(ObjectTypeEvent),
			EdgeType:    string(EdgeTypeCausedBy),
			DstObjectID: ref,
			DstType:     string(ObjectTypeEvent),
			Weight:      DefaultCausalWeight,
		})
	}

	return edges
}
