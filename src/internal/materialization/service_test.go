package materialization

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func TestService_MaterializeEvent_Basic(t *testing.T) {
	svc := NewService()

	ev := schemas.Event{
		EventID:     "evt_1",
		AgentID:     "agent_1",
		SessionID:   "sess_1",
		WorkspaceID: "ws_1",
		EventType:   "user_message",
		LogicalTS:   42,
		Payload:     map[string]any{"text": "hello from the agent"},
	}
	res := svc.MaterializeEvent(ev)

	if res.Record.ObjectID != "mem_evt_1" {
		t.Errorf("Record.ObjectID: want mem_evt_1, got %q", res.Record.ObjectID)
	}
	if res.Record.Text != "hello from the agent" {
		t.Errorf("Record.Text: want %q, got %q", "hello from the agent", res.Record.Text)
	}
	if res.Record.Namespace != "ws_1" {
		t.Errorf("Record.Namespace: want ws_1, got %q", res.Record.Namespace)
	}
	if res.Memory.MemoryID != "mem_evt_1" {
		t.Errorf("Memory.MemoryID: want mem_evt_1, got %q", res.Memory.MemoryID)
	}
	if res.Memory.MemoryType != "episodic" {
		t.Errorf("Memory.MemoryType: want episodic, got %q", res.Memory.MemoryType)
	}
	if !res.Memory.IsActive {
		t.Error("Memory.IsActive: should be true")
	}
	if res.Version.ObjectID != "mem_evt_1" {
		t.Errorf("Version.ObjectID: want mem_evt_1, got %q", res.Version.ObjectID)
	}
	if res.Version.MutationEventID != "evt_1" {
		t.Errorf("Version.MutationEventID: want evt_1, got %q", res.Version.MutationEventID)
	}
}

func TestService_MaterializeEvent_HookAttributes(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		SchemaVersion: schemas.DynamicEventSchemaV04,
		Identity:      schemas.EventIdentity{EventID: "evt_hooks", WorkspaceID: "ws_hooks"},
		Actor:         schemas.EventActor{AgentID: "agent_hooks", SessionID: "sess_hooks"},
		EventInfo:     schemas.EventDescriptor{EventType: "user_message"},
		Materialization: schemas.EventMaterialization{
			Hooks: schemas.EventHooks{Materializers: []string{"mat.custom"}},
		},
		Retrieval: schemas.EventRetrieval{
			IndexText: "hooked event",
			Hooks: schemas.EventHooks{
				Indexers: []string{"idx.custom"},
				QueryOps: []string{"query.custom"},
			},
		},
		Access: schemas.EventAccess{
			Hooks: schemas.EventHooks{Policy: []string{"policy.custom"}},
		},
		Causality: schemas.EventCausality{
			Hooks: schemas.EventHooks{Evidence: []string{"evidence.custom"}},
		},
		Extensions: schemas.EventExtensions{
			Hooks: schemas.EventHooks{
				Chains: []string{"chain.custom"},
				Custom: []string{"custom.hook"},
			},
		},
	}

	res := svc.MaterializeEvent(ev)
	attrs := res.Record.Attributes
	for key, want := range map[string]string{
		"hook_materializers": "mat.custom",
		"hook_indexers":      "idx.custom",
		"hook_query_ops":     "query.custom",
		"hook_policy":        "policy.custom",
		"hook_evidence":      "evidence.custom",
		"hook_chains":        "chain.custom",
		"hook_custom":        "custom.hook",
	} {
		if got := attrs[key]; got != want {
			t.Fatalf("attribute %s: want %q, got %q", key, want, got)
		}
	}
}

