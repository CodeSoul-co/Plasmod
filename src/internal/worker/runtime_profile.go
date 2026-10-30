package worker

import (
	"encoding/json"
	"sort"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognitive/memorybank"
)

type inferredProfile struct {
	Profile         *memorybank.ProfileState `json:"profile"`
	SourceMemoryIDs []string                 `json:"source_memory_ids"`
	Inferred        bool                     `json:"inferred"`
}

// Profiles are derived records; they never write Agent.RoleProfile or user
// configuration. History and source IDs remain available for confirmation.
func (r *Runtime) updateInferredProfile(tenant, workspace, agent string) error {
	var memories []schemas.Memory
	for _, m := range r.storage.Objects().ListMemories(agent, "") {
		if m.TenantID != tenant || m.WorkspaceID != workspace || !m.IsActive || r.ObjectForgotten(m.MemoryID) || (m.LifecycleState != "active" && m.LifecycleState != "reinforced") {
			continue
		}
		if m.MemoryType == "profile" || m.MemoryType == "preference_constraint" {
			memories = append(memories, m)
		}
	}
	if len(memories) == 0 {
		return nil
	}
	sort.Slice(memories, func(i, j int) bool {
		if memories[i].ValidFrom == memories[j].ValidFrom {
			return memories[i].MemoryID < memories[j].MemoryID
		}
		return memories[i].ValidFrom < memories[j].ValidFrom
	})
	out := inferredProfile{Inferred: true}
	for _, m := range memories {
		out.SourceMemoryIDs = append(out.SourceMemoryIDs, m.MemoryID)
		out.Profile = memorybank.MergeProfile(out.Profile, memorybank.ExtractProfile([]schemas.Memory{m}))
	}
	data, _ := json.Marshal(out)
	id := "profile:" + stableID(tenant, workspace, agent)
	st := schemas.State{StateID: id, TenantID: tenant, WorkspaceID: workspace, AgentID: agent, StateType: "inferred_profile", StateValue: string(data), Version: 1, CheckpointTS: time.Now().UTC().Format(time.RFC3339Nano), Access: schemas.CanonicalAccess{TenantID: tenant, WorkspaceID: workspace, OwnerAgentID: agent, Visibility: "private"}}
	fingerprint, _ := json.Marshal(memories)
	st.StateKey = stableID(string(fingerprint))
	if old, ok := r.storage.Objects().GetState(id); ok {
		if old.StateKey == st.StateKey {
			return nil
		}
		st.Version = old.Version + 1
	}
	var edges []schemas.Edge
	for _, m := range memories {
		edges = append(edges, schemas.Edge{EdgeID: "edge_" + stableID(id, m.MemoryID), SrcObjectID: id, SrcType: "agent_state", DstObjectID: m.MemoryID, DstType: "memory", EdgeType: "derived_from", Weight: 1})
	}
	version := schemas.ObjectVersion{ObjectID: id, ObjectType: "agent_state", Version: st.Version, ValidFrom: st.CheckpointTS, SnapshotTag: "inferred_profile", Snapshot: canonicalSnapshot(st), Access: st.Access}
	return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st, Versions: []schemas.ObjectVersion{version}, Edges: edges})
}
