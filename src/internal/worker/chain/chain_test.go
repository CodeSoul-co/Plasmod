// Package chain provides unit tests for the four canonical execution flow chains.
//
// Test coverage:
//   - MainChain: write path task flow and error propagation
//   - MemoryPipelineChain: cognitive pipeline with consolidation toggle
//   - QueryChain: reasoning/post-processing with proof trace
//   - CollaborationChain: conflict resolution and cross-agent communication
//
// Author: Member D
package chain

import (
	"errors"
	"testing"

	"andb/src/internal/schemas"
	"andb/src/internal/worker/nodes"
)

// ─── Mock Workers ─────────────────────────────────────────────────────────────
//
// These mock implementations allow testing chain logic without real storage
// or network dependencies. Each mock tracks call counts and can be configured
// to return errors for negative path testing.

// mockObjectMaterializationWorker implements nodes.ObjectMaterializationWorker
type mockObjectMaterializationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockObjectMaterializationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeObjectMaterialization, State: nodes.NodeStateReady}
}

func (m *mockObjectMaterializationWorker) Materialize(_ schemas.Event) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock object materialization error")
	}
	return nil
}

func (m *mockObjectMaterializationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.ObjectMaterializationOutput{}, nil
}

// mockStateMaterializationWorker implements nodes.StateMaterializationWorker
type mockStateMaterializationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockStateMaterializationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeStateMaterialization, State: nodes.NodeStateReady}
}

func (m *mockStateMaterializationWorker) Apply(_ schemas.Event) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock state materialization error")
	}
	return nil
}

func (m *mockStateMaterializationWorker) Checkpoint(_, _ string) error {
	return nil
}

func (m *mockStateMaterializationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.StateApplyOutput{}, nil
}

// mockToolTraceWorker implements nodes.ToolTraceWorker
type mockToolTraceWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockToolTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeToolTrace, State: nodes.NodeStateReady}
}

func (m *mockToolTraceWorker) TraceToolCall(_ schemas.Event) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock tool trace error")
	}
	return nil
}

func (m *mockToolTraceWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.ToolTraceOutput{}, nil
}

// mockIndexBuildWorker implements nodes.IndexBuildWorker
type mockIndexBuildWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockIndexBuildWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeIndexBuild, State: nodes.NodeStateReady}
}

func (m *mockIndexBuildWorker) IndexObject(_, _, _, _ string) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock index build error")
	}
	return nil
}

func (m *mockIndexBuildWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.IndexBuildOutput{}, nil
}

// mockGraphRelationWorker implements nodes.GraphRelationWorker
type mockGraphRelationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockGraphRelationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeGraphRelation, State: nodes.NodeStateReady}
}

func (m *mockGraphRelationWorker) IndexEdge(_, _, _, _, _ string, _ float64) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock graph relation error")
	}
	return nil
}

func (m *mockGraphRelationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.GraphRelationOutput{}, nil
}

// mockMemoryExtractionWorker implements nodes.MemoryExtractionWorker
type mockMemoryExtractionWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockMemoryExtractionWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeMemoryExtraction, State: nodes.NodeStateReady}
}

func (m *mockMemoryExtractionWorker) Extract(_, _, _, _ string) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock memory extraction error")
	}
	return nil
}

func (m *mockMemoryExtractionWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.MemoryExtractionOutput{}, nil
}

// mockMemoryConsolidationWorker implements nodes.MemoryConsolidationWorker
type mockMemoryConsolidationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockMemoryConsolidationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeMemoryConsolidation, State: nodes.NodeStateReady}
}

func (m *mockMemoryConsolidationWorker) Consolidate(_, _ string) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock memory consolidation error")
	}
	return nil
}

func (m *mockMemoryConsolidationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.MemoryConsolidationOutput{}, nil
}

// mockSummarizationWorker implements nodes.SummarizationWorker
type mockSummarizationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockSummarizationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeSummarization, State: nodes.NodeStateReady}
}

func (m *mockSummarizationWorker) Summarize(_, _ string, _ int) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock summarization error")
	}
	return nil
}

func (m *mockSummarizationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.SummarizationOutput{}, nil
}

// mockReflectionPolicyWorker implements nodes.ReflectionPolicyWorker
type mockReflectionPolicyWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockReflectionPolicyWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeReflectionPolicy, State: nodes.NodeStateReady}
}

