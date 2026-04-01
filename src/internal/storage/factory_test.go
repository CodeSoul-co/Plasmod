package storage

import (
	"os"
	"testing"

	"andb/src/internal/schemas"
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

func TestBuildRuntime_hybridObjectsDiskRestMemory(t *testing.T) {
	t.Setenv(EnvStorage, "hybrid")
	t.Setenv(EnvDataDir, t.TempDir())
	t.Setenv(EnvBadgerInMemory, "true")
	t.Setenv(EnvStoreObjects, "disk")
	t.Setenv(EnvStoreEdges, "memory")
	bundle, err := buildRuntime(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bundle.Close() }()
	if bundle.Config.Stores["objects"] != backendDisk {
		t.Fatalf("objects: %s", bundle.Config.Stores["objects"])
	}
	if bundle.Config.Stores["edges"] != backendMemory {
		t.Fatalf("edges: %s", bundle.Config.Stores["edges"])
	}
	obj := bundle.RuntimeStorage.Objects()
	obj.PutMemory(schemasMemoryFixture())
	if len(obj.ListMemories("", "")) != 1 {
		t.Fatal("expected 1 memory on badger object store")
	}
	edg := bundle.RuntimeStorage.Edges()
	edg.PutEdge(schemasEdgeFixture())
	if len(edg.ListEdges()) != 1 {
		t.Fatal("expected 1 edge on memory edge store")
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
