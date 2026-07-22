package worker

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func capabilityEvent(id string) schemas.Event {
	return schemas.Event{
		EventID: id, TenantID: "tenant-cap", WorkspaceID: "workspace-cap",
		AgentID: "agent-cap", SessionID: "session-cap", EventType: "artifact",
		Payload: map[string]any{
			"text": "capability profile event", "artifact_uri": "s3://bucket/report.txt",
		},
	}
}

func TestRuntimeMaterializationProfilesMutateCanonicalState(t *testing.T) {
	tests := []struct {
		profile      string
		wantMemory   bool
		wantState    bool
		wantArtifact bool
		wantEdges    bool
		wantVersions bool
	}{
		{"full", true, true, true, true, true},
		{"none", false, false, false, false, false},
		{"memory_only", true, false, false, false, true},
		{"no_state", true, false, true, true, true},
		{"no_artifact", true, true, false, true, true},
		{"no_edge", true, true, true, false, true},
		{"no_version", true, true, true, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.profile, func(t *testing.T) {
			runtime := buildTestRuntime(t)
			cfg := schemas.DefaultRuntimeCapabilities()
			cfg.MaterializationProfile = tc.profile
			runtime.ConfigureCapabilities(cfg)
			ack, err := runtime.SubmitIngest(capabilityEvent("event-" + tc.profile))
			if err != nil {
				t.Fatal(err)
			}
			memoryID, _ := ack["memory_id"].(string)
			_, memoryFound := runtime.storage.Objects().GetMemory("mem_event-" + tc.profile)
			if memoryFound != tc.wantMemory {
				t.Fatalf("memory found=%t want=%t ack=%v", memoryFound, tc.wantMemory, ack)
			}
			if (ack["state_id"] != nil) != tc.wantState {
				t.Fatalf("state ack mismatch: %v", ack)
			}
			if (ack["artifact_id"] != nil) != tc.wantArtifact {
				t.Fatalf("artifact ack mismatch: %v", ack)
			}
			if (len(runtime.storage.Edges().ListEdges()) > 0) != tc.wantEdges {
				t.Fatalf("edge presence mismatch: %d", len(runtime.storage.Edges().ListEdges()))
			}
			if tc.wantMemory && (len(runtime.storage.Versions().GetVersions(memoryID)) > 0) != tc.wantVersions {
				t.Fatalf("version presence mismatch for %s", memoryID)
			}
			if tc.profile == "none" {
				response := runtime.ExecuteQuery(schemas.QueryRequest{
					QueryText: "capability profile event", QueryScope: "workspace-cap",
					WorkspaceID: "workspace-cap", AgentID: "agent-cap", TopK: 5,
				})
				if len(response.Objects) != 1 || response.Objects[0] != "event-none" {
					t.Fatalf("no-materialization did not expose the flat Event projection: %v", response.Objects)
				}
			}
		})
	}
}

func TestRuntimeReplayRestoresStateWithoutAppendingWAL(t *testing.T) {
	runtime := buildTestRuntime(t)
	if _, err := runtime.SubmitIngest(capabilityEvent("replay-original")); err != nil {
		t.Fatal(err)
	}
	latestBefore := runtime.wal.LatestLSN()
	before := runtime.RuntimeStateSummary()
	if _, err := runtime.AdminResetMaterialized(nil, schemas.DefaultAlgorithmConfig()); err != nil {
		t.Fatal(err)
	}
	if got := runtime.RuntimeStateSummary()["objects"].(int); got != 0 {
		t.Fatalf("reset left %d objects", got)
	}
	result, err := runtime.AdminReplayApply(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.wal.LatestLSN() != latestBefore || result["wal_appends_during_replay"] != int64(0) {
		t.Fatalf("replay appended WAL entries: before=%d after=%d result=%v", latestBefore, runtime.wal.LatestLSN(), result)
	}
	after := runtime.RuntimeStateSummary()
	if after["objects"] != before["objects"] || after["edges"] != before["edges"] {
		t.Fatalf("recovery mismatch before=%v after=%v", before, after)
	}
}

func TestRuntimeEvidenceNoneReturnsObjectsAndDiagnostics(t *testing.T) {
	runtime := buildTestRuntime(t)
	cfg := schemas.DefaultRuntimeCapabilities()
	cfg.EvidenceProfile = "none"
	runtime.ConfigureCapabilities(cfg)
	if _, err := runtime.SubmitIngest(capabilityEvent("evidence-none")); err != nil {
		t.Fatal(err)
	}
	response := runtime.ExecuteQuery(schemas.QueryRequest{
		QueryText: "capability profile", QueryScope: "workspace-cap",
		WorkspaceID: "workspace-cap", AgentID: "agent-cap", SessionID: "session-cap", TopK: 5,
	})
	if len(response.Objects) == 0 || len(response.Edges) != 0 || len(response.ProofTrace) != 0 || len(response.Provenance) != 0 {
		t.Fatalf("unexpected no-evidence response: %+v", response)
	}
	if response.Diagnostics == nil {
		t.Fatal("query diagnostics missing")
	}
}
