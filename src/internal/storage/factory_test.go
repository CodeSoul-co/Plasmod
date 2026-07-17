package storage

import (
	"os"
	"strings"
	"testing"

	"plasmod/src/internal/schemas"
)

func TestBuildRuntime_defaultIsDisk(t *testing.T) {
	t.Setenv(EnvStorage, "")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true") // keep test fast: Badger RAM mode
	bundle, err := buildRuntime(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
	if !bundle.Config.BadgerEnabled {
		t.Fatalf("expected badger enabled for default disk mode")
	}
}

func TestBuildRuntime_diskAllBadger(t *testing.T) {
	t.Setenv(EnvStorage, "disk")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true")
	bundle, err := buildRuntime(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
	if !bundle.Config.BadgerEnabled {
		t.Fatal("expected BadgerEnabled")
	}
	for k, v := range bundle.Config.Stores {
		if v != backendDisk {
			t.Fatalf("store %s = %s want disk", k, v)
		}
	}
	obj := bundle.RuntimeStorage.Objects()
	obj.PutAgent(schemasAgentFixture())
	if len(obj.ListAgents()) != 1 {
		t.Fatalf("agents: %d", len(obj.ListAgents()))
	}
}

func TestBadgerSnapshotVersionStore_PutVersionIsIdempotent(t *testing.T) {
	t.Setenv(EnvStorage, "disk")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true")
	bundle, err := buildRuntime(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()

	store := bundle.RuntimeStorage.Versions()
	version := schemas.ObjectVersion{
		ObjectID: "mem-1", Version: 2, MutationEventID: "evt-1", ValidFrom: "first",
	}
	store.PutVersion(version)
	store.PutVersion(version)

	got := store.GetVersions(version.ObjectID)
	if len(got) != 1 {
		t.Fatalf("versions after retry = %d, want 1: %+v", len(got), got)
	}
}

func TestBuildRuntime_rejectsMixedCanonicalStores(t *testing.T) {
	t.Setenv(EnvStorage, "hybrid")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true")
	t.Setenv(EnvStoreObjects, "disk")
	t.Setenv(EnvStoreEdges, "memory")
	_, err := buildRuntime(os.Getenv)
	if err == nil || !strings.Contains(err.Error(), "objects, edges, and versions") {
		t.Fatalf("buildRuntime error = %v, want canonical store boundary error", err)
	}
}

func TestBuildRuntime_explicitDiskOverridesMemoryMode(t *testing.T) {
	t.Setenv(EnvStorage, "memory")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true")
	t.Setenv(EnvStoreSegments, "disk")
	bundle, err := buildRuntime(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
	if !bundle.Config.BadgerEnabled {
		t.Fatal("expected badger when one store is disk")
	}
	if bundle.Config.Stores["segments"] != backendDisk {
		t.Fatalf("segments: %s", bundle.Config.Stores["segments"])
	}
	if bundle.Config.Stores["objects"] != backendMemory {
		t.Fatalf("objects should stay memory: %s", bundle.Config.Stores["objects"])
	}
}

func schemasAgentFixture() schemas.Agent {
	return schemas.Agent{
		AgentID: "a1", TenantID: "t", WorkspaceID: "w", Status: "active",
	}
}

func schemasMemoryFixture() schemas.Memory {
	return schemas.Memory{
		MemoryID: "m1", AgentID: "a1", SessionID: "s1", Content: "x",
	}
}

func schemasEdgeFixture() schemas.Edge {
	return schemas.Edge{
		EdgeID: "e1", SrcObjectID: "a", DstObjectID: "b", EdgeType: "rel",
	}
}
