package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/chain"
	"plasmod/src/internal/worker/cognitive"
	"plasmod/src/internal/worker/cognitive/memorybank"
	"plasmod/src/internal/worker/nodes"
)

func lifecycleRuntime(t *testing.T, store storage.RuntimeStorage) *Runtime {
	t.Helper()
	r := buildTestRuntime(t)
	if store != nil {
		r.storage = store
		r.tieredObjects = storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
		plane, err := dataplane.NewTieredDataPlaneWithEmbedder(r.tieredObjects, dataplane.NewTfidfEmbedder(32))
		if err != nil {
			t.Fatal(err)
		}
		r.plane = plane
		r.nodeManager = nodes.CreateManager()
		r.nodeManager.RegisterData(nodes.CreateInMemoryDataNode("data", store.Segments()))
		r.nodeManager.RegisterIndex(nodes.CreateInMemoryIndexNode("index", store.Indexes()))
		r.nodeManager.RegisterQuery(nodes.CreateInMemoryQueryNode("query", plane))
		r.mainChain = chain.CreateMainChain(r.nodeManager)
		r.memoryChain = chain.CreateMemoryPipelineChain(r.nodeManager)
		r.queryChain = chain.CreateQueryChain(r.nodeManager)
		r.collaborationChain = chain.CreateCollaborationChain(r.nodeManager)
		if err := r.ConfigureEmbeddingSpec(storage.EmbeddingSpec{Family: "tfidf", Dim: 32}); err != nil {
			t.Fatal(err)
		}
	}
	r.MemoryBankMode = true
	r.nodeManager.RegisterAlgorithmDispatch(cognitive.CreateAlgorithmDispatchWorker("bank", memorybank.New("bank", memorybank.DefaultConfig()), r.storage.Objects(), r.storage.AlgorithmStates(), r.storage.Audits(), r.storage))
	return r
}
func putConversation(t *testing.T, r *Runtime, id, session, text string, importance float64) string {
	t.Helper()
	confidence := 1.0
	ack, err := r.SubmitIngest(schemas.Event{Identity: schemas.EventIdentity{EventID: id, TenantID: "tenant", WorkspaceID: "workspace"}, Actor: schemas.EventActor{AgentID: "agent", SessionID: session}, EventInfo: schemas.EventDescriptor{EventType: "user_message", Importance: &importance, Confidence: &confidence}, Access: schemas.EventAccess{Visibility: "workspace"}, Payload: map[string]any{"text": text}})
	if err != nil {
		t.Fatal(err)
	}
	if ack["chain_name"] != "main_chain" {
		t.Fatalf("bypassed chain: %+v", ack)
	}
	return ack["memory_id"].(string)
}
func maintenanceRequest(id, operation string) MaintenanceRequest {
	return MaintenanceRequest{OperationID: id, Operation: operation, TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent"}
}

func TestLifecycleCandidateConfirmationAndScopedMaintenance(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	id := putConversation(t, r, "evt_candidate", "session1", "I prefer concise answers", 0)
	m, _ := r.storage.Objects().GetMemory(id)
	if m.LifecycleState != "candidate" {
		t.Fatalf("ordinary message admitted: %+v", m)
	}
	req := maintenanceRequest("decay", "decay")
	out, err := r.Maintain(context.Background(), req)
	if err != nil || out.UpdatedCount != 1 {
		t.Fatalf("decay: %+v %v", out, err)
	}
	m, _ = r.storage.Objects().GetMemory(id)
	if m.LifecycleState != "candidate" {
		t.Fatalf("decay promoted candidate: %s", m.LifecycleState)
	}
	req = maintenanceRequest("confirm", "update")
	req.Signals = map[string]float64{id: 1}
	out, err = r.Maintain(context.Background(), req)
	if err != nil || out.UpdatedCount != 1 || out.Items[0].After != "active" {
		t.Fatalf("confirmation: %+v %v", out, err)
	}
	first, _ := r.storage.Objects().GetState("governance:memorybank_v1:" + id)
	replay, err := r.Maintain(context.Background(), req)
	if err != nil || replay.Status != "completed" {
		t.Fatalf("retry: %+v %v", replay, err)
	}
	second, _ := r.storage.Objects().GetState(first.StateID)
	if second.StateValue != first.StateValue {
		t.Fatal("idempotent update reinforced twice")
	}
	req.Signals[id] = 2
	if _, err := r.Maintain(context.Background(), req); err == nil {
		t.Fatal("accepted conflicting idempotency key")
	}
	req = maintenanceRequest("foreign", "update")
	req.TenantID = "other"
	req.MemoryIDs = []string{id}
	out, err = r.Maintain(context.Background(), req)
	if err != nil || out.UpdatedCount != 0 || out.SkippedCount != 1 {
		t.Fatalf("tenant isolation: %+v %v", out, err)
	}
}

func TestLifecycleSummaryProvenanceRetrievalAndForget(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	a := putConversation(t, r, "evt_one", "session", "coffee without sugar", 1)
	b := putConversation(t, r, "evt_two", "session", "tea without milk", 1)
	out, err := r.Maintain(context.Background(), maintenanceRequest("summary", "summarize"))
	if err != nil || out.UpdatedCount != 2 || len(out.ProducedIDs) != 1 {
		t.Fatalf("summary: %+v %v", out, err)
	}
	id := out.ProducedIDs[0]
	derived, ok := r.storage.Objects().GetMemory(id)
	if !ok || derived.TenantID != "tenant" || derived.WorkspaceID != "workspace" || len(derived.SourceEventIDs) != 2 || derived.EmbeddingRef == "" {
		t.Fatalf("incomplete derivative: %+v", derived)
	}
	if len(r.storage.Versions().GetVersions(id)) == 0 || len(r.storage.Edges().EdgesFrom(id)) < 2 {
		t.Fatal("missing version/provenance")
	}
	for _, source := range []string{a, b} {
		m, _ := r.storage.Objects().GetMemory(source)
		if m.LifecycleState != "compressed" {
			t.Fatalf("source not persisted: %+v", m)
		}
	}
	if err := r.plane.Flush(); err != nil {
		t.Fatal(err)
	}
	query := schemas.QueryRequest{QueryText: "coffee", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", QueryScope: "workspace", ResponseMode: "objects_only", TopK: 10}
	if got := r.ExecuteQuery(query); !containsString(got.Objects, id) {
		t.Fatalf("derived not searchable: %+v", got)
	}
	query.TenantID = "other"
	if got := r.ExecuteQuery(query); containsString(got.Objects, id) {
		t.Fatal("cross-tenant summary leak")
	}
	query.TenantID = "tenant"
	req := MemoryMutationRequest{OperationID: "forget", Operation: "forget", TenantID: "tenant", WorkspaceID: "workspace", ReferenceIDs: []string{"evt_one"}}
	forgotten, err := r.MutateMemory(context.Background(), req)
	if err != nil || forgotten.Status != "completed" || !containsString(forgotten.ForgottenIDs, id) {
		t.Fatalf("forget: %+v %v", forgotten, err)
	}
	if !r.ObjectForgotten("evt_one") || !r.ObjectForgotten(id) {
		t.Fatal("raw or derived evidence can reappear")
	}
	if got := r.ExecuteQuery(query); containsString(got.Objects, id) || containsString(got.Objects, a) {
		t.Fatalf("forgotten returned: %+v", got)
	}
	if _, err := r.MutateMemory(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleDiskRecovery(t *testing.T) {
	t.Setenv(storage.EnvStorage, "disk")
	t.Setenv(storage.EnvDataDir, t.TempDir())
	t.Setenv(storage.EnvBadgerInMemory, "false")
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleRuntime(t, bundle.RuntimeStorage)
	id := putConversation(t, r, "evt_disk", "session", "persisted preference", 0)
	req := maintenanceRequest("disk_confirm", "update")
	req.MemoryIDs = []string{id}
	req.Signals = map[string]float64{id: 1}
	out, err := r.Maintain(context.Background(), req)
	if err != nil || out.Status != "completed" {
		t.Fatalf("before restart: %+v %v", out, err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err = storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	restored := lifecycleRuntime(t, bundle.RuntimeStorage)
	if receipt, ok := restored.MaintenanceStatus("tenant", "workspace", "disk_confirm"); !ok || receipt.UpdatedCount != 1 {
		t.Fatalf("lost receipt: %+v", receipt)
	}
	m, ok := restored.storage.Objects().GetMemory(id)
	if !ok || m.LifecycleState != "active" {
		t.Fatalf("lost state: %+v", m)
	}
	// Restored algorithm reads durable governance rather than re-admitting at zero signals.
	out2 := restored.DispatchAlgorithm("recall", []string{id}, "preference", "", "agent", "session", nil)
	if !containsString(out2.ScoredRefs, id) {
		t.Fatalf("restart recall: %+v", out2)
	}
}

type extractionProvider struct{}

func (extractionProvider) Complete(_ context.Context, system, input string) (string, error) {
	if len(system) > 0 && system[:7] == "Extract" {
		data, _ := json.Marshal(map[string]any{"facts": []map[string]any{{"text": "prefers concise answers", "kind": "preference_constraint", "quote": input, "start": 0, "end": len([]rune(input)), "confidence": .9}}})
		return string(data), nil
	}
	return "", errors.New("unexpected summary")
}
func TestLifecycleRepeatedExtractedFactsPromoteAcrossSessions(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	r.ConfigureCognition(extractionProvider{})
	for i, session := range []string{"s1", "s2", "s3"} {
		putConversation(t, r, "evt_repeat_"+session, session, "Please keep answers concise", 0)
		for _, m := range r.storage.Objects().ListMemories("agent", "") {
			if m.MemoryType == "preference_constraint" {
				if len(m.SourceSpans) != 1 {
					t.Fatal("missing exact quote")
				}
				if i < 2 && m.LifecycleState != "candidate" {
					t.Fatal("promoted without sufficient evidence")
				}
				if i == 2 && m.LifecycleState != "active" {
					t.Fatalf("repeat failed to promote: %+v", m)
				}
			}
		}
	}
}

type failingLifecyclePlane struct{ dataplane.DataPlane }

func (f failingLifecyclePlane) Ingest(dataplane.IngestRecord) error {
	return errors.New("projection unavailable")
}
func TestLifecycleSummaryFailureDoesNotCompressSources(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	id := putConversation(t, r, "evt_failure", "s", "retained source", 1)
	r.plane = failingLifecyclePlane{r.plane}
	out, err := r.Maintain(context.Background(), maintenanceRequest("failure", "compress"))
	if err != nil || out.Status != "partial" || out.FailedCount != 1 {
		t.Fatalf("hidden projection failure: %+v %v", out, err)
	}
	m, _ := r.storage.Objects().GetMemory(id)
	if m.LifecycleState != "active" {
		t.Fatal("source compressed before derivative was searchable")
	}
}

func TestLifecycleCorrectionAndConflictDecisionsPersist(t *testing.T) {
	t.Setenv(storage.EnvStorage, "disk")
	t.Setenv(storage.EnvDataDir, t.TempDir())
	t.Setenv(storage.EnvBadgerInMemory, "false")
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bundle.Close() })
	r := lifecycleRuntime(t, bundle.RuntimeStorage)
	old := putConversation(t, r, "evt_old", "s", "old preference", 1)
	replacement := schemas.Event{Identity: schemas.EventIdentity{TenantID: "tenant", WorkspaceID: "workspace"}, Actor: schemas.EventActor{AgentID: "agent", SessionID: "s"}, Payload: map[string]any{"text": "new preference"}}
	req := MemoryMutationRequest{OperationID: "correct", Operation: "correct", TenantID: "tenant", WorkspaceID: "workspace", ReferenceIDs: []string{old}, Replacement: &replacement}
	out, err := r.MutateMemory(context.Background(), req)
	if err != nil || out.Status != "completed" || out.ReplacementID == "" {
		t.Fatalf("correction: %+v %v", out, err)
	}
	again, err := r.MutateMemory(context.Background(), req)
	if err != nil || again.ReplacementID != out.ReplacementID {
		t.Fatalf("correction retry: %+v %v", again, err)
	}
	a := putConversation(t, r, "evt_pref_old", "s", "I prefer coffee", 1)
	b := putConversation(t, r, "evt_pref_new", "s", "I prefer tea", 1)
	for _, id := range []string{a, b} {
		m, _ := r.storage.Objects().GetMemory(id)
		m.MemoryType = "preference_constraint"
		r.storage.Objects().PutMemory(m)
	}
	decision := ConflictResolutionRequest{OperationID: "decision", TenantID: "tenant", WorkspaceID: "workspace", WinnerID: b, LoserID: a, Kind: "preference_change"}
	result, err := r.ResolveMemoryConflict(decision)
	if err != nil || result["status"] != "completed" {
		t.Fatalf("decision: %+v %v", result, err)
	}
	loser, _ := r.storage.Objects().GetMemory(a)
	if loser.IsActive || loser.ValidTo == "" {
		t.Fatal("lost preference history")
	}
	st, ok := r.storage.Objects().GetState("profile:" + stableID("tenant", "workspace", "agent"))
	if !ok || st.StateType != "inferred_profile" {
		t.Fatal("profile not persisted")
	}
	if _, err := r.ResolveMemoryConflict(decision); err != nil {
		t.Fatal(err)
	}
	if err := r.ShutdownConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err = storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	restored := lifecycleRuntime(t, bundle.RuntimeStorage)
	if state, ok := restored.storage.Objects().GetState(st.StateID); !ok || state.StateValue != st.StateValue {
		t.Fatal("inferred profile changed across restart")
	}
	if result, err := restored.ResolveMemoryConflict(decision); err != nil || result["status"] != "completed" {
		t.Fatalf("conflict decision lost: %+v %v", result, err)
	}
	if recalled := restored.DispatchAlgorithm("recall", []string{a, b}, "preference", "", "agent", "s", nil); !containsString(recalled.ScoredRefs, b) || containsString(recalled.ScoredRefs, a) {
		t.Fatalf("resolved conflict not restored: %+v", recalled)
	}
}

func TestQueuedTasksUseRuntimeChainsAndCountFailures(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	o := CreateOrchestrator(r.nodeManager, 1, 1)
	o.BindRuntime(r)
	o.execute(Task{ID: "queued", Type: TaskTypeIngest, Payload: chain.MainChainInput{Event: schemas.Event{EventID: "evt_queued", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", Payload: map[string]any{"text": "queued memory"}}}})
	if _, ok := r.storage.Objects().GetEvent("evt_queued"); !ok {
		t.Fatal("queued chain bypassed WAL/canonical runtime")
	}
	o.execute(Task{ID: "invalid", Type: TaskTypeMemory, Payload: "invalid"})
	if stats := o.Stats(); stats.Completed != 1 || stats.Failed != 1 {
		t.Fatalf("misreported execution: %+v", stats)
	}
}

func TestLifecycleDerivedDiskReindexAndForgetRecovery(t *testing.T) {
	t.Setenv(storage.EnvStorage, "disk")
	t.Setenv(storage.EnvDataDir, t.TempDir())
	t.Setenv(storage.EnvBadgerInMemory, "false")
	bundle, err := storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleRuntime(t, bundle.RuntimeStorage)
	putConversation(t, r, "evt_archive1", "s", "coffee without sugar", 1)
	putConversation(t, r, "evt_archive2", "s", "tea without milk", 1)
	receipt, err := r.Maintain(context.Background(), maintenanceRequest("persist-summary", "summarize"))
	if err != nil || len(receipt.ProducedIDs) != 1 {
		t.Fatalf("summary: %+v %v", receipt, err)
	}
	id := receipt.ProducedIDs[0]
	if err := r.ShutdownConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err = storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	restored := lifecycleRuntime(t, bundle.RuntimeStorage)
	if _, err := restored.ReindexEmbeddings(); err != nil {
		t.Fatal(err)
	}
	q := schemas.QueryRequest{QueryText: "coffee", TenantID: "tenant", WorkspaceID: "workspace", AgentID: "agent", QueryScope: "workspace", ResponseMode: "objects_only", TopK: 10}
	if got := restored.ExecuteQuery(q); !containsString(got.Objects, id) {
		t.Fatalf("derived not recalled after disk reopen/index rebuild: %+v", got)
	}
	mutation, err := restored.MutateMemory(context.Background(), MemoryMutationRequest{OperationID: "erase-persisted", Operation: "forget", TenantID: "tenant", WorkspaceID: "workspace", ReferenceIDs: []string{"evt_archive1"}})
	if err != nil || mutation.Status != "completed" {
		t.Fatalf("forget: %+v %v", mutation, err)
	}
	if err := restored.ShutdownConsistency(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err = storage.BuildRuntimeFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.Close()
	again := lifecycleRuntime(t, bundle.RuntimeStorage)
	if _, err := again.ReindexEmbeddings(); err != nil {
		t.Fatal(err)
	}
	if got := again.ExecuteQuery(q); containsString(got.Objects, id) {
		t.Fatalf("forgotten derived memory resurrected after index rebuild: %+v", got)
	}
}

func TestForgetExtractedFactRevokesOriginalAndSiblings(t *testing.T) {
	r := lifecycleRuntime(t, nil)
	r.ConfigureCognition(extractionProvider{})
	original := putConversation(t, r, "evt_fact_origin", "s", "Please keep answers concise", 1)
	factID := ""
	for _, m := range r.storage.Objects().ListMemories("agent", "s") {
		if m.MemoryType == "preference_constraint" {
			factID = m.MemoryID
		}
	}
	if factID == "" {
		t.Fatal("missing extracted fact")
	}
	out, err := r.MutateMemory(context.Background(), MemoryMutationRequest{OperationID: "forget-fact", Operation: "forget", TenantID: "tenant", WorkspaceID: "workspace", ReferenceIDs: []string{factID}})
	if err != nil || out.Status != "completed" || !containsString(out.ForgottenIDs, original) || !r.ObjectForgotten("evt_fact_origin") {
		t.Fatalf("original utterance can reintroduce forgotten fact: %+v %v", out, err)
	}
}