func (m *mockReflectionPolicyWorker) Reflect(_, _ string) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock reflection policy error")
	}
	return nil
}

func (m *mockReflectionPolicyWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.ReflectionPolicyOutput{}, nil
}

// mockProofTraceWorker implements nodes.ProofTraceWorker
type mockProofTraceWorker struct {
	id        string
	callCount int
	trace     []string
}

func (m *mockProofTraceWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeProofTrace, State: nodes.NodeStateReady}
}

func (m *mockProofTraceWorker) AssembleTrace(objectIDs []string, _ int) []string {
	m.callCount++
	if m.trace != nil {
		return m.trace
	}
	// Default: return object IDs as trace steps
	return objectIDs
}

func (m *mockProofTraceWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.ProofTraceOutput{}, nil
}

// mockSubgraphExecutorWorker implements nodes.SubgraphExecutorWorker
type mockSubgraphExecutorWorker struct {
	id        string
	callCount int
}

func (m *mockSubgraphExecutorWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeSubgraph, State: nodes.NodeStateReady}
}

func (m *mockSubgraphExecutorWorker) Expand(req schemas.GraphExpandRequest, gNodes []schemas.GraphNode, edges []schemas.Edge) schemas.GraphExpandResponse {
	m.callCount++
	return schemas.GraphExpandResponse{
		Subgraph: schemas.EvidenceSubgraph{
			Nodes: gNodes,
			Edges: edges,
		},
	}
}

func (m *mockSubgraphExecutorWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.SubgraphExpandOutput{}, nil
}

// mockConflictMergeWorker implements nodes.ConflictMergeWorker
type mockConflictMergeWorker struct {
	id        string
	callCount int
	winnerID  string
	shouldErr bool
}

func (m *mockConflictMergeWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeConflictMerge, State: nodes.NodeStateReady}
}

func (m *mockConflictMergeWorker) Merge(leftID, rightID, _ string) (string, error) {
	m.callCount++
	if m.shouldErr {
		return "", errors.New("mock conflict merge error")
	}
	if m.winnerID != "" {
		return m.winnerID, nil
	}
	return leftID, nil
}

func (m *mockConflictMergeWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.ConflictMergeOutput{}, nil
}

// mockCommunicationWorker implements nodes.CommunicationWorker
type mockCommunicationWorker struct {
	id        string
	callCount int
	shouldErr bool
}

func (m *mockCommunicationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeCommunication, State: nodes.NodeStateReady}
}

func (m *mockCommunicationWorker) Broadcast(_, _, _ string) error {
	m.callCount++
	if m.shouldErr {
		return errors.New("mock communication error")
	}
	return nil
}

func (m *mockCommunicationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.BroadcastOutput{}, nil
}

// mockMicroBatchScheduler implements nodes.MicroBatchScheduler
type mockMicroBatchScheduler struct {
	id        string
	callCount int
	queue     []any
}

func (m *mockMicroBatchScheduler) Info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: m.id, Type: nodes.NodeTypeMicroBatch, State: nodes.NodeStateReady}
}

func (m *mockMicroBatchScheduler) Enqueue(_ string, payload any) []any {
	m.callCount++
	m.queue = append(m.queue, payload)
	return nil
}

func (m *mockMicroBatchScheduler) Flush() []any {
	out := m.queue
	m.queue = nil
	return out
}

func (m *mockMicroBatchScheduler) SetThreshold(_ int) {}

func (m *mockMicroBatchScheduler) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	return schemas.MicroBatchFlushOutput{}, nil
}

// ─── Test Helpers ─────────────────────────────────────────────────────────────

