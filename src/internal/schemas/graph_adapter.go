package schemas

func MemoryToGraphNode(m Memory) GraphNode {
	props := map[string]any{
		"memory_type":      m.MemoryType,
		"agent_id":         m.AgentID,
		"session_id":       m.SessionID,
		"scope":            m.Scope,
		"level":            m.Level,
		"content":          m.Content,
		"summary":          m.Summary,
		"source_event_ids": m.SourceEventIDs,
		"confidence":       m.Confidence,
		"importance":       m.Importance,
		"freshness_score":  m.FreshnessScore,
		"ttl":              m.TTL,
		"valid_from":       m.ValidFrom,
		"valid_to":         m.ValidTo,
		"provenance_ref":   m.ProvenanceRef,
		"version":          m.Version,
		"is_active":        m.IsActive,
		"join_key":         "mem:" + m.MemoryID,
	}

	label := m.Summary
	if label == "" {
		label = m.Content
	}
	if label == "" {
		label = m.MemoryType
	}

	return GraphNode{
		ObjectID:   m.MemoryID,
		ObjectType: "memory",
		Label:      label,
		Properties: props,
	}
}

func EventToGraphNode(e Event) GraphNode {
	props := map[string]any{
		"tenant_id":       e.TenantID,
		"workspace_id":    e.WorkspaceID,
		"agent_id":        e.AgentID,
		"session_id":      e.SessionID,
		"event_type":      e.EventType,
		"event_time":      e.EventTime,
		"ingest_time":     e.IngestTime,
		"visible_time":    e.VisibleTime,
		"logical_ts":      e.LogicalTS,
		"parent_event_id": e.ParentEventID,
		"causal_refs":     e.CausalRefs,
		"payload":         e.Payload,
		"source":          e.Source,
		"importance":      e.Importance,
		"visibility":      e.Visibility,
		"version":         e.Version,
		"join_key":        "evt:" + e.EventID,
	}

	label := e.EventType
	if label == "" {
		label = "event"
	}

	return GraphNode{
		ObjectID:   e.EventID,
		ObjectType: "event",
		Label:      label,
		Properties: props,
	}
}

func ArtifactToGraphNode(a Artifact) GraphNode {
	props := map[string]any{
		"session_id":           a.SessionID,
		"owner_agent_id":       a.OwnerAgentID,
		"artifact_type":        a.ArtifactType,
		"uri":                  a.URI,
		"content_ref":          a.ContentRef,
		"mime_type":            a.MimeType,
		"metadata":             a.Metadata,
		"hash":                 a.Hash,
		"produced_by_event_id": a.ProducedByEventID,
		"version":              a.Version,
		"join_key":             "art:" + a.ArtifactID,
	}

	label := a.ArtifactType
	if label == "" {
		label = a.MimeType
	}
	if label == "" {
		label = "artifact"
	}

	return GraphNode{
		ObjectID:   a.ArtifactID,
		ObjectType: "artifact",
		Label:      label,
		Properties: props,
	}
}
