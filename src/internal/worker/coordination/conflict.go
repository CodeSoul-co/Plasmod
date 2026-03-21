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

func (w *InMemoryConflictMergeWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.ConflictMergeInput)
	if !ok {
		return schemas.ConflictMergeOutput{}, fmt.Errorf("conflict_merge: unexpected input type %T", input)
	}
	winnerID, err := w.Merge(in.LeftID, in.RightID, in.ObjectType)
	if err != nil {
		return schemas.ConflictMergeOutput{}, err
	}
	if winnerID == "" {
		return schemas.ConflictMergeOutput{}, nil
	}
	loserID := in.LeftID
	if winnerID == in.LeftID {
		loserID = in.RightID
	}
	return schemas.ConflictMergeOutput{WinnerID: winnerID, LoserID: loserID, Resolved: true}, nil
}

func (w *InMemoryConflictMergeWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeConflictMerge,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"conflict_detect", "last_writer_wins", "conflict_edge"},
	}
}

func (w *InMemoryConflictMergeWorker) Merge(leftID, rightID, objectType string) (string, error) {
	if objectType != "memory" || leftID == rightID {
		return "", nil
	}
	left, okL := w.objStore.GetMemory(leftID)
	right, okR := w.objStore.GetMemory(rightID)
	if !okL || !okR {
		return "", nil
	}
	if left.AgentID != right.AgentID || left.SessionID != right.SessionID {
		return "", nil
	}
	if !left.IsActive || !right.IsActive {
		return "", nil
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
		EdgeID:      schemas.IDPrefixEdge + winnerID + "_conflict_" + loserID,
		SrcObjectID: winnerID,
		SrcType:     objectType,
		EdgeType:    string(schemas.EdgeTypeConflictResolved),
		DstObjectID: loserID,
		DstType:     objectType,
		Weight:      1.0,
		CreatedTS:   now,
	})
	return winnerID, nil
}