// buildTestManager creates a Manager with all mock workers registered.
func buildTestManager() (*nodes.Manager, *testMocks) {
	mgr := nodes.CreateManager()
	mocks := &testMocks{
		objMat:       &mockObjectMaterializationWorker{id: "mock_obj_mat"},
		stateMat:     &mockStateMaterializationWorker{id: "mock_state_mat"},
		toolTrace:    &mockToolTraceWorker{id: "mock_tool_trace"},
		indexBuild:   &mockIndexBuildWorker{id: "mock_index_build"},
		graphRel:     &mockGraphRelationWorker{id: "mock_graph_rel"},
		memExtract:   &mockMemoryExtractionWorker{id: "mock_mem_extract"},
		memConsol:    &mockMemoryConsolidationWorker{id: "mock_mem_consol"},
		summarize:    &mockSummarizationWorker{id: "mock_summarize"},
		reflect:      &mockReflectionPolicyWorker{id: "mock_reflect"},
		proofTrace:   &mockProofTraceWorker{id: "mock_proof_trace"},
		subgraph:     &mockSubgraphExecutorWorker{id: "mock_subgraph"},
		conflictMrg:  &mockConflictMergeWorker{id: "mock_conflict_merge"},
		comm:         &mockCommunicationWorker{id: "mock_comm"},
		microBatch:   &mockMicroBatchScheduler{id: "mock_micro_batch"},
	}

	mgr.RegisterObjectMaterialization(mocks.objMat)
	mgr.RegisterStateMaterialization(mocks.stateMat)
	mgr.RegisterToolTrace(mocks.toolTrace)
	mgr.RegisterIndexBuild(mocks.indexBuild)
	mgr.RegisterGraphRelation(mocks.graphRel)
	mgr.RegisterMemoryExtraction(mocks.memExtract)
	mgr.RegisterMemoryConsolidation(mocks.memConsol)
	mgr.RegisterSummarization(mocks.summarize)
	mgr.RegisterReflectionPolicy(mocks.reflect)
	mgr.RegisterProofTrace(mocks.proofTrace)
	mgr.RegisterSubgraphExecutor(mocks.subgraph)
	mgr.RegisterConflictMerge(mocks.conflictMrg)
	mgr.RegisterCommunication(mocks.comm)
	mgr.RegisterMicroBatch(mocks.microBatch)

	return mgr, mocks
}

type testMocks struct {
	objMat      *mockObjectMaterializationWorker
	stateMat    *mockStateMaterializationWorker
	toolTrace   *mockToolTraceWorker
	indexBuild  *mockIndexBuildWorker
	graphRel    *mockGraphRelationWorker
	memExtract  *mockMemoryExtractionWorker
	memConsol   *mockMemoryConsolidationWorker
	summarize   *mockSummarizationWorker
	reflect     *mockReflectionPolicyWorker
	proofTrace  *mockProofTraceWorker
	subgraph    *mockSubgraphExecutorWorker
	conflictMrg *mockConflictMergeWorker
	comm        *mockCommunicationWorker
	microBatch  *mockMicroBatchScheduler
}

// ─── MainChain Tests ──────────────────────────────────────────────────────────

func TestMainChain_Run_Success(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateMainChain(mgr)

	input := MainChainInput{
		Event: schemas.Event{
			EventID:     "evt_001",
			WorkspaceID: "ws_test",
			Payload:     map[string]any{schemas.PayloadKeyText: "test content"},
		},
	}

	result := chain.Run(input)

	if !result.OK {
		t.Errorf("MainChain.Run() failed: %s", result.Error)
	}
	if result.ChainName != "main_chain" {
		t.Errorf("ChainName = %q, want %q", result.ChainName, "main_chain")
	}
	if mocks.objMat.callCount != 1 {
		t.Errorf("ObjectMaterialization call count = %d, want 1", mocks.objMat.callCount)
	}
	if mocks.stateMat.callCount != 1 {
		t.Errorf("StateMaterialization call count = %d, want 1", mocks.stateMat.callCount)
	}
	if mocks.toolTrace.callCount != 1 {
		t.Errorf("ToolTrace call count = %d, want 1", mocks.toolTrace.callCount)
	}
	if mocks.indexBuild.callCount != 1 {
		t.Errorf("IndexBuild call count = %d, want 1", mocks.indexBuild.callCount)
	}
	if mocks.graphRel.callCount != 1 {
		t.Errorf("GraphRelation call count = %d, want 1", mocks.graphRel.callCount)
	}

	// Verify meta contains expected fields
	if result.Meta["event_id"] != "evt_001" {
		t.Errorf("Meta[event_id] = %v, want evt_001", result.Meta["event_id"])
	}
}

