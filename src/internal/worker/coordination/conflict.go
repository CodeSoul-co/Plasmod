package coordination

import (
	"fmt"
	"time"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryConflictMergeWorker resolves conflicts between two competing memory
// objects via last-writer-wins (higher Version survives).
// The loser is marked IsActive=false and a "conflict_resolved" edge is created.
type InMemoryConflictMergeWorker struct {
	id        string
	objStore  storage.ObjectStore
	edgeStore storage.GraphEdgeStore
}

func CreateInMemoryConflictMergeWorker(
	id string,
	objStore storage.ObjectStore,
	edgeStore storage.GraphEdgeStore,
) *InMemoryConflictMergeWorker {
	return &InMemoryConflictMergeWorker{id: id, objStore: objStore, edgeStore: edgeStore}
}

func (w *InMemoryConflictMergeWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeConflictMerge,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"conflict_detect", "last_writer_wins", "conflict_edge"},
	}
}

func (w *InMemoryConflictMergeWorker) Merge(leftID, rightID, objectType string) error {
	if objectType != "memory" || leftID == rightID {
		return nil
	}
	left, okL := w.objStore.GetMemory(leftID)
	right, okR := w.objStore.GetMemory(rightID)
	if !okL || !okR {
		return nil
	}
	if left.AgentID != right.AgentID || left.SessionID != right.SessionID {
		return nil
	}
	if !left.IsActive || !right.IsActive {
		return nil
	}
	var winnerID, loserID string
	if left.Version >= right.Version {
		winnerID, loserID = leftID, rightID
		right.IsActive = false
		w.objStore.PutMemory(right)
	} else {
		winnerID, loserID = rightID, leftID
		left.IsActive = false
		w.objStore.PutMemory(left)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	w.edgeStore.PutEdge(schemas.Edge{
		EdgeID:      fmt.Sprintf("edge_%s_conflict_%s", winnerID, loserID),
		SrcObjectID: winnerID,
		SrcType:     objectType,
		EdgeType:    "conflict_resolved",
		DstObjectID: loserID,
		DstType:     objectType,
		Weight:      1.0,
		CreatedTS:   now,
	})
	return nil
}
