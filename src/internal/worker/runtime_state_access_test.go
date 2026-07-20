package worker

import (
	"errors"
	"strings"
	"testing"

	"plasmod/src/internal/schemas"
)

func TestRuntimeStateMutationIsMonotonicAndReplayIdempotent(t *testing.T) {
	runtime := buildTestRuntime(t)
	events := []schemas.Event{
		stateMutationEvent("state-v1", "planning"),
		stateMutationEvent("state-v2", "executing"),
	}
	for _, event := range events {
		if _, err := runtime.SubmitIngest(event); err != nil {
			t.Fatalf("SubmitIngest(%s): %v", event.EventID, err)
		}
	}

	stateID := schemas.CanonicalStateID("tenant", "workspace", "agent", "session", "phase")
	state, ok := runtime.storage.Objects().GetState(stateID)
	if !ok {
		t.Fatalf("canonical state %q not found", stateID)
	}
	if state.Version != 2 || state.StateValue != "executing" || state.DerivedFromEventID != "state-v2" {
		t.Fatalf("unexpected latest state: %+v", state)
	}
	versions := runtime.storage.Versions().GetVersions(stateID)
	if len(versions) != 2 || versions[0].ValidTo == "" || len(versions[0].Snapshot) == 0 || len(versions[1].Snapshot) == 0 {
		t.Fatalf("state history is not recoverable: %+v", versions)
	}

	if _, err := runtime.SubmitIngest(events[0]); err != nil {
		t.Fatalf("replay old event: %v", err)
	}
	replayed, _ := runtime.storage.Objects().GetState(stateID)
	if replayed.Version != 2 || replayed.StateValue != "executing" || len(runtime.storage.Versions().GetVersions(stateID)) != 2 {
		t.Fatalf("old replay rolled canonical state backward: %+v", replayed)
	}
}

func TestRuntimeQueryEnforcesCanonicalAccessAndReturnsDecision(t *testing.T) {
	runtime := buildTestRuntime(t)
	event := schemas.Event{
		Identity:  schemas.EventIdentity{EventID: "access-event", TenantID: "tenant", WorkspaceID: "workspace"},
		Actor:     schemas.EventActor{AgentID: "owner", SessionID: "session"},
		EventInfo: schemas.EventDescriptor{EventType: string(schemas.EventTypeMemoryWriteRequested)},
		Access: schemas.EventAccess{
			Visibility:     string(schemas.VisibilityPrivate),
			VisibleToRoles: []string{"critic"},
		},
		Payload: map[string]any{"text": "private canonical memory"},
	}
	if _, err := runtime.SubmitIngest(event); err != nil {
		t.Fatalf("SubmitIngest: %v", err)
	}
	memoryID := "mem_access-event"

	denied := runtime.ExecuteQuery(schemas.QueryRequest{
		TargetObjectIDs: []string{memoryID}, ResponseMode: schemas.ResponseModeObjectsOnly,
		TenantID: "tenant", WorkspaceID: "workspace", RequesterAgentID: "reader",
	})
	if len(denied.Objects) != 0 || len(denied.AccessDecisions) != 0 {
		t.Fatalf("private object leaked to ungranted reader: %+v", denied)
	}

	allowed := runtime.ExecuteQuery(schemas.QueryRequest{
		TargetObjectIDs: []string{memoryID}, ResponseMode: schemas.ResponseModeObjectsOnly,
		TenantID: "tenant", WorkspaceID: "workspace", RequesterAgentID: "reader",
		RequesterRoles: []string{"critic"},
	})
	if len(allowed.Objects) != 1 || len(allowed.AccessDecisions) != 1 || allowed.AccessDecisions[0].Reason != "explicit_role_grant" {
		t.Fatalf("role grant was not enforced or explained: %+v", allowed)
	}
}

