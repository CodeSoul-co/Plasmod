package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDynamicEventV04Normalize(t *testing.T) {
	raw := []byte(`{
		"schema_version": "plasmod.dynamic_event.v0.4",
		"identity": {
			"trace_id": "trace_1",
			"event_id": "evt_1",
			"tenant_id": "tenant_1",
			"workspace_id": "workspace_1",
			"source": "synthetic_stream",
			"dataset": "deep10M",
			"import_batch_id": "batch_1",
			"file_name": "events.jsonl"
		},
		"actor": {
			"session_id": "session_1",
			"agent_id": "agent_1",
			"role_profile": "planner"
		},
		"time": {
			"event_time": 1710000000123,
			"logical_ts": 42
		},
		"event": {
			"event_type": "state_update",
			"event_subtype": "todo_update",
			"importance": 0.7,
			"confidence": 0.9
		},
		"object": {
			"object_type": "agent_state",
			"state_type": "todo_state",
			"state_key": "todo.open"
		},
		"causality": {
			"parent_event_id": "evt_0",
			"causal_refs": ["evt_0"]
		},
		"access": {
			"consistency": "bounded",
			"visibility": "workspace",
			"policy_tags": ["synthetic"]
		},
		"retrieval": {
			"index_text": "update todo state",
			"retrieval_namespace": "workspace_1/session_1"
		},
		"payload": {
			"state": {
				"value": "open"
			}
		},
		"extensions": {
			"labels": ["fixture"]
		}
	}`)
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal dynamic event: %v", err)
	}
	ev = ev.NormalizeDynamicEventV04()

	if ev.EventID != "evt_1" || ev.AgentID != "agent_1" || ev.SessionID != "session_1" {
		t.Fatalf("legacy identity/actor fields not normalized: %+v", ev)
	}
	if ev.EventType != "state_update" || ev.Importance != 0.7 {
		t.Fatalf("event descriptor not normalized: type=%q importance=%f", ev.EventType, ev.Importance)
	}
	if ev.LogicalTS != 42 || ev.EventTime == "" {
		t.Fatalf("time fields not normalized: logical=%d event_time=%q", ev.LogicalTS, ev.EventTime)
	}
	if ev.StateKey() != "todo.open" || ev.StateValueString() != "open" {
		t.Fatalf("state payload helpers failed: key=%q value=%q", ev.StateKey(), ev.StateValueString())
	}
	if ev.Text() != "update todo state" {
		t.Fatalf("retrieval index_text should drive Text(), got %q", ev.Text())
	}
	if ev.Payload[PayloadKeyDataset] != "deep10M" ||
		ev.Payload[PayloadKeyImportBatchID] != "batch_1" ||
		ev.Payload[PayloadKeyFileName] != "events.jsonl" {
		t.Fatalf("identity payload hints missing: %+v", ev.Payload)
	}
	if ev.Data.PayloadSizeBytes == 0 || ev.Data.PayloadHash == "" {
		t.Fatalf("data accounting not populated: %+v", ev.Data)
	}
}

func TestNormalizeObjectTypeName(t *testing.T) {
	if got := NormalizeObjectTypeName("state"); got != string(ObjectTypeAgentState) {
		t.Fatalf("state should normalize to agent_state, got %q", got)
	}
	if got := NormalizeObjectTypeName("artifact"); got != "artifact" {
		t.Fatalf("artifact should stay artifact, got %q", got)
	}
}

