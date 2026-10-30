package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/chain"
)

// MaintenanceRequest is deliberately scoped. Selection and lifecycle decisions
// stay in Plasmod even when an external scheduler supplies the trigger.
type MaintenanceRequest struct {
	OperationID string             `json:"operation_id"`
	Operation   string             `json:"operation"`
	TenantID    string             `json:"tenant_id"`
	WorkspaceID string             `json:"workspace_id"`
	AgentID     string             `json:"agent_id,omitempty"`
	SessionID   string             `json:"session_id,omitempty"`
	MemoryIDs   []string           `json:"memory_ids,omitempty"`
	Signals     map[string]float64 `json:"signals,omitempty"`
	NowTS       string             `json:"now_ts,omitempty"`
	Limit       int                `json:"limit,omitempty"`
	AfterID     string             `json:"after_id,omitempty"`
}

type MaintenanceItem struct {
	MemoryID       string   `json:"memory_id"`
	Status         string   `json:"status"`
	Reason         string   `json:"reason"`
	Before         string   `json:"before,omitempty"`
	After          string   `json:"after,omitempty"`
	SourceEventIDs []string `json:"source_event_ids,omitempty"`
}

type MaintenanceReceipt struct {
	OperationID   string             `json:"operation_id"`
	Operation     string             `json:"operation"`
	ChainName     string             `json:"chain_name"`
	Status        string             `json:"status"`
	SelectedCount int                `json:"selected_count"`
	UpdatedCount  int                `json:"updated_count"`
	SkippedCount  int                `json:"skipped_count"`
	FailedCount   int                `json:"failed_count"`
	ProducedIDs   []string           `json:"produced_ids,omitempty"`
	Items         []MaintenanceItem  `json:"items"`
	NextCursor    string             `json:"next_cursor,omitempty"`
	HasMore       bool               `json:"has_more"`
	Request       MaintenanceRequest `json:"request"`
	UpdatedAt     string             `json:"updated_at"`
}

func stableID(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}
func operationStateID(tenant, workspace, id string) string {
	return "maintenance:" + stableID(tenant, workspace, id)
}

func (r *Runtime) MaintenanceStatus(tenant, workspace, id string) (MaintenanceReceipt, bool) {
	st, ok := r.storage.Objects().GetState(operationStateID(tenant, workspace, id))
	var out MaintenanceReceipt
	if !ok || json.Unmarshal([]byte(st.StateValue), &out) != nil {
		return out, false
	}
	return out, true
}
func (r *Runtime) saveMaintenance(out *MaintenanceReceipt) error {
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	req := out.Request
	st := schemas.State{StateID: operationStateID(req.TenantID, req.WorkspaceID, req.OperationID), TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID, StateType: "memory_maintenance", StateValue: string(data), CheckpointTS: out.UpdatedAt, Version: 1, Access: schemas.CanonicalAccess{TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, OwnerAgentID: req.AgentID, Visibility: "private"}}
	return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st})
}

