package schemas

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CanonicalAccess is the persisted access descriptor shared by canonical
// objects derived from an Event. Scope remains a compatibility field on
// Memory; this descriptor is the authoritative structured representation.
type CanonicalAccess struct {
	TenantID        string   `json:"tenant_id,omitempty"`
	WorkspaceID     string   `json:"workspace_id,omitempty"`
	TeamID          string   `json:"team_id,omitempty"`
	OwnerAgentID    string   `json:"owner_agent_id,omitempty"`
	SessionID       string   `json:"session_id,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	VisibleToAgents []string `json:"visible_to_agents,omitempty"`
	VisibleToRoles  []string `json:"visible_to_roles,omitempty"`
	PolicyTags      []string `json:"policy_tags,omitempty"`
	ShareContractID string   `json:"share_contract_id,omitempty"`
}

// CanonicalAccessFromEvent copies the Event access contract into an immutable
// canonical descriptor. When legacy input omits visibility, the narrowest
// useful scope present on the Event becomes the default.
func CanonicalAccessFromEvent(ev Event) CanonicalAccess {
	ev = ev.NormalizeDynamicEventV04()
	visibility := strings.TrimSpace(ev.Access.Visibility)
	if visibility == "" {
		switch {
		case ev.Actor.SessionID != "":
			visibility = string(VisibilitySession)
		case ev.Identity.WorkspaceID != "":
			visibility = string(VisibilityWorkspace)
		case ev.Actor.AgentID != "":
			visibility = string(VisibilityPrivate)
		default:
			visibility = string(VisibilityPublic)
		}
	}
	return CanonicalAccess{
		TenantID:        ev.Identity.TenantID,
		WorkspaceID:     ev.Identity.WorkspaceID,
		TeamID:          ev.Actor.TeamID,
		OwnerAgentID:    ev.Actor.AgentID,
		SessionID:       ev.Actor.SessionID,
		Visibility:      visibility,
		VisibleToAgents: cloneStringSlice(ev.Access.VisibleToAgents),
		VisibleToRoles:  cloneStringSlice(ev.Access.VisibleToRoles),
		PolicyTags:      cloneStringSlice(ev.Access.PolicyTags),
		ShareContractID: strings.TrimSpace(ev.Access.ShareContractID),
	}
}

// CanonicalStateID returns a stable, scope-safe ID for one mutable state key.
// The digest prevents collisions between sessions and unsafe key characters
// while remaining deterministic across retries and WAL replay.
func CanonicalStateID(tenantID, workspaceID, agentID, sessionID, stateKey string) string {
	identity := strings.Join([]string{
		strings.TrimSpace(tenantID),
		strings.TrimSpace(workspaceID),
		strings.TrimSpace(agentID),
		strings.TrimSpace(sessionID),
		strings.TrimSpace(stateKey),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return IDPrefixState + hex.EncodeToString(sum[:12])
}

// CanonicalMemoryID resolves the single canonical ID used by materialization,
// replay, and asynchronous maintenance. Explicit IDs are accepted only for a
// typed Memory object; ordinary Events retain the mem_<event_id> convention.
func CanonicalMemoryID(ev Event) string {
	ev = ev.NormalizeDynamicEventV04()
	if NormalizeObjectTypeName(ev.Object.ObjectType) == string(ObjectTypeMemory) && strings.TrimSpace(ev.Object.ObjectID) != "" {
		return strings.TrimSpace(ev.Object.ObjectID)
	}
	return IDPrefixMemory + ev.Identity.EventID
}

// AccessDecision explains why an object was visible to a query principal.
// Denied object decisions are intentionally not returned to clients because
// doing so would disclose object existence.
type AccessDecision struct {
	ObjectID        string `json:"object_id"`
	PrincipalID     string `json:"principal_id,omitempty"`
	Visibility      string `json:"visibility,omitempty"`
	Reason          string `json:"reason"`
	ShareContractID string `json:"share_contract_id,omitempty"`
	MutationLSN     int64  `json:"mutation_lsn,omitempty"`
}