func TestRuntimeEvidenceDropsUnauthorizedGraphReferences(t *testing.T) {
	runtime := buildTestRuntime(t)
	publicID := "mem_public"
	secretID := "mem_secret"
	runtime.storage.Objects().PutMemory(schemas.Memory{
		MemoryID: publicID, AgentID: "owner", Content: "visible", IsActive: true,
		Access: schemas.CanonicalAccess{Visibility: string(schemas.VisibilityPublic)},
	})
	runtime.storage.Objects().PutMemory(schemas.Memory{
		MemoryID: secretID, AgentID: "owner", Content: "secret", IsActive: true,
		Access: schemas.CanonicalAccess{OwnerAgentID: "owner", Visibility: string(schemas.VisibilityPrivate)},
	})
	runtime.storage.Edges().PutEdge(schemas.Edge{
		EdgeID: "edge_public_secret", SrcObjectID: publicID, SrcType: "memory",
		DstObjectID: secretID, DstType: "memory", EdgeType: string(schemas.EdgeTypeSupports),
		Access: schemas.CanonicalAccess{Visibility: string(schemas.VisibilityPublic)},
	})

	response := runtime.ExecuteQuery(schemas.QueryRequest{
		TargetObjectIDs: []string{publicID}, RequesterAgentID: "reader",
		ResponseMode: schemas.ResponseModeStructuredEvidence,
	})
	if len(response.Objects) != 1 || response.Objects[0] != publicID {
		t.Fatalf("public seed missing: %+v", response.Objects)
	}
	for _, node := range response.Nodes {
		if node.ObjectID == secretID {
			t.Fatalf("private node leaked into evidence: %+v", node)
		}
	}
	for _, edge := range response.Edges {
		if edge.EdgeID == "edge_public_secret" || edge.SrcObjectID == secretID || edge.DstObjectID == secretID {
			t.Fatalf("edge disclosed private endpoint: %+v", edge)
		}
	}
	for _, step := range response.ProofTrace {
		if step.SourceID == secretID || step.TargetID == secretID || step.EdgeID == "edge_public_secret" {
			t.Fatalf("proof trace disclosed private reference: %+v", step)
		}
	}
	foundAccessProof := false
	for _, step := range response.ProofTrace {
		if step.Operation == "access_allowed" && strings.Contains(step.Description, "access:public") {
			foundAccessProof = true
		}
	}
	if len(response.AccessDecisions) != 1 || !foundAccessProof {
		t.Fatalf("allowed access was not explained: %+v", response)
	}
}

func TestRuntimeDispatchShareUsesWALDerivationAndContract(t *testing.T) {
	runtime := buildTestRuntime(t)
	if _, err := runtime.SubmitIngest(schemas.Event{
		Identity:  schemas.EventIdentity{EventID: "source-event", TenantID: "tenant", WorkspaceID: "workspace"},
		Actor:     schemas.EventActor{AgentID: "owner", SessionID: "session"},
		EventInfo: schemas.EventDescriptor{EventType: string(schemas.EventTypeMemoryWriteRequested)},
		Access:    schemas.EventAccess{Visibility: string(schemas.VisibilityPrivate)},
		Payload:   map[string]any{"text": "source memory", "memory_type": string(schemas.MemoryTypeFactual)},
	}); err != nil {
		t.Fatalf("SubmitIngest source: %v", err)
	}
	runtime.storage.Contracts().PutContract(schemas.ShareContract{
		ContractID: "contract", TenantID: "tenant", WorkspaceID: "workspace", Scope: "workspace",
		ReadAgents: []string{"reader"}, DeriveAgents: []string{"owner"},
	})

	sharedID, err := runtime.DispatchShareWithContract("owner", "reader", "mem_source-event", "contract")
	if err != nil {
		t.Fatalf("DispatchShareWithContract: %v", err)
	}
	shared, ok := runtime.storage.Objects().GetMemory(sharedID)
	if !ok {
		t.Fatalf("shared canonical memory %q not found", sharedID)
	}
	if shared.AgentID != "owner" || shared.Access.OwnerAgentID != "owner" || shared.Access.ShareContractID != "contract" ||
		len(shared.Access.VisibleToAgents) != 1 || shared.Access.VisibleToAgents[0] != "reader" {
		t.Fatalf("shared ownership/access is incorrect: %+v", shared)
	}
	if len(shared.SourceEventIDs) != 1 {
		t.Fatalf("shared memory omitted derivation event: %+v", shared.SourceEventIDs)
	}
	shareEvent, ok := runtime.storage.Objects().GetEvent(shared.SourceEventIDs[0])
	if !ok || shareEvent.Time.WalLSN == 0 || shareEvent.Causality.SourceObjectID != "mem_source-event" {
		t.Fatalf("share did not persist a causal WAL event: %+v", shareEvent)
	}
	versions := runtime.storage.Versions().GetVersions(sharedID)
	if len(versions) != 1 || len(versions[0].Snapshot) == 0 || versions[0].MutationLSN != shareEvent.Time.WalLSN {
		t.Fatalf("shared object version is not recoverable: %+v", versions)
	}
	foundDerivedEdge := false
	for _, edge := range runtime.storage.Edges().ListEdges() {
		if edge.SrcObjectID == "mem_source-event" && edge.DstObjectID == sharedID && edge.EdgeType == string(schemas.EdgeTypeDerivedFrom) {
			foundDerivedEdge = true
		}
	}
	if !foundDerivedEdge {
		t.Fatal("share derivation edge was not persisted")
	}

	reader := runtime.ExecuteQuery(schemas.QueryRequest{
		TargetObjectIDs: []string{sharedID}, ResponseMode: schemas.ResponseModeObjectsOnly,
		TenantID: "tenant", WorkspaceID: "workspace", RequesterAgentID: "reader",
	})
	if len(reader.Objects) != 1 || len(reader.AccessDecisions) != 1 || reader.AccessDecisions[0].Reason != "share_contract" {
		t.Fatalf("contract reader could not retrieve shared memory: %+v", reader)
	}
	stranger := runtime.ExecuteQuery(schemas.QueryRequest{
		TargetObjectIDs: []string{sharedID}, ResponseMode: schemas.ResponseModeObjectsOnly,
		TenantID: "tenant", WorkspaceID: "workspace", RequesterAgentID: "stranger",
	})
	if len(stranger.Objects) != 0 {
		t.Fatalf("share contract leaked to stranger: %+v", stranger)
	}
}