func TestService_MaterializeEvent_PayloadDatasetAndFileName(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:     "evt_ds",
		AgentID:     "agent_1",
		SessionID:   "sess_1",
		WorkspaceID: "ws_1",
		EventType:   "dataset_record",
		LogicalTS:   1,
		Payload: map[string]any{
			"text":            "dataset=f.bin dataset_name:DS row:0",
			"dataset":         "DS",
			"file_name":       "f.bin",
			"import_batch_id": "batch_20260413T120000Z",
		},
	}
	res := svc.MaterializeEvent(ev)
	if res.Memory.DatasetName != "DS" {
		t.Errorf("Memory.DatasetName: want DS, got %q", res.Memory.DatasetName)
	}
	if res.Memory.SourceFileName != "f.bin" {
		t.Errorf("Memory.SourceFileName: want f.bin, got %q", res.Memory.SourceFileName)
	}
	if res.Memory.ImportBatchID != "batch_20260413T120000Z" {
		t.Errorf("Memory.ImportBatchID: want batch_20260413T120000Z, got %q", res.Memory.ImportBatchID)
	}
}

func TestService_MaterializeEvent_DynamicEventV04(t *testing.T) {
	svc := NewService()
	confidence := 0.91
	ttl := int64(60_000)
	ev := schemas.Event{
		SchemaVersion: schemas.DynamicEventSchemaV04,
		Identity: schemas.EventIdentity{
			EventID:       "evt_v04",
			TenantID:      "tenant_1",
			WorkspaceID:   "workspace_1",
			Source:        "synthetic_stream",
			Dataset:       "dataset_1",
			ImportBatchID: "batch_1",
			FileName:      "trace.jsonl",
		},
		Actor: schemas.EventActor{
			AgentID:   "agent_1",
			SessionID: "session_1",
		},
		Time: schemas.EventTime{LogicalTS: 99},
		EventInfo: schemas.EventDescriptor{
			EventType:  string(schemas.EventTypeToolResult),
			Confidence: &confidence,
		},
		Object: schemas.EventObject{
			ObjectType:   string(schemas.ObjectTypeArtifact),
			ArtifactName: "result.json",
		},
		Causality: schemas.EventCausality{
			SourceObjectID: "mem_source",
			TargetObjectID: "mem_target",
			EdgeKind:       string(schemas.EdgeTypeSupports),
			Reason:         "tool evidence",
		},
		Access: schemas.EventAccess{
			Consistency: "bounded",
			Visibility:  "workspace",
			TTLMS:       &ttl,
			PolicyTags:  []string{"synthetic"},
		},
		Retrieval: schemas.EventRetrieval{
			IndexText:          "tool result text",
			EmbeddingRef:       "embedding://evt_v04",
			RetrievalNamespace: "workspace_1/session_1",
		},
		Payload: map[string]any{
			"artifact": map[string]any{
				"uri":       "s3://bucket/result.json",
				"mime_type": "application/json",
			},
		},
	}
	res := svc.MaterializeEvent(ev)
	if res.Record.ObjectID != "mem_evt_v04" || res.Record.Text != "tool result text" {
		t.Fatalf("unexpected record: %+v", res.Record)
	}
	if res.Record.Namespace != "workspace_1/session_1" {
		t.Fatalf("namespace should use retrieval namespace, got %q", res.Record.Namespace)
	}
	if res.Memory.DatasetName != "dataset_1" || res.Memory.ImportBatchID != "batch_1" || res.Memory.SourceFileName != "trace.jsonl" {
		t.Fatalf("identity fields not copied to memory: %+v", res.Memory)
	}
	if res.Memory.Confidence != confidence {
		t.Fatalf("confidence: got %f want %f", res.Memory.Confidence, confidence)
	}
	if len(res.Memory.PolicyTags) != 1 || res.Memory.PolicyTags[0] != "synthetic" {
		t.Fatalf("policy tags not copied: %+v", res.Memory.PolicyTags)
	}
	if res.Memory.EmbeddingRef != "embedding://evt_v04" {
		t.Fatalf("embedding ref not copied: %q", res.Memory.EmbeddingRef)
	}
	if res.Artifact == nil || res.Artifact.URI != "s3://bucket/result.json" || res.Artifact.MimeType != "application/json" {
		t.Fatalf("artifact not derived from v0.4 payload: %+v", res.Artifact)
	}
	if res.Record.Attributes["payload_size_bytes"] == "" || res.Record.Attributes["payload_hash"] == "" {
		t.Fatalf("data accounting attributes missing: %+v", res.Record.Attributes)
	}
	foundExplicitEdge := false
	for _, edge := range res.Edges {
		if edge.SrcObjectID == "mem_source" && edge.DstObjectID == "mem_target" && edge.EdgeType == string(schemas.EdgeTypeSupports) {
			foundExplicitEdge = true
		}
	}
	if !foundExplicitEdge {
		t.Fatalf("explicit causality edge not materialized: %+v", res.Edges)
	}
}