func (r *Runtime) Maintain(ctx context.Context, req MaintenanceRequest) (MaintenanceReceipt, error) {
	r.maintenanceMu.Lock()
	defer r.maintenanceMu.Unlock()
	out := MaintenanceReceipt{OperationID: req.OperationID, Operation: req.Operation, ChainName: "memory_pipeline_chain", Status: "running", Request: req}
	if req.OperationID == "" || req.TenantID == "" || req.WorkspaceID == "" {
		return out, errors.New("operation_id, tenant_id and workspace_id are required")
	}
	switch req.Operation {
	case "ingest", "update", "decay", "compress", "summarize":
	default:
		return out, errors.New("unsupported maintenance operation")
	}
	if req.NowTS != "" {
		if _, err := time.Parse(time.RFC3339, req.NowTS); err != nil {
			return out, errors.New("now_ts must be RFC3339")
		}
	}
	if saved, ok := r.MaintenanceStatus(req.TenantID, req.WorkspaceID, req.OperationID); ok {
		if !reflect.DeepEqual(saved.Request, req) {
			return out, errors.New("operation_id already used with a different request")
		}
		if saved.Status != "running" {
			return saved, nil
		}
		// An interrupted dispatch may already have changed algorithm state. Never
		// silently replay an uncertain reinforcement; expose the recovery boundary.
		saved.Status = "partial"
		saved.Items = append(saved.Items, MaintenanceItem{Status: "failed", Reason: "interrupted; inspect objects and start a new operation"})
		saved.FailedCount++
		saveErr := r.saveMaintenance(&saved)
		return saved, saveErr
	}
	var memories []schemas.Memory
	if len(req.MemoryIDs) > 0 {
		seen := map[string]bool{}
		for _, id := range req.MemoryIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			m, ok := r.storage.Objects().GetMemory(id)
			if !ok {
				out.Items = append(out.Items, MaintenanceItem{MemoryID: id, Status: "skipped", Reason: "not_found_or_out_of_scope"})
				out.SkippedCount++
				continue
			}
			memories = append(memories, m)
		}
	} else {
		memories = r.storage.Objects().ListMemories(req.AgentID, req.SessionID)
	}
	sort.Slice(memories, func(i, j int) bool { return memories[i].MemoryID < memories[j].MemoryID })
	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var selected []schemas.Memory
	for _, m := range memories {
		if m.TenantID != req.TenantID || m.WorkspaceID != req.WorkspaceID || (req.AgentID != "" && m.AgentID != req.AgentID) || (req.SessionID != "" && m.SessionID != req.SessionID) {
			if len(req.MemoryIDs) > 0 {
				out.Items = append(out.Items, MaintenanceItem{MemoryID: m.MemoryID, Status: "skipped", Reason: "not_found_or_out_of_scope"})
				out.SkippedCount++
			}
			continue
		}
		if m.MemoryID <= req.AfterID {
			continue
		}
		if out.SelectedCount >= limit {
			out.HasMore = true
			break
		}
		out.SelectedCount++
		out.NextCursor = m.MemoryID
		reason := ""
		if !m.IsActive || r.isForgotten(m.MemoryID, m.TenantID, m.WorkspaceID) {
			reason = "inactive_or_forgotten"
		}
		if (req.Operation == "compress" || req.Operation == "summarize") && m.LifecycleState != "active" && m.LifecycleState != "reinforced" {
			reason = "requires_admitted_memory"
		}
		if reason != "" {
			out.Items = append(out.Items, MaintenanceItem{MemoryID: m.MemoryID, Status: "skipped", Reason: reason})
			out.SkippedCount++
			continue
		}
		selected = append(selected, m)
	}
	if err := r.saveMaintenance(&out); err != nil {
		return out, err
	}
	// Never combine distinct access descriptors into a more permissive summary.
	groups := map[string][]schemas.Memory{}
	var keys []string
	for _, m := range selected {
		key := m.MemoryID
		if req.Operation == "compress" || req.Operation == "summarize" {
			access, _ := json.Marshal(m.Access)
			key = stableID(m.TenantID, m.WorkspaceID, m.AgentID, m.SessionID, m.Scope, string(access), fmt.Sprint(m.TTL), m.ValidTo)
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], m)
	}
	for _, key := range keys {
		group := groups[key]
		ids := make([]string, 0, len(group))
		for _, m := range group {
			ids = append(ids, m.MemoryID)
		}
		var dispatched schemas.AlgorithmDispatchOutput
		err := ctx.Err()
		if err == nil {
			var result chain.ChainResult
			dispatched, result = r.memoryChain.RunAlgorithm(schemas.AlgorithmDispatchInput{Operation: req.Operation, MemoryIDs: ids, NowTS: req.NowTS, AgentID: group[0].AgentID, SessionID: group[0].SessionID, Signals: req.Signals}, func(op string, derived, sources []schemas.Memory) error {
				return r.persistDerivedMemoriesContext(ctx, op, derived, sources)
			})
			if !result.OK {
				dispatched.Error = result.Error
			}
			if dispatched.Error != "" {
				err = errors.New(dispatched.Error)
			}
		}
		if err == nil && r.MemoryBankMode {
			err = r.updateInferredProfile(req.TenantID, req.WorkspaceID, group[0].AgentID)
		}
		out.ProducedIDs = append(out.ProducedIDs, dispatched.ProducedIDs...)
		for _, before := range group {
			item := MaintenanceItem{MemoryID: before.MemoryID, Before: before.LifecycleState, SourceEventIDs: before.SourceEventIDs, Status: "updated", Reason: "algorithm_evaluated"}
			if err != nil {
				item.Status = "failed"
				item.Reason = err.Error()
				out.FailedCount++
			} else {
				after, _ := r.storage.Objects().GetMemory(before.MemoryID)
				item.After = after.LifecycleState
				if dispatched.UpdatedCount == 0 && len(dispatched.ProducedIDs) == 0 {
					item.Status = "skipped"
					item.Reason = "algorithm_no_change"
					out.SkippedCount++
				} else {
					out.UpdatedCount++
					if item.After != item.Before {
						item.Reason = "algorithm_lifecycle_transition"
						if raw, ok := r.storage.Objects().GetState("governance:" + after.AlgorithmStateRef + ":" + after.MemoryID); ok {
							var state schemas.MemoryAlgorithmState
							if json.Unmarshal([]byte(raw.StateValue), &state) == nil && state.AdmissionReason != "" {
								item.Reason = state.AdmissionReason
								item.SourceEventIDs = state.EvidenceEventIDs
							}
						}
					}
					if e := r.persistMemoryVersion(after, req.Operation); e != nil {
						item.Status = "failed"
						item.Reason = e.Error()
						out.UpdatedCount--
						out.FailedCount++
					}
				}
			}
			out.Items = append(out.Items, item)
		}
		if e := r.saveMaintenance(&out); e != nil {
			return out, e
		}
	}
	out.Status = "completed"
	if out.FailedCount > 0 {
		out.Status = "partial"
	}
	if out.SelectedCount == 0 {
		out.Status = "empty"
	}
	saveErr := r.saveMaintenance(&out)
	return out, saveErr
}