func TestRuntimeDispatchShareRejectsMismatchedContractScope(t *testing.T) {
	runtime := buildTestRuntime(t)
	if _, err := runtime.SubmitIngest(schemas.Event{
		Identity:  schemas.EventIdentity{EventID: "scoped-source", TenantID: "tenant", WorkspaceID: "workspace"},
		Actor:     schemas.EventActor{AgentID: "owner", TeamID: "team-a", SessionID: "session-a"},
		EventInfo: schemas.EventDescriptor{EventType: string(schemas.EventTypeMemoryWriteRequested)},
		Access:    schemas.EventAccess{Visibility: string(schemas.VisibilityPrivate)},
		Payload:   map[string]any{"text": "scoped source"},
	}); err != nil {
		t.Fatalf("SubmitIngest source: %v", err)
	}
	runtime.storage.Contracts().PutContract(schemas.ShareContract{
		ContractID: "wrong-scope", TenantID: "tenant", WorkspaceID: "workspace", Scope: "team-b",
		ReadAgents: []string{"reader"}, DeriveAgents: []string{"owner"},
	})

	if _, err := runtime.DispatchShareWithContract("owner", "reader", "mem_scoped-source", "wrong-scope"); err == nil ||
		!errors.Is(err, ErrShareForbidden) || !strings.Contains(err.Error(), "does not match memory scope") {
		t.Fatalf("mismatched contract scope was not rejected: %v", err)
	}
}

func TestRuntimeDispatchShareClassifiesInputAndLookupErrors(t *testing.T) {
	runtime := buildTestRuntime(t)
	if _, err := runtime.DispatchShareWithContract("", "reader", "missing", ""); !errors.Is(err, ErrShareInvalid) {
		t.Fatalf("invalid principal error = %v, want ErrShareInvalid", err)
	}
	if _, err := runtime.DispatchShareWithContract("owner", "reader", "missing", ""); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("missing memory error = %v, want ErrShareNotFound", err)
	}
	runtime.storage.Objects().PutMemory(schemas.Memory{MemoryID: "memory", AgentID: "owner", IsActive: true})
	if _, err := runtime.DispatchShareWithContract("stranger", "reader", "memory", ""); !errors.Is(err, ErrShareForbidden) {
		t.Fatalf("ownership error = %v, want ErrShareForbidden", err)
	}
}

func stateMutationEvent(eventID, value string) schemas.Event {
	return schemas.Event{
		EventID: eventID, TenantID: "tenant", WorkspaceID: "workspace",
		AgentID: "agent", SessionID: "session", EventType: string(schemas.EventTypeStateUpdate),
		Payload: map[string]any{schemas.PayloadKeyStateKey: "phase", schemas.PayloadKeyStateValue: value, "text": value},
	}
}