func TestMainChain_Run_ObjectMaterializationError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.objMat.shouldErr = true
	chain := CreateMainChain(mgr)

	input := MainChainInput{
		Event: schemas.Event{EventID: "evt_err"},
	}

	result := chain.Run(input)

	if result.OK {
		t.Error("MainChain.Run() should fail when ObjectMaterialization errors")
	}
	if result.Error == "" {
		t.Error("MainChain.Run() should return error message")
	}
	// Subsequent workers should NOT be called
	if mocks.indexBuild.callCount != 0 {
		t.Errorf("IndexBuild should not be called after error, got %d calls", mocks.indexBuild.callCount)
	}
}

func TestMainChain_Run_IndexBuildError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.indexBuild.shouldErr = true
	chain := CreateMainChain(mgr)

	input := MainChainInput{
		Event: schemas.Event{EventID: "evt_idx_err"},
	}

	result := chain.Run(input)

	if result.OK {
		t.Error("MainChain.Run() should fail when IndexBuild errors")
	}
	// GraphRelation should NOT be called after IndexBuild error
	if mocks.graphRel.callCount != 0 {
		t.Errorf("GraphRelation should not be called after error, got %d calls", mocks.graphRel.callCount)
	}
}

func TestMainChain_Run_CustomMemoryID(t *testing.T) {
	mgr, _ := buildTestManager()
	chain := CreateMainChain(mgr)

	input := MainChainInput{
		Event:     schemas.Event{EventID: "evt_custom"},
		MemoryID:  "custom_mem_id",
		Namespace: "custom_ns",
	}

	result := chain.Run(input)

	if !result.OK {
		t.Errorf("MainChain.Run() failed: %s", result.Error)
	}
	if result.Meta["memory_id"] != "custom_mem_id" {
		t.Errorf("Meta[memory_id] = %v, want custom_mem_id", result.Meta["memory_id"])
	}
	if result.Meta["namespace"] != "custom_ns" {
		t.Errorf("Meta[namespace] = %v, want custom_ns", result.Meta["namespace"])
	}
}

// ─── MemoryPipelineChain Tests ────────────────────────────────────────────────

func TestMemoryPipelineChain_Run_ExtractionOnly(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateMemoryPipelineChain(mgr)

	input := MemoryPipelineInput{
		EventID:          "evt_mem_001",
		AgentID:          "agent_1",
		SessionID:        "session_1",
		Content:          "test memory content",
		RunConsolidation: false,
	}

	result := chain.Run(input)

	if !result.OK {
		t.Errorf("MemoryPipelineChain.Run() failed: %s", result.Error)
	}
	if mocks.memExtract.callCount != 1 {
		t.Errorf("MemoryExtraction call count = %d, want 1", mocks.memExtract.callCount)
	}
	// Consolidation should NOT run
	if mocks.memConsol.callCount != 0 {
		t.Errorf("MemoryConsolidation should not run, got %d calls", mocks.memConsol.callCount)
	}
	if mocks.summarize.callCount != 0 {
		t.Errorf("Summarization should not run, got %d calls", mocks.summarize.callCount)
	}
	// Reflection should always run
	if mocks.reflect.callCount != 1 {
		t.Errorf("ReflectionPolicy call count = %d, want 1", mocks.reflect.callCount)
	}
}

func TestMemoryPipelineChain_Run_WithConsolidation(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateMemoryPipelineChain(mgr)

	input := MemoryPipelineInput{
		EventID:          "evt_mem_002",
		AgentID:          "agent_1",
		SessionID:        "session_1",
		Content:          "test memory content",
		RunConsolidation: true,
		MaxSummaryLevel:  2,
	}

	result := chain.Run(input)

	if !result.OK {
		t.Errorf("MemoryPipelineChain.Run() failed: %s", result.Error)
	}
	if mocks.memExtract.callCount != 1 {
		t.Errorf("MemoryExtraction call count = %d, want 1", mocks.memExtract.callCount)
	}
	if mocks.memConsol.callCount != 1 {
		t.Errorf("MemoryConsolidation call count = %d, want 1", mocks.memConsol.callCount)
	}
	if mocks.summarize.callCount != 1 {
		t.Errorf("Summarization call count = %d, want 1", mocks.summarize.callCount)
	}
	if mocks.reflect.callCount != 1 {
		t.Errorf("ReflectionPolicy call count = %d, want 1", mocks.reflect.callCount)
	}

	// Verify meta
	if result.Meta["consolidation_ran"] != true {
		t.Errorf("Meta[consolidation_ran] = %v, want true", result.Meta["consolidation_ran"])
	}
}

