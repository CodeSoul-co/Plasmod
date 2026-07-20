package schemas

import "testing"

func TestCanonicalAccessFromEventDefaultsToNarrowestScopeAndCopiesGrants(t *testing.T) {
	event := Event{
		Identity: EventIdentity{TenantID: "tenant", WorkspaceID: "workspace"},
		Actor:    EventActor{AgentID: "owner", SessionID: "session", TeamID: "team"},
		Access: EventAccess{
			VisibleToAgents: []string{"reader"},
			VisibleToRoles:  []string{"critic"},
			PolicyTags:      []string{"pii"},
			ShareContractID: "contract-1",
		},
	}

	access := CanonicalAccessFromEvent(event)
	if access.Visibility != string(VisibilitySession) {
		t.Fatalf("default visibility = %q, want session", access.Visibility)
	}
	if access.TenantID != "tenant" || access.WorkspaceID != "workspace" || access.TeamID != "team" ||
		access.OwnerAgentID != "owner" || access.SessionID != "session" {
		t.Fatalf("canonical scope was not copied: %+v", access)
	}
	if access.ShareContractID != "contract-1" || access.VisibleToAgents[0] != "reader" ||
		access.VisibleToRoles[0] != "critic" || access.PolicyTags[0] != "pii" {
		t.Fatalf("canonical grants were not copied: %+v", access)
	}
	event.Access.VisibleToAgents[0] = "mutated"
	if access.VisibleToAgents[0] != "reader" {
		t.Fatal("canonical access aliases mutable event slices")
	}
}

func TestCanonicalStateIDIsStableAndScopeSafe(t *testing.T) {
	base := CanonicalStateID("tenant", "workspace", "agent", "session-a", "phase")
	if base == "" || base != CanonicalStateID("tenant", "workspace", "agent", "session-a", "phase") {
		t.Fatalf("state ID is not deterministic: %q", base)
	}
	for name, candidate := range map[string]string{
		"tenant":    CanonicalStateID("other", "workspace", "agent", "session-a", "phase"),
		"workspace": CanonicalStateID("tenant", "other", "agent", "session-a", "phase"),
		"agent":     CanonicalStateID("tenant", "workspace", "other", "session-a", "phase"),
		"session":   CanonicalStateID("tenant", "workspace", "agent", "session-b", "phase"),
		"key":       CanonicalStateID("tenant", "workspace", "agent", "session-a", "status"),
	} {
		if candidate == base {
			t.Fatalf("%s boundary collided with base state ID", name)
		}
	}
}

func TestCanonicalMemoryIDHonorsOnlyTypedExplicitIDs(t *testing.T) {
	typed := Event{
		Identity: EventIdentity{EventID: "event"},
		Object:   EventObject{ObjectID: "shared-memory", ObjectType: string(ObjectTypeMemory)},
	}
	if got := CanonicalMemoryID(typed); got != "shared-memory" {
		t.Fatalf("typed explicit memory ID = %q", got)
	}
	typed.Object.ObjectType = string(ObjectTypeArtifact)
	if got := CanonicalMemoryID(typed); got != "mem_event" {
		t.Fatalf("non-memory explicit ID changed memory identity: %q", got)
	}
}
