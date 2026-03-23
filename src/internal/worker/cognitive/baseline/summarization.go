package baseline

import (
	"fmt"
	"strings"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemorySummarizationWorker produces level-1 (summary) and level-2
// (abstraction) Memory objects by compressing existing level-(n-1) memories.
// This is the baseline algorithm's summarization pipeline step.
type InMemorySummarizationWorker struct {
	id       string
	objStore storage.ObjectStore
}

func CreateInMemorySummarizationWorker(id string, objStore storage.ObjectStore) *InMemorySummarizationWorker {
	return &InMemorySummarizationWorker{id: id, objStore: objStore}
}

func (w *InMemorySummarizationWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.SummarizationInput)
	if !ok {
		return schemas.SummarizationOutput{}, fmt.Errorf("summarization: unexpected input type %T", input)
	}
	allBefore := w.objStore.ListMemories(in.AgentID, in.SessionID)
	existingIDs := make(map[string]bool, len(allBefore))
	for _, m := range allBefore {
		if m.Level > 0 {
			existingIDs[m.MemoryID] = true
		}
	}
	if err := w.Summarize(in.AgentID, in.SessionID, in.MaxLevel); err != nil {
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
		all := w.objStore.ListMemories(agentID, sessionID)
		var srcs []schemas.Memory
		var srcIDs []string
		for _, m := range all {
			if m.Level == level-1 && m.IsActive {
				srcs = append(srcs, m)
				srcIDs = append(srcIDs, m.MemoryID)
			}
		}
		if len(srcs) < 2 {
			continue
		}
		var parts []string
		var totalImportance float64
		for _, m := range srcs {
			parts = append(parts, m.Content)
			totalImportance += m.Importance
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
			Content:        strings.Join(parts, " | "),
			Summary:        fmt.Sprintf("Level-%d compression of %d memories", level, len(srcs)),
			SourceEventIDs: srcIDs,
			Confidence:     schemas.DefaultConfidence,
			Importance:     totalImportance / float64(len(srcs)),
			IsActive:       true,
			LifecycleState: string(schemas.MemoryLifecycleActive),
			ValidFrom:      now,
			Version:        1,
		})
	}
	return nil
}