func TestEventMarshalEmitsCanonicalV04Only(t *testing.T) {
	ev := Event{
		EventID:   "evt_legacy",
		AgentID:   "agent_legacy",
		SessionID: "session_legacy",
		EventType: "observation",
		Payload:   map[string]any{PayloadKeyText: "legacy text"},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	body := string(data)
	for _, want := range []string{`"schema_version"`, `"identity"`, `"actor"`, `"event"`, `"payload"`, `"data"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("canonical event JSON missing %s: %s", want, body)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal marshaled event: %v", err)
	}
	if _, ok := doc["event_id"]; ok {
		t.Fatalf("legacy event_id should not be emitted at top-level: %s", body)
	}
	if !strings.Contains(body, `"identity":{"event_id":"evt_legacy"`) {
		t.Fatalf("event_id should be emitted inside identity: %s", body)
	}
}

func TestEventUnmarshalAcceptsLegacyFlatJSON(t *testing.T) {
	raw := []byte(`{
		"event_id": "evt_flat",
		"agent_id": "agent_flat",
		"session_id": "session_flat",
		"event_type": "observation",
		"payload": {"text": "flat text"}
	}`)
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal flat event: %v", err)
	}
	if ev.SchemaVersion != DynamicEventSchemaV04 {
		t.Fatalf("legacy event should normalize to v0.4, got %q", ev.SchemaVersion)
	}
	if ev.Identity.EventID != "evt_flat" || ev.Actor.AgentID != "agent_flat" || ev.EventInfo.EventType != "observation" {
		t.Fatalf("legacy fields not promoted: %+v", ev)
	}
	if ev.Text() != "flat text" {
		t.Fatalf("text helper failed: %q", ev.Text())
	}
}

func TestDynamicEventV04AcceptsProducerAliases(t *testing.T) {
	raw := []byte(`{
		"schema_version": "plasmod.dynamic_event.v0.4",
		"identity": {
			"trace_id": "trace_alias",
			"event_id": "evt_alias",
			"replay_order": 7
		},
		"actor": {
			"session_id": "session_alias",
			"agent_id": "agent_alias",
			"agent_kind": "tool_agent"
		},
		"time": {
			"event_time_ms": 1710000000123,
			"ingest_time_ms": 1710000001123,
			"visible_time_ms": 1710000002123,
			"logical_ts": 99
		},
		"event": {
			"event_type": "tool_result"
		},
		"access": {
			"consistency": "bounded",
			"sharing": "session"
		},
		"retrieval": {
			"index_text": "alias event text"
		},
		"payload": {}
	}`)
	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("unmarshal alias event: %v", err)
	}
	if ev.Identity.ReplayOrder != 7 {
		t.Fatalf("replay_order not retained: %+v", ev.Identity)
	}
	if ev.Actor.AgentType != "tool_agent" {
		t.Fatalf("agent_kind should normalize to agent_type, got %q", ev.Actor.AgentType)
	}
	if ev.Time.EventTime != 1710000000123 || ev.Time.IngestTime != 1710000001123 || ev.Time.VisibleTime != 1710000002123 {
		t.Fatalf("millisecond time aliases not normalized: %+v", ev.Time)
	}
	if ev.Access.Visibility != "session" || ev.Visibility != "session" {
		t.Fatalf("sharing should normalize to visibility, access=%q legacy=%q", ev.Access.Visibility, ev.Visibility)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal alias event: %v", err)
	}
	body := string(data)
	for _, forbidden := range []string{`"agent_kind"`, `"event_time_ms"`, `"ingest_time_ms"`, `"visible_time_ms"`, `"sharing"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("canonical JSON should not emit producer alias %s: %s", forbidden, body)
		}
	}
	for _, want := range []string{`"agent_type":"tool_agent"`, `"event_time":1710000000123`, `"visibility":"session"`, `"replay_order":7`} {
		if !strings.Contains(body, want) {
			t.Fatalf("canonical JSON missing normalized field %s: %s", want, body)
		}
	}
}

func TestEventHooksMergeModuleAndExtensionHooks(t *testing.T) {
	ev := Event{
		Materialization: EventMaterialization{
			Hooks: EventHooks{Materializers: []string{"mat.custom", "shared"}},
		},
		Retrieval: EventRetrieval{
			Hooks: EventHooks{
				Indexers: []string{"idx.custom"},
				QueryOps: []string{"query.rerank"},
				Custom:   []string{"retrieval.custom"},
			},
		},
		Access: EventAccess{
			Hooks: EventHooks{
				Policy: []string{"policy.custom"},
				Custom: []string{"access.custom"},
			},
		},
		Causality: EventCausality{
			Hooks: EventHooks{Evidence: []string{"evidence.expand"}},
		},
		Extensions: EventExtensions{
			Hooks: EventHooks{
				Materializers: []string{"shared", "mat.global"},
				Chains:        []string{"chain.reflect"},
				Custom:        []string{"global.custom"},
			},
		},
	}

	if got := strings.Join(ev.MaterializerHooks(), ","); got != "mat.custom,shared,mat.global" {
		t.Fatalf("materializer hooks not merged/deduped: %q", got)
	}
	if got := strings.Join(ev.IndexerHooks(), ","); got != "idx.custom" {
		t.Fatalf("indexer hooks not read: %q", got)
	}
	if got := strings.Join(ev.QueryOpHooks(), ","); got != "query.rerank" {
		t.Fatalf("query hooks not read: %q", got)
	}
	if got := strings.Join(ev.PolicyHooks(), ","); got != "policy.custom" {
		t.Fatalf("policy hooks not read: %q", got)
	}
	if got := strings.Join(ev.EvidenceHooks(), ","); got != "evidence.expand" {
		t.Fatalf("evidence hooks not read: %q", got)
	}
	if got := strings.Join(ev.ChainHooks(), ","); got != "chain.reflect" {
		t.Fatalf("chain hooks not read: %q", got)
	}
	if got := strings.Join(ev.CustomHooks(), ","); got != "access.custom,retrieval.custom,global.custom" {
		t.Fatalf("custom hooks not merged: %q", got)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal hooks event: %v", err)
	}
	body := string(data)
	for _, want := range []string{`"materialization"`, `"hooks"`, `"mat.custom"`, `"retrieval.custom"`, `"chain.reflect"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("canonical JSON missing hook marker %s: %s", want, body)
		}
	}
}