func TestMemoryPipelineChain_Run_ExtractionError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.memExtract.shouldErr = true
	chain := CreateMemoryPipelineChain(mgr)

	input := MemoryPipelineInput{
		EventID:          "evt_err",
		AgentID:          "agent_1",
		SessionID:        "session_1",
		RunConsolidation: true,
	}

	result := chain.Run(input)

	if result.OK {
		t.Error("MemoryPipelineChain.Run() should fail when extraction errors")
	}
	// Subsequent workers should NOT be called
	if mocks.memConsol.callCount != 0 {
		t.Errorf("MemoryConsolidation should not be called after error")
	}
}

func TestMemoryPipelineChain_Run_ConsolidationError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.memConsol.shouldErr = true
	chain := CreateMemoryPipelineChain(mgr)

	input := MemoryPipelineInput{
		EventID:          "evt_consol_err",
		AgentID:          "agent_1",
		SessionID:        "session_1",
		RunConsolidation: true,
	}

	result := chain.Run(input)

	if result.OK {
		t.Error("MemoryPipelineChain.Run() should fail when consolidation errors")
	}
	// Summarization should NOT be called after consolidation error
	if mocks.summarize.callCount != 0 {
		t.Errorf("Summarization should not be called after error")
	}
}

// ─── QueryChain Tests ─────────────────────────────────────────────────────────

func TestQueryChain_Run_Success(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.proofTrace.trace = []string{"step1", "step2", "step3"}
	chain := CreateQueryChain(mgr)

	input := QueryChainInput{
		ObjectIDs: []string{"obj_1", "obj_2"},
		MaxDepth:  3,
		GraphNodes: []schemas.GraphNode{
			{ObjectID: "obj_1", ObjectType: "memory"},
		},
		GraphEdges: []schemas.Edge{
			{EdgeID: "edge_1", SrcObjectID: "obj_1", DstObjectID: "obj_2"},
		},
	}

	output, result := chain.Run(input)

	if !result.OK {
		t.Errorf("QueryChain.Run() failed: %s", result.Error)
	}
	if len(output.ProofTrace) != 3 {
		t.Errorf("ProofTrace length = %d, want 3", len(output.ProofTrace))
	}
	if mocks.proofTrace.callCount != 1 {
		t.Errorf("ProofTrace call count = %d, want 1", mocks.proofTrace.callCount)
	}
	if mocks.subgraph.callCount != 1 {
		t.Errorf("SubgraphExecutor call count = %d, want 1", mocks.subgraph.callCount)
	}
}

func TestQueryChain_Run_NoSubgraphExpansion(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateQueryChain(mgr)

	// No GraphNodes or GraphEdges provided
	input := QueryChainInput{
		ObjectIDs: []string{"obj_1"},
		MaxDepth:  2,
	}

	output, result := chain.Run(input)

	if !result.OK {
		t.Errorf("QueryChain.Run() failed: %s", result.Error)
	}
	// Subgraph expansion should be skipped
	if mocks.subgraph.callCount != 0 {
		t.Errorf("SubgraphExecutor should not be called without graph data, got %d calls", mocks.subgraph.callCount)
	}
	if len(output.Subgraph.Edges) != 0 {
		t.Errorf("Subgraph.Edges should be empty, got %d", len(output.Subgraph.Edges))
	}
}

func TestQueryChain_Run_EmptyObjectIDs(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateQueryChain(mgr)

	input := QueryChainInput{
		ObjectIDs: []string{},
	}

	_, result := chain.Run(input)

	if !result.OK {
		t.Errorf("QueryChain.Run() failed: %s", result.Error)
	}
	// ProofTrace should still be called (with empty input)
	if mocks.proofTrace.callCount != 1 {
		t.Errorf("ProofTrace call count = %d, want 1", mocks.proofTrace.callCount)
	}
}

// ─── CollaborationChain Tests ─────────────────────────────────────────────────

