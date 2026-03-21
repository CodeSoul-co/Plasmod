package cognitive

import (
	"context"
	"fmt"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// ─── LLM Provider Interface (Anti-Corruption Layer) ───────────────────────────
//
// LLMProvider defines the contract for LLM-based memory operations.
// This interface serves as a dependency injection point for future LLM integration.
//
// Production implementations should integrate with:
//   - MemGPT / Letta for hierarchical memory management
//   - LangMem for semantic memory compression
//   - OpenAI / Anthropic / local LLMs for text generation
//
// The interface is intentionally minimal to allow flexible backend implementations.
type LLMProvider interface {
	// Complete generates a completion for the given prompt.
	// Used for memory consolidation, summarization, and reflection.
	Complete(ctx context.Context, prompt string) (string, error)

	// Embed generates a vector embedding for the given text.
	// Used for semantic similarity and memory retrieval.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ─── Consolidation Strategy Interface ─────────────────────────────────────────
//
// ConsolidationStrategy defines how episodic memories are merged into semantic
// memories. This allows swapping between dummy and LLM-based implementations.
type ConsolidationStrategy interface {
	// Consolidate merges multiple episodic memories into a single semantic memory.
	// Returns the consolidated content and a summary description.
	Consolidate(ctx context.Context, memories []schemas.Memory) (content string, summary string, err error)
}

// dummyConsolidationStrategy is the default placeholder implementation.
// It performs simple string concatenation without any semantic understanding.
//
// WARNING: This is NOT suitable for production use. Replace with an LLM-based
// strategy for meaningful memory compression.
type dummyConsolidationStrategy struct{}

func (s *dummyConsolidationStrategy) Consolidate(_ context.Context, memories []schemas.Memory) (string, string, error) {
	combined := ""
	for _, m := range memories {
		combined += m.Content + " "
	}
	summary := fmt.Sprintf("Consolidated from %d level-0 memories (dummy concatenation)", len(memories))
	return combined, summary, nil
}

// llmConsolidationStrategy uses an LLM to perform semantic memory compression.
// This is the recommended production implementation.
//
// TODO: Implement when LLMProvider is available in pkg/llm.
// type llmConsolidationStrategy struct {
//     llm       LLMProvider
//     promptTpl string // e.g., "Summarize these memories into a coherent narrative:\n{{.Memories}}"
// }

// ─── InMemoryMemoryConsolidationWorker ────────────────────────────────────────

// InMemoryMemoryConsolidationWorker reads level-0 memories for an
// agent/session and produces a level-1 summary record.
//
// # Dependency Injection Points
//
//   - store: ObjectStore for reading/writing Memory objects (required)
//   - strategy: ConsolidationStrategy for memory merging (optional, defaults to dummy)
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
// To enable LLM-based consolidation:
//  1. Implement LLMProvider interface (e.g., OpenAIProvider, AnthropicProvider)
//  2. Create llmConsolidationStrategy using the LLMProvider
//  3. Inject the strategy via CreateInMemoryMemoryConsolidationWorkerWithStrategy
type InMemoryMemoryConsolidationWorker struct {
	id       string
	store    storage.ObjectStore
	strategy ConsolidationStrategy
	// llm is reserved for future LLM integration.
	// When set, it will be used by llmConsolidationStrategy.
	llm LLMProvider
}

// CreateInMemoryMemoryConsolidationWorker creates a worker with the default
// dummy consolidation strategy (simple string concatenation).
func CreateInMemoryMemoryConsolidationWorker(id string, store storage.ObjectStore) *InMemoryMemoryConsolidationWorker {
	return &InMemoryMemoryConsolidationWorker{
		id:       id,
		store:    store,
		strategy: &dummyConsolidationStrategy{},
	}
}

// CreateInMemoryMemoryConsolidationWorkerWithStrategy creates a worker with a
// custom consolidation strategy. Use this for LLM-based implementations.
func CreateInMemoryMemoryConsolidationWorkerWithStrategy(
	id string,
	store storage.ObjectStore,
	strategy ConsolidationStrategy,
) *InMemoryMemoryConsolidationWorker {
	return &InMemoryMemoryConsolidationWorker{
		id:       id,
		store:    store,
		strategy: strategy,
	}
}

// SetLLMProvider injects an LLM provider for future LLM-based consolidation.
// This is a no-op until llmConsolidationStrategy is implemented.
func (w *InMemoryMemoryConsolidationWorker) SetLLMProvider(llm LLMProvider) {
	w.llm = llm
}

func (w *InMemoryMemoryConsolidationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.MemoryConsolidationInput)
	if !ok {
		return schemas.MemoryConsolidationOutput{}, fmt.Errorf("consolidation: unexpected input type %T", input)
	}
	// capture source count before consolidation
	allBefore := w.store.ListMemories(in.AgentID, in.SessionID)
	sourceCount := 0
	for _, m := range allBefore {
		if m.Level == 0 && m.IsActive {
			sourceCount++
		}
	}
	err := w.Consolidate(in.AgentID, in.SessionID)
	if err != nil {
		return schemas.MemoryConsolidationOutput{}, err
	}
	summaryID := schemas.IDPrefixSummary + in.AgentID + "_" + in.SessionID
	if _, ok := w.store.GetMemory(summaryID); !ok {
		return schemas.MemoryConsolidationOutput{SourceCount: sourceCount}, nil
	}
	return schemas.MemoryConsolidationOutput{SummaryID: summaryID, SourceCount: sourceCount}, nil
}

func (w *InMemoryMemoryConsolidationWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeMemoryConsolidation,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"memory_consolidate", "level1_summary"},
	}
}

func (w *InMemoryMemoryConsolidationWorker) Consolidate(agentID, sessionID string) error {
	memories := w.store.ListMemories(agentID, sessionID)
	if len(memories) == 0 {
		return nil
	}

	// Filter level-0 active memories
	var level0Memories []schemas.Memory
	var sourceIDs []string
	for _, m := range memories {
		if m.Level == 0 && m.IsActive {
			level0Memories = append(level0Memories, m)
			sourceIDs = append(sourceIDs, m.MemoryID)
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}

	// Use the injected strategy (defaults to dummyConsolidationStrategy)
	// Future: Replace with LLM-based strategy for semantic compression
	content, summary, err := w.strategy.Consolidate(context.Background(), level0Memories)
	if err != nil {
		return fmt.Errorf("consolidation strategy failed: %w", err)
	}

	w.store.PutMemory(schemas.Memory{
		MemoryID:       schemas.IDPrefixSummary + agentID + "_" + sessionID,
		MemoryType:     string(schemas.MemoryTypeSemantic),
		AgentID:        agentID,
		SessionID:      sessionID,
		SourceEventIDs: sourceIDs,
		Content:        content,
		Summary:        summary,
		Level:          1,
		IsActive:       true,
		Version:        1,
	})
	return nil
}
