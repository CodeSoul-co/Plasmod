package cognitive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// ─── Summarization Strategy Interface ─────────────────────────────────────────
//
// SummarizationStrategy defines how memories at level N-1 are compressed into
// level N summaries. This allows swapping between dummy and LLM-based implementations.
type SummarizationStrategy interface {
	// Summarize compresses multiple source memories into a single summary.
	// level indicates the target level (1 for semantic, 2 for procedural).
	// Returns the summarized content and a description.
	Summarize(ctx context.Context, memories []schemas.Memory, level int) (content string, summary string, err error)
}

// dummySummarizationStrategy is the default placeholder implementation.
// It performs simple string concatenation without any semantic understanding.
//
// WARNING: This is NOT suitable for production use. Replace with an LLM-based
// strategy for meaningful memory summarization.
type dummySummarizationStrategy struct{}

func (s *dummySummarizationStrategy) Summarize(_ context.Context, memories []schemas.Memory, level int) (string, string, error) {
	var parts []string
	for _, m := range memories {
		parts = append(parts, m.Content)
	}
	content := strings.Join(parts, " | ")
	summary := fmt.Sprintf("Level-%d compression of %d memories (dummy concatenation)", level, len(memories))
	return content, summary, nil
}

// llmSummarizationStrategy uses an LLM to perform semantic summarization.
// This is the recommended production implementation.
//
// TODO: Implement when LLMProvider is available in pkg/llm.
// type llmSummarizationStrategy struct {
//     llm          LLMProvider
//     summaryTpl   string // level-1 prompt: "Summarize these episodic memories..."
//     abstractTpl  string // level-2 prompt: "Create a high-level abstraction..."
//     maxTokens    int    // context window budget per summarization call
// }

// ─── InMemorySummarizationWorker ──────────────────────────────────────────────

// InMemorySummarizationWorker produces level-1 (summary) and level-2
// (abstraction) Memory objects by compressing existing level-(n-1) memories
// for the given agent+session up to maxLevel.
//
// # Memory Hierarchy
//
//	Level 0: Episodic memories (raw event extractions)
//	Level 1: Semantic summaries (consolidated from level-0)
//	Level 2: Procedural abstractions (meta-summaries from level-1)
//
// # Dependency Injection Points
//
//   - objStore: ObjectStore for reading/writing Memory objects (required)
//   - strategy: SummarizationStrategy for memory compression (optional, defaults to dummy)
//   - llm: LLMProvider for future LLM integration (optional, reserved)
//
// # Current Implementation (defaultDummyImplementation)
//
// The current implementation uses simple string concatenation as a placeholder.
// This is intentional to allow the system to function without LLM dependencies
// during development and testing.
//
// # Future LLM Integration
//
// To enable LLM-based summarization:
//  1. Implement LLMProvider interface (e.g., OpenAIProvider, AnthropicProvider)
//  2. Create llmSummarizationStrategy using the LLMProvider
//  3. Inject the strategy via CreateInMemorySummarizationWorkerWithStrategy
type InMemorySummarizationWorker struct {
	id       string
	objStore storage.ObjectStore
	strategy SummarizationStrategy
	// llm is reserved for future LLM integration.
	// When set, it will be used by llmSummarizationStrategy.
	llm LLMProvider
}

// CreateInMemorySummarizationWorker creates a worker with the default
// dummy summarization strategy (simple string concatenation).
func CreateInMemorySummarizationWorker(id string, objStore storage.ObjectStore) *InMemorySummarizationWorker {
	return &InMemorySummarizationWorker{
		id:       id,
		objStore: objStore,
		strategy: &dummySummarizationStrategy{},
	}
}

// CreateInMemorySummarizationWorkerWithStrategy creates a worker with a
// custom summarization strategy. Use this for LLM-based implementations.
func CreateInMemorySummarizationWorkerWithStrategy(
	id string,
	objStore storage.ObjectStore,
	strategy SummarizationStrategy,
) *InMemorySummarizationWorker {
	return &InMemorySummarizationWorker{
		id:       id,
		objStore: objStore,
		strategy: strategy,
	}
}

// SetLLMProvider injects an LLM provider for future LLM-based summarization.
// This is a no-op until llmSummarizationStrategy is implemented.
func (w *InMemorySummarizationWorker) SetLLMProvider(llm LLMProvider) {
	w.llm = llm
}

func (w *InMemorySummarizationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.SummarizationInput)
	if !ok {
		return schemas.SummarizationOutput{}, fmt.Errorf("summarization: unexpected input type %T", input)
	}
	// snapshot existing summary IDs before the call so we can diff afterwards
	allBefore := w.objStore.ListMemories(in.AgentID, in.SessionID)
	existingIDs := make(map[string]bool, len(allBefore))
	for _, m := range allBefore {
		if m.Level > 0 {
			existingIDs[m.MemoryID] = true
		}
	}
	err := w.Summarize(in.AgentID, in.SessionID, in.MaxLevel)
	if err != nil {
		return schemas.SummarizationOutput{}, err
	}
	allAfter := w.objStore.ListMemories(in.AgentID, in.SessionID)
	var newIDs []string
	for _, m := range allAfter {
		if m.Level > 0 && !existingIDs[m.MemoryID] {
			newIDs = append(newIDs, m.MemoryID)
		}
	}
	return schemas.SummarizationOutput{ProducedIDs: newIDs}, nil
}

func (w *InMemorySummarizationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeSummarization,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"level1_summary", "level2_abstraction", "context_compression"},
	}
}

func (w *InMemorySummarizationWorker) Summarize(agentID, sessionID string, maxLevel int) error {
	if maxLevel < 1 {
		maxLevel = 1
	}
	if maxLevel > 2 {
		maxLevel = 2
	}
	for level := 1; level <= maxLevel; level++ {
		srcLevel := level - 1
		all := w.objStore.ListMemories(agentID, sessionID)

		var srcs []schemas.Memory
		var srcIDs []string
		var totalImportance float64
		for _, m := range all {
			if m.Level == srcLevel && m.IsActive {
				srcs = append(srcs, m)
				srcIDs = append(srcIDs, m.MemoryID)
				totalImportance += m.Importance
			}
		}
		if len(srcs) < 2 {
			continue
		}

		// Use the injected strategy (defaults to dummySummarizationStrategy)
		// Future: Replace with LLM-based strategy for semantic summarization
		content, summary, err := w.strategy.Summarize(context.Background(), srcs, level)
		if err != nil {
			return fmt.Errorf("summarization strategy failed at level %d: %w", level, err)
		}

		memType := string(schemas.MemoryTypeSemantic)
		if level == 2 {
			memType = string(schemas.MemoryTypeProcedural)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		w.objStore.PutMemory(schemas.Memory{
			MemoryID:       fmt.Sprintf("%sl%d_%s_%s_%d", schemas.IDPrefixSummary, level, agentID, sessionID, time.Now().UnixNano()),
			MemoryType:     memType,
			AgentID:        agentID,
			SessionID:      sessionID,
			Level:          level,
			Content:        content,
			Summary:        summary,
			SourceEventIDs: srcIDs,
			Confidence:     schemas.DefaultConfidence,
			Importance:     totalImportance / float64(len(srcs)),
			IsActive:       true,
			ValidFrom:      now,
			Version:        1,
		})
	}
	return nil
}
