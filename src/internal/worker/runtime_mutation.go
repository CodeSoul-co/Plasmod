package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

type MemoryMutationRequest struct {
	OperationID  string         `json:"operation_id"`
	Operation    string         `json:"operation"`
	TenantID     string         `json:"tenant_id"`
	WorkspaceID  string         `json:"workspace_id"`
	ReferenceIDs []string       `json:"reference_ids"`
	Replacement  *schemas.Event `json:"replacement,omitempty"`
}
type MemoryMutationReceipt struct {
	OperationID   string                `json:"operation_id"`
	Operation     string                `json:"operation"`
	ChainName     string                `json:"chain_name"`
	Status        string                `json:"status"`
	ForgottenIDs  []string              `json:"forgotten_ids"`
	ReplacementID string                `json:"replacement_id,omitempty"`
	Error         string                `json:"error,omitempty"`
	Request       MemoryMutationRequest `json:"request"`
}

func mutationStateID(tenant, workspace, id string) string {
	return "mutation:" + stableID(tenant, workspace, id)
}
func (r *Runtime) MutationStatus(tenant, workspace, id string) (MemoryMutationReceipt, bool) {
	st, ok := r.storage.Objects().GetState(mutationStateID(tenant, workspace, id))
	var out MemoryMutationReceipt
	if !ok || json.Unmarshal([]byte(st.StateValue), &out) != nil {
		return out, false
	}
	return out, true
}
func (r *Runtime) saveMutation(out *MemoryMutationReceipt) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	req := out.Request
	st := schemas.State{StateID: mutationStateID(req.TenantID, req.WorkspaceID, req.OperationID), TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, StateType: "memory_mutation", StateValue: string(data), Version: 1, Access: schemas.CanonicalAccess{TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, Visibility: "private"}}
	return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st})
}

// MutateMemory is a recoverable workflow, not a claimed multi-store transaction.
// Markers are written first; retries continue the same plan and replacement ID.
func (r *Runtime) MutateMemory(ctx context.Context, req MemoryMutationRequest) (MemoryMutationReceipt, error) {
	r.maintenanceMu.Lock()
	defer r.maintenanceMu.Unlock()
	out := MemoryMutationReceipt{OperationID: req.OperationID, Operation: req.Operation, ChainName: "memory_pipeline_chain", Status: "running", Request: req}
	if req.TenantID == "" || req.WorkspaceID == "" || req.OperationID == "" || len(req.ReferenceIDs) == 0 {
		return out, errors.New("operation_id, tenant_id, workspace_id and reference_ids are required")
	}
	if req.Operation != "forget" && req.Operation != "correct" {
		return out, errors.New("operation must be forget or correct")
	}
	if req.Operation == "correct" && req.Replacement == nil {
		return out, errors.New("correct requires replacement event")
	}
	for _, id := range req.ReferenceIDs {
		if id == "" {
			return out, errors.New("reference_ids cannot contain empty IDs")
		}
	}
	if req.Replacement != nil {
		ev := req.Replacement.NormalizeDynamicEventV04()
		if (ev.Identity.TenantID != "" && ev.Identity.TenantID != req.TenantID) || (ev.Identity.WorkspaceID != "" && ev.Identity.WorkspaceID != req.WorkspaceID) {
			return out, errors.New("replacement scope differs from mutation scope")
		}
		if ev.Text() == "" {
			return out, errors.New("replacement text is required")
		}
	}
	if saved, ok := r.MutationStatus(req.TenantID, req.WorkspaceID, req.OperationID); ok {
		savedJSON, _ := json.Marshal(saved.Request)
		requestJSON, _ := json.Marshal(req)
		if !bytes.Equal(savedJSON, requestJSON) {
			return out, errors.New("operation_id already used with a different request")
		}
		if saved.Status == "completed" {
			return saved, nil
		}
		out = saved
		out.Error = ""
	}
	out.Status = "running"
	if err := r.saveMutation(&out); err != nil {
		return out, err
	}
	execute := func() error {
		forgotten := map[string]bool{}
		for _, id := range req.ReferenceIDs {
			forgotten[id] = true
			// Forgetting an extracted fact must also revoke its original utterance,
			// otherwise replay or another derivative can reintroduce the same fact.
			if m, ok := r.storage.Objects().GetMemory(id); ok && m.TenantID == req.TenantID && m.WorkspaceID == req.WorkspaceID {
				for _, source := range m.SourceEventIDs {
					forgotten[source] = true
				}
			}
		}
		// Traverse dependencies only, never session/ownership edges.
		changed := true
		for changed {
			changed = false
			for _, m := range r.storage.Objects().ListMemories("", "") {
				if m.TenantID != req.TenantID || m.WorkspaceID != req.WorkspaceID || forgotten[m.MemoryID] {
					continue
				}
				for _, src := range m.SourceEventIDs {
					if forgotten[src] {
						forgotten[m.MemoryID] = true
						changed = true
						break
					}
				}
				if forgotten[m.MemoryID] {
					continue
				}
				for _, e := range r.storage.Edges().EdgesFrom(m.MemoryID) {
					if (e.EdgeType == "derived_from" || e.EdgeType == "caused_by") && forgotten[e.DstObjectID] {
						forgotten[m.MemoryID] = true
						changed = true
						break
					}
				}
			}
			for _, raw := range r.storage.Objects().ListEvents("", "") {
				ev := raw.NormalizeDynamicEventV04()
				if ev.Identity.TenantID != req.TenantID || ev.Identity.WorkspaceID != req.WorkspaceID || forgotten[ev.Identity.EventID] {
					continue
				}
				if forgotten[schemas.CanonicalMemoryID(ev)] {
					forgotten[ev.Identity.EventID] = true
					changed = true
				}
			}
		}
		out.ForgottenIDs = nil
		for id := range forgotten {
			out.ForgottenIDs = append(out.ForgottenIDs, id)
		}
		sort.Strings(out.ForgottenIDs)
		// Save the plan before side effects so a crash yields a queryable receipt.
		if err := r.saveMutation(&out); err != nil {
			return err
		}
		for _, id := range out.ForgottenIDs {
			if err := ctx.Err(); err != nil {
				return err
			}
			st := schemas.State{StateID: "forgotten:" + stableID(req.TenantID, req.WorkspaceID, id), TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, StateType: "forgotten_source", StateKey: id, StateValue: req.OperationID, Version: 1, Access: schemas.CanonicalAccess{TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, Visibility: "private"}}
			if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st}); err != nil {
				return err
			}
			if m, ok := r.storage.Objects().GetMemory(id); ok && m.TenantID == req.TenantID && m.WorkspaceID == req.WorkspaceID {
				m.IsActive = false
				m.LifecycleState = "deleted_logically"
				m.ValidTo = time.Now().UTC().Format(time.RFC3339)
				if err := r.persistMemoryVersion(m, req.Operation); err != nil {
					return err
				}
				if r.tieredObjects != nil {
					r.tieredObjects.SoftDeleteMemoryTierCleanup(id)
				}
			}
		}
		if req.Operation == "correct" {
			ev := req.Replacement.NormalizeDynamicEventV04()
			ev.Identity.TenantID = req.TenantID
			ev.Identity.WorkspaceID = req.WorkspaceID
			ev.Identity.EventID = "evt_correction_" + stableID(req.TenantID, req.WorkspaceID, req.OperationID)
			ev.Object.ObjectID = ""
			ev.Object.ObjectType = "memory"
			ev.Access.Consistency = "strict"
			// Correction is a new assertion, not a derivation of the retracted text.
			ev.Causality = schemas.EventCausality{}
			ack, err := r.SubmitIngestContext(ctx, ev)
			if err != nil {
				return err
			}
			if id, ok := ack["memory_id"].(string); ok {
				out.ReplacementID = id
			}
		}
		return nil
	}
	// Governance mutations are owned by the memory chain as well.
	result := r.memoryChain.Mutate(execute)
	if !result.OK {
		out.Status = "partial"
		out.Error = result.Error
	} else {
		out.Status = "completed"
	}
	saveErr := r.saveMutation(&out)
	return out, saveErr
}