func TestService_MaterializeEvent_SkipVectorIndexWhenEmbeddingDisabled(t *testing.T) {
	svc := NewService()
	res := svc.MaterializeEvent(schemas.Event{
		SchemaVersion: schemas.DynamicEventSchemaV04,
		Identity:      schemas.EventIdentity{EventID: "evt_no_embedding", WorkspaceID: "ws_1"},
		Actor:         schemas.EventActor{AgentID: "agent_1", SessionID: "sess_1"},
		EventInfo:     schemas.EventDescriptor{EventType: "observation"},
		Retrieval: schemas.EventRetrieval{
			IndexText:    "object-visible event without embedding",
			HasEmbedding: false,
		},
	})
	if !res.Record.SkipVectorIndex {
		t.Fatal("expected vector index projection to be skipped")
	}

	res = svc.MaterializeEvent(schemas.Event{
		EventID:   "evt_legacy",
		AgentID:   "agent_1",
		SessionID: "sess_1",
		Payload:   map[string]any{"text": "legacy event should keep prior indexing behavior"},
	})
	if res.Record.SkipVectorIndex {
		t.Fatal("legacy event without retrieval.index_text should not skip vector index")
	}

	res = svc.MaterializeEvent(schemas.Event{
		SchemaVersion: schemas.DynamicEventSchemaV04,
		Identity:      schemas.EventIdentity{EventID: "evt_with_vector", WorkspaceID: "ws_1"},
		Actor:         schemas.EventActor{AgentID: "agent_1", SessionID: "sess_1"},
		EventInfo:     schemas.EventDescriptor{EventType: "observation"},
		Retrieval: schemas.EventRetrieval{
			IndexText:       "externally embedded event",
			EmbeddingVector: []float32{0.1, 0.2, 0.3},
		},
	})
	if res.Record.SkipVectorIndex {
		t.Fatal("event with explicit embedding vector should not skip vector index")
	}
}

func TestService_MaterializeEvent_SkipVectorIndexEnvOverride(t *testing.T) {
	t.Setenv("PLASMOD_SKIP_VECTOR_INDEX", "1")
	svc := NewService()
	res := svc.MaterializeEvent(schemas.Event{
		SchemaVersion: schemas.DynamicEventSchemaV04,
		Identity:      schemas.EventIdentity{EventID: "evt_skip_env", WorkspaceID: "ws_1"},
		Actor:         schemas.EventActor{AgentID: "agent_1", SessionID: "sess_1"},
		EventInfo:     schemas.EventDescriptor{EventType: "memory"},
		Retrieval: schemas.EventRetrieval{
			IndexText:       "event with explicit vector but global indexing disabled",
			HasEmbedding:    true,
			EmbeddingVector: []float32{0.1, 0.2, 0.3},
		},
	})
	if !res.Record.SkipVectorIndex {
		t.Fatal("PLASMOD_SKIP_VECTOR_INDEX should override per-event embedding settings")
	}
}

