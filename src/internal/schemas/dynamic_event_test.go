package schemas

import (
	"encoding/json"
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