// ObjectForgotten covers raw events, derived memories and retained versions.
func (r *Runtime) ObjectForgotten(id string) bool {
	record := r.canonicalAccessRecord(id)
	if !record.exists {
		return false
	}
	return r.referencesForgotten(id, record.access.TenantID, record.access.WorkspaceID, map[string]bool{})
}
func (r *Runtime) referencesForgotten(id, tenant, workspace string, seen map[string]bool) bool {
	if r.isForgotten(id, tenant, workspace) {
		return true
	}
	if seen[id] {
		return false
	}
	seen[id] = true
	if state, ok := r.storage.Objects().GetState(id); ok && state.DerivedFromEventID != "" {
		if r.referencesForgotten(state.DerivedFromEventID, tenant, workspace, seen) {
			return true
		}
	}
	if raw, ok := r.storage.Objects().GetEvent(id); ok {
		ev := raw.NormalizeDynamicEventV04()
		refs := append([]string(nil), ev.Causality.SourceObjectIDs...)
		refs = append(refs, ev.Causality.SourceObjectID)
		refs = append(refs, ev.Causality.CausalRefs...)
		for _, ref := range refs {
			if ref != "" && r.referencesForgotten(ref, tenant, workspace, seen) {
				return true
			}
		}
		if source, ok := ev.Payload["source_id"].(string); ok && r.isForgotten(source, tenant, workspace) {
			return true
		}
	}
	if m, ok := r.storage.Objects().GetMemory(id); ok {
		for _, source := range m.SourceEventIDs {
			if r.referencesForgotten(source, tenant, workspace, seen) {
				return true
			}
		}
	}
	for _, e := range r.storage.Edges().EdgesFrom(id) {
		if e.EdgeType == "derived_from" || e.EdgeType == "caused_by" {
			if r.referencesForgotten(e.DstObjectID, tenant, workspace, seen) {
				return true
			}
		}
	}
	return false
}