func TestService_MaterializeEvent_EdgeDerivation(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:    "evt_2",
		AgentID:    "agent_2",
		SessionID:  "sess_2",
		EventType:  "tool_result_returned",
		CausalRefs: []string{"evt_1"},
	}
	res := svc.MaterializeEvent(ev)

	if len(res.Edges) < 5 {
		t.Errorf("Expected at least 5 edges (event+session+agent+causal+state relations), got %d", len(res.Edges))
	}

	edgeTypes := map[string]bool{}
	for _, e := range res.Edges {
		edgeTypes[e.EdgeType] = true
	}
	for _, want := range []string{"caused_by", "belongs_to_session", "owned_by_agent", "derived_from", "projected_from"} {
		if !edgeTypes[want] {
			t.Errorf("Missing edge type: %q", want)
		}
	}
	for _, e := range res.Edges {
		if e.ProvenanceRef != ev.EventID {
			t.Errorf("edge %s provenance_ref: want %q, got %q", e.EdgeID, ev.EventID, e.ProvenanceRef)
		}
	}
}

func TestService_MaterializeEvent_StateAndArtifact(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:     "evt_state_1",
		AgentID:     "agent_1",
		SessionID:   "sess_1",
		WorkspaceID: "ws_1",
		EventType:   "user_message",
		LogicalTS:   7,
		Payload: map[string]any{
			"text":          "hello",
			"artifact_uri":  "s3://bucket/obj1.bin",
			"mime_type":     "application/octet-stream",
			"artifact_name": "weights.pt",
		},
	}
	res := svc.MaterializeEvent(ev)
	if res.State == nil || res.StateVersion == nil {
		t.Fatal("expected non-nil State and StateVersion")
	}
	if res.State.StateValue != res.Memory.MemoryID {
		t.Errorf("State.StateValue should point to memory id, got %q want %q", res.State.StateValue, res.Memory.MemoryID)
	}
	if res.Artifact == nil || res.ArtifactVersion == nil {
		t.Fatal("expected non-nil Artifact when artifact_uri is set")
	}
	if res.Artifact.URI != "s3://bucket/obj1.bin" {
		t.Errorf("Artifact.URI: %q", res.Artifact.URI)
	}
	if res.Artifact.Metadata["name"] != "weights.pt" {
		t.Errorf("artifact_name in metadata: %#v", res.Artifact.Metadata)
	}
	edgeTypes := map[string]bool{}
	for _, e := range res.Edges {
		edgeTypes[e.EdgeType] = true
		if e.ProvenanceRef != ev.EventID {
			t.Errorf("edge %s provenance_ref: want %q, got %q", e.EdgeID, ev.EventID, e.ProvenanceRef)
		}
	}
	for _, want := range []string{"created_by", "grounded_on_resource", "projected_from"} {
		if !edgeTypes[want] {
			t.Errorf("missing relation edge type: %s", want)
		}
	}
}

func TestService_MaterializeEvent_NoArtifactWithoutURI(t *testing.T) {
	svc := NewService()
	ev := schemas.Event{
		EventID:   "evt_plain",
		AgentID:   "a",
		SessionID: "s",
		EventType: "user_message",
		LogicalTS: 1,
		Payload:   map[string]any{"text": "only text"},
	}
	res := svc.MaterializeEvent(ev)
	if res.State == nil {
		t.Fatal("expected State")
	}
	if res.Artifact != nil {
		t.Fatal("expected no Artifact without uri in payload")
	}
}

func TestResolveMemoryType(t *testing.T) {
	cases := []struct {
		eventType  string
		wantMemory string
	}{
		{"user_message", "episodic"},
		{"assistant_message", "episodic"},
		{"critique_generated", "reflective"},
		{"plan_updated", "procedural"},
		{"tool_result_returned", "factual"},
		{"unknown_type", "episodic"},
	}
	for _, tc := range cases {
		ev := schemas.Event{EventInfo: schemas.EventDescriptor{EventType: tc.eventType}}
		got := resolveMemoryType(ev)
		if got != tc.wantMemory {
			t.Errorf("resolveMemoryType(%q): want %q, got %q", tc.eventType, tc.wantMemory, got)
		}
	}
}
