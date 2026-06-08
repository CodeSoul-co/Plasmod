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
	if m.DatasetName != "" {
		props["dataset_name"] = m.DatasetName
	}
	if m.SourceFileName != "" {
		props["source_file_name"] = m.SourceFileName
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
	e = e.NormalizeDynamicEventV04()
	props := map[string]any{
		"tenant_id":       e.Identity.TenantID,
		"workspace_id":    e.Identity.WorkspaceID,
		"agent_id":        e.Actor.AgentID,
		"session_id":      e.Actor.SessionID,
		"event_type":      e.EventInfo.EventType,
		"event_time":      e.Time.EventTime,
		"ingest_time":     e.Time.IngestTime,
		"visible_time":    e.Time.VisibleTime,
		"logical_ts":      e.Time.LogicalTS,
		"parent_event_id": e.Causality.ParentEventID,
		"causal_refs":     e.Causality.CausalRefs,
		"payload":         e.Payload,
		"source":          e.Identity.Source,
		"importance":      resolveGraphEventImportance(e),
		"visibility":      e.Access.Visibility,
		"version":         e.Version,
		"join_key":        "evt:" + e.Identity.EventID,
	}

	label := e.EventInfo.EventType
	if label == "" {
		label = "event"
	}

	return GraphNode{
		ObjectID:   e.Identity.EventID,
		ObjectType: "event",
		Label:      label,
		Properties: props,
	}
}

func resolveGraphEventImportance(e Event) float64 {
	if e.EventInfo.Importance != nil {
		return *e.EventInfo.Importance
	}
	return 0
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
