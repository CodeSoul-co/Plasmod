package coordinator

import (
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

func TestObjectCoordinatorPersistsRecoverableVersionBoundaries(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	coordinator := NewObjectCoordinator(store.Objects(), store.Versions())
	access := schemas.CanonicalAccess{TenantID: "tenant", OwnerAgentID: "agent", Visibility: string(schemas.VisibilityPrivate)}

	coordinator.PutMemory(schemas.Memory{
		MemoryID: "memory", AgentID: "agent", Content: "v1", IsActive: true, Access: access,
	}, "event-v1")
	coordinator.PutMemory(schemas.Memory{
		MemoryID: "memory", AgentID: "agent", Content: "v2", IsActive: true, Access: access,
	}, "event-v2")

	memory, ok := store.Objects().GetMemory("memory")
	if !ok || memory.Version != 2 || memory.Content != "v2" {
		t.Fatalf("latest canonical memory is incorrect: %+v", memory)
	}
	versions := store.Versions().GetVersions("memory")
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2: %+v", len(versions), versions)
	}
	if versions[0].ValidTo == "" || versions[1].ValidFrom == "" {
		t.Fatalf("version validity interval missing: %+v", versions)
	}
	if versions[0].Snapshot["content"] != "v1" || versions[1].Snapshot["content"] != "v2" {
		t.Fatalf("version snapshots are not recoverable: %+v", versions)
	}
	if versions[1].Access.TenantID != "tenant" || versions[1].MutationEventID != "event-v2" {
		t.Fatalf("version mutation metadata missing: %+v", versions[1])
	}
}