func TestCollaborationChain_Run_Success(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.conflictMrg.winnerID = "mem_left"
	chain := CreateCollaborationChain(mgr)

	input := CollaborationChainInput{
		LeftMemID:     "mem_left",
		RightMemID:    "mem_right",
		ObjectType:    "memory",
		SourceAgentID: "agent_A",
		TargetAgentID: "agent_B",
	}

	output, result := chain.Run(input)

	if !result.OK {
		t.Errorf("CollaborationChain.Run() failed: %s", result.Error)
	}
	if output.WinnerMemID != "mem_left" {
		t.Errorf("WinnerMemID = %q, want %q", output.WinnerMemID, "mem_left")
	}
	if mocks.conflictMrg.callCount != 1 {
		t.Errorf("ConflictMerge call count = %d, want 1", mocks.conflictMrg.callCount)
	}
	if mocks.microBatch.callCount != 1 {
		t.Errorf("MicroBatch call count = %d, want 1", mocks.microBatch.callCount)
	}
	if mocks.comm.callCount != 1 {
		t.Errorf("Communication call count = %d, want 1", mocks.comm.callCount)
	}
	// Verify shared memory ID format
	expectedSharedID := schemas.IDPrefixShared + "mem_left_to_agent_B"
	if output.SharedMemID != expectedSharedID {
		t.Errorf("SharedMemID = %q, want %q", output.SharedMemID, expectedSharedID)
	}
}

func TestCollaborationChain_Run_SameAgent(t *testing.T) {
	mgr, mocks := buildTestManager()
	chain := CreateCollaborationChain(mgr)

	input := CollaborationChainInput{
		LeftMemID:     "mem_left",
		RightMemID:    "mem_right",
		ObjectType:    "memory",
		SourceAgentID: "agent_A",
		TargetAgentID: "agent_A", // Same as source
	}

	output, result := chain.Run(input)

	if !result.OK {
		t.Errorf("CollaborationChain.Run() failed: %s", result.Error)
	}
	// Communication should NOT be called for same agent
	if mocks.comm.callCount != 0 {
		t.Errorf("Communication should not be called for same agent, got %d calls", mocks.comm.callCount)
	}
	if output.SharedMemID != "" {
		t.Errorf("SharedMemID should be empty for same agent, got %q", output.SharedMemID)
	}
}

func TestCollaborationChain_Run_ConflictMergeError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.conflictMrg.shouldErr = true
	chain := CreateCollaborationChain(mgr)

	input := CollaborationChainInput{
		LeftMemID:     "mem_left",
		RightMemID:    "mem_right",
		ObjectType:    "memory",
		SourceAgentID: "agent_A",
		TargetAgentID: "agent_B",
	}

	_, result := chain.Run(input)

	if result.OK {
		t.Error("CollaborationChain.Run() should fail when ConflictMerge errors")
	}
	// Communication should NOT be called after error
	if mocks.comm.callCount != 0 {
		t.Errorf("Communication should not be called after error")
	}
}

func TestCollaborationChain_Run_CommunicationError(t *testing.T) {
	mgr, mocks := buildTestManager()
	mocks.comm.shouldErr = true
	chain := CreateCollaborationChain(mgr)

	input := CollaborationChainInput{
		LeftMemID:     "mem_left",
		RightMemID:    "mem_right",
		ObjectType:    "memory",
		SourceAgentID: "agent_A",
		TargetAgentID: "agent_B",
	}

	_, result := chain.Run(input)

	if result.OK {
		t.Error("CollaborationChain.Run() should fail when Communication errors")
	}
}

// ─── ChainResult Helper Tests ─────────────────────────────────────────────────

func TestChainResult_Ok(t *testing.T) {
	result := ok("test_chain", map[string]any{"key": "value"})

	if !result.OK {
		t.Error("ok() should return OK=true")
	}
	if result.ChainName != "test_chain" {
		t.Errorf("ChainName = %q, want %q", result.ChainName, "test_chain")
	}
	if result.Error != "" {
		t.Errorf("Error should be empty, got %q", result.Error)
	}
	if result.Meta["key"] != "value" {
		t.Errorf("Meta[key] = %v, want value", result.Meta["key"])
	}
}

func TestChainResult_Fail(t *testing.T) {
	result := fail("test_chain", "something went wrong")

	if result.OK {
		t.Error("fail() should return OK=false")
	}
	if result.ChainName != "test_chain" {
		t.Errorf("ChainName = %q, want %q", result.ChainName, "test_chain")
	}
	if result.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", result.Error, "something went wrong")
	}
}
