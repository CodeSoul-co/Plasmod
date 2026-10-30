package worker

import (
	"encoding/json"
	"errors"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

type ConflictResolutionRequest struct {
	OperationID string `json:"operation_id"`
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
	WinnerID    string `json:"winner_id"`
	LoserID     string `json:"loser_id"`
	Kind        string `json:"kind"` // factual_conflict or preference_change
}

func (r *Runtime) ResolveMemoryConflict(req ConflictResolutionRequest) (map[string]any, error) {
	r.maintenanceMu.Lock()
	defer r.maintenanceMu.Unlock()
	if req.OperationID == "" || req.TenantID == "" || req.WorkspaceID == "" || req.WinnerID == "" || req.LoserID == "" || req.WinnerID == req.LoserID {
		return nil, errors.New("operation_id, scope and distinct winner_id/loser_id are required")
	}
	if req.Kind != "factual_conflict" && req.Kind != "preference_change" {
		return nil, errors.New("kind must be factual_conflict or preference_change")
	}
	encoded, _ := json.Marshal(req)
	key := "conflict_resolution:" + stableID(req.TenantID, req.WorkspaceID, req.OperationID)
	if saved, ok := r.storage.Objects().GetState(key); ok {
		if saved.StateValue != string(encoded) {
			return nil, errors.New("operation_id reused")
		}
		if saved.StateKey == "completed" {
			return map[string]any{"status": "completed", "operation_id": req.OperationID, "winner_id": req.WinnerID, "chain_name": "collaboration_chain"}, nil
		}
	}
	winner, wok := r.storage.Objects().GetMemory(req.WinnerID)
	loser, lok := r.storage.Objects().GetMemory(req.LoserID)
	if !wok || !lok || winner.TenantID != req.TenantID || loser.TenantID != req.TenantID || winner.WorkspaceID != req.WorkspaceID || loser.WorkspaceID != req.WorkspaceID || winner.AgentID != loser.AgentID || r.ObjectForgotten(req.WinnerID) || r.ObjectForgotten(req.LoserID) {
		return nil, errors.New("conflict objects missing or out of scope")
	}
	st := schemas.State{StateID: key, TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, AgentID: winner.AgentID, StateType: "conflict_resolution", StateValue: string(encoded), StateKey: "running", Access: winner.Access, Version: 1}
	if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st}); err != nil {
		return nil, err
	}
	result := r.collaborationChain.Decide(func() error {
		now := time.Now().UTC().Format(time.RFC3339)
		loser.IsActive = false
		loser.LifecycleState = "hidden"
		loser.ValidTo = now
		if err := r.persistMemoryVersion(loser, req.Kind); err != nil {
			return err
		}
		winner.IsActive = true
		winner.LifecycleState = "active"
		if err := r.persistMemoryVersion(winner, req.Kind); err != nil {
			return err
		}
		for _, m := range []schemas.Memory{winner, loser} {
			raw, ok := r.storage.Objects().GetState("governance:memorybank_v1:" + m.MemoryID)
			if !ok {
				continue
			}
			var state schemas.MemoryAlgorithmState
			if err := json.Unmarshal([]byte(raw.StateValue), &state); err != nil {
				return err
			}
			state.SuggestedLifecycleState = m.LifecycleState
			state.UserConfirmed = m.MemoryID == req.WinnerID
			state.AdmissionReason = "user_resolved_" + req.Kind
			state.UpdatedAt = now
			data, _ := json.Marshal(state)
			raw.StateValue = string(data)
			if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &raw}); err != nil {
				return err
			}
			r.storage.AlgorithmStates().PutAlgorithmState(state)
		}
		edge := schemas.Edge{EdgeID: "edge_" + stableID(key), SrcObjectID: req.WinnerID, SrcType: "memory", DstObjectID: req.LoserID, DstType: "memory", EdgeType: "conflict_resolved", CreatedTS: now, Properties: map[string]any{"kind": req.Kind, "operation_id": req.OperationID}, Access: winner.Access}
		if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{Edges: []schemas.Edge{edge}}); err != nil {
			return err
		}
		return r.updateInferredProfile(req.TenantID, req.WorkspaceID, winner.AgentID)
	})
	status := "completed"
	if !result.OK {
		status = "partial"
	}
	st.StateKey = status
	if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st}); err != nil {
		return nil, err
	}
	return map[string]any{"status": status, "operation_id": req.OperationID, "winner_id": req.WinnerID, "chain_name": "collaboration_chain", "error": result.Error}, nil
}