func (r *Runtime) persistMemoryVersion(m schemas.Memory, operation string) error {
	latest, ok := r.storage.Versions().LatestVersion(m.MemoryID)
	if ok && m.Version <= latest.Version {
		m.Version = latest.Version + 1
	}
	v := schemas.ObjectVersion{ObjectID: m.MemoryID, ObjectType: "memory", Version: m.Version, ValidFrom: time.Now().UTC().Format(time.RFC3339Nano), SnapshotTag: operation, Snapshot: canonicalSnapshot(m), Access: m.Access}
	return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{Memory: &m, Versions: []schemas.ObjectVersion{v}})
}

func (r *Runtime) persistDerivedMemories(operation string, derived, sources []schemas.Memory) error {
	return r.persistDerivedMemoriesContext(context.Background(), operation, derived, sources)
}
func (r *Runtime) persistDerivedMemoriesContext(ctx context.Context, operation string, derived, sources []schemas.Memory) error {
	if len(sources) == 0 {
		return errors.New("derived memory requires sources")
	}
	src := sources[0]
	for _, source := range sources {
		if r.ObjectForgotten(source.MemoryID) {
			return errors.New("cannot derive from forgotten source")
		}
	}
	for _, m := range sources {
		if m.TenantID != src.TenantID || m.WorkspaceID != src.WorkspaceID || m.AgentID != src.AgentID || m.SessionID != src.SessionID || m.Scope != src.Scope || !reflect.DeepEqual(m.Access, src.Access) {
			return errors.New("derived sources have different identity or access")
		}
	}
	for i := range derived {
		m := &derived[i]
		if saved, ok := r.storage.Objects().GetMemory(m.MemoryID); ok {
			*m = saved
		} else if err := r.summarizeContent(ctx, operation, m, sources); err != nil {
			return err
		}
		m.TenantID = src.TenantID
		m.WorkspaceID = src.WorkspaceID
		m.AgentID = src.AgentID
		m.SessionID = src.SessionID
		m.Scope = src.Scope
		m.Access = src.Access
		m.OwnerType = src.OwnerType
		m.PolicyTags = append([]string(nil), src.PolicyTags...)
		m.TTL = src.TTL
		if m.ValidTo == "" || (src.ValidTo != "" && src.ValidTo < m.ValidTo) {
			m.ValidTo = src.ValidTo
		}
		seen := map[string]bool{}
		m.SourceEventIDs = nil
		var sourceIDs []string
		for _, source := range sources {
			sourceIDs = append(sourceIDs, source.MemoryID)
			for _, id := range source.SourceEventIDs {
				if !seen[id] {
					m.SourceEventIDs = append(m.SourceEventIDs, id)
					seen[id] = true
				}
			}
		}
		eventID := "evt_derived_" + stableID(m.MemoryID)
		m.ProvenanceRef = eventID
		m.EmbeddingRef = "embedding:" + m.MemoryID
		ev := schemas.Event{Identity: schemas.EventIdentity{EventID: eventID, TenantID: m.TenantID, WorkspaceID: m.WorkspaceID}, Actor: schemas.EventActor{AgentID: m.AgentID, SessionID: m.SessionID}, Object: schemas.EventObject{ObjectID: m.MemoryID, ObjectType: "memory"}, EventInfo: schemas.EventDescriptor{EventType: "memory.derived", Importance: &m.Importance, Confidence: &m.Confidence}, Access: schemas.EventAccess{Visibility: m.Access.Visibility, VisibleToAgents: m.Access.VisibleToAgents, VisibleToRoles: m.Access.VisibleToRoles, PolicyTags: m.PolicyTags}, Causality: schemas.EventCausality{SourceObjectIDs: sourceIDs, EdgeKind: "derived_from"}, Payload: map[string]any{"text": m.Content, "operation": operation, "source_event_ids": m.SourceEventIDs}}
		ev = ev.NormalizeDynamicEventV04()
		mat := r.materializer.MaterializeEvent(ev)
		for _, sourceID := range sourceIDs {
			mat.Edges = append(mat.Edges, schemas.Edge{EdgeID: "edge_" + stableID(m.MemoryID, sourceID), SrcObjectID: m.MemoryID, SrcType: "memory", DstObjectID: sourceID, DstType: "memory", EdgeType: "derived_from", Weight: 1, ProvenanceRef: eventID})
		}
		mat.Version.Snapshot = canonicalSnapshot(*m)
		mat.Version.Version = m.Version
		mat.Version.Access = m.Access
		if err := r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{Event: &ev, Memory: m, Versions: []schemas.ObjectVersion{mat.Version}, Edges: mat.Edges, IncludeMemoryBaseEdges: true, IncludeEventBaseEdges: true}); err != nil {
			return err
		}
		r.stampEmbeddingSpec(&mat.Record)
		if err := r.plane.Ingest(mat.Record); err != nil {
			return err
		}
		r.nodeManager.DispatchIngest(mat.Record)
		r.flushDirty.Store(true)
	}
	return nil
}

// Forgotten markers survive canonical/WAL replay and gate all evidence reads.
func (r *Runtime) isForgotten(id, tenant, workspace string) bool {
	_, ok := r.storage.Objects().GetState("forgotten:" + stableID(tenant, workspace, id))
	return ok
}
