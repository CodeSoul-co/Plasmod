package cognitive

import (
	"fmt"
	"strings"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemorySummarizationWorker produces level-1 (summary) and level-2
// (abstraction) Memory objects by compressing existing level-(n-1) memories
// for the given agent+session up to maxLevel.
//
// Level-1: concatenates all level-0 active memories into a single summary.
// Level-2: concatenates all level-1 summaries into an abstraction.
type InMemorySummarizationWorker struct {
	id       string
	objStore storage.ObjectStore
}

func CreateInMemorySummarizationWorker(id string, objStore storage.ObjectStore) *InMemorySummarizationWorker {
	return &InMemorySummarizationWorker{id: id, objStore: objStore}
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
		for _, m := range all {
			if m.Level == srcLevel && m.IsActive {
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
		memType := "semantic"
		if level == 2 {
			memType = "procedural"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		w.objStore.PutMemory(schemas.Memory{
			MemoryID:       fmt.Sprintf("summary_l%d_%s_%s_%d", level, agentID, sessionID, time.Now().UnixNano()),
			MemoryType:     memType,
			AgentID:        agentID,
			SessionID:      sessionID,
			Level:          level,
			Content:        strings.Join(parts, " | "),
			Summary:        fmt.Sprintf("Level-%d compression of %d memories", level, len(srcs)),
			SourceEventIDs: srcIDs,
			Confidence:     0.85,
			Importance:     totalImportance / float64(len(srcs)),
			IsActive:       true,
			ValidFrom:      now,
			Version:        1,
		})
	}
	return nil
}
