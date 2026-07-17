package storage

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

func newBadgerProjectionStorage(t *testing.T) RuntimeStorage {
	t.Helper()
	db, err := openBadgerInMemory()
	if err != nil {
		t.Fatalf("openBadgerInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewCompositeRuntimeStorage(
		newBadgerSegmentStore(db),
		newBadgerIndexStore(db),
		newBadgerObjectStore(db),
		newBadgerGraphEdgeStore(db),
		newBadgerSnapshotVersionStore(db),
		newBadgerPolicyStore(db),
		newBadgerShareContractStore(db),
		NewHotObjectCache(16),
	)
}

func TestBadgerCanonicalProjectionSoak(t *testing.T) {
	rawEvents := os.Getenv("PLASMOD_CANONICAL_PROJECTION_SOAK_EVENTS")
	if rawEvents == "" {
		t.Skip("set PLASMOD_CANONICAL_PROJECTION_SOAK_EVENTS to run the disk soak")
	}
	events, err := strconv.Atoi(rawEvents)
	if err != nil || events <= 0 {
		t.Fatalf("invalid PLASMOD_CANONICAL_PROJECTION_SOAK_EVENTS=%q", rawEvents)
	}
	payloadBytes := 1024
	if rawPayloadBytes := os.Getenv("PLASMOD_CANONICAL_PROJECTION_SOAK_PAYLOAD_BYTES"); rawPayloadBytes != "" {
		payloadBytes, err = strconv.Atoi(rawPayloadBytes)
		if err != nil || payloadBytes <= 0 {
			t.Fatalf("invalid PLASMOD_CANONICAL_PROJECTION_SOAK_PAYLOAD_BYTES=%q", rawPayloadBytes)
		}
	}
	db, err := openBadger(t.TempDir())
	if err != nil {
		t.Fatalf("openBadger: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := NewCompositeRuntimeStorage(
		newBadgerSegmentStore(db),
		newBadgerIndexStore(db),
		newBadgerObjectStore(db),
		newBadgerGraphEdgeStore(db),
		newBadgerSnapshotVersionStore(db),
		newBadgerPolicyStore(db),
		newBadgerShareContractStore(db),
		NewHotObjectCache(16),
	)

	rng := rand.New(rand.NewSource(7))
	payload := make([]byte, payloadBytes)
	var maxLatency time.Duration
	var over250ms int
	started := time.Now()
	for index := 0; index < events; index++ {
		for offset := range payload {
			payload[offset] = byte(32 + rng.Intn(95))
		}
		memoryID := fmt.Sprintf("memory-soak-%09d", index)
		eventID := fmt.Sprintf("event-soak-%09d", index)
		memory := schemas.Memory{
			MemoryID:       memoryID,
			AgentID:        fmt.Sprintf("agent-%03d", index%128),
			SessionID:      fmt.Sprintf("session-%05d", index%8192),
			Content:        string(payload),
			SourceEventIDs: []string{eventID},
			Version:        int64(index + 1),
			IsActive:       true,
		}
		projection := CanonicalProjection{
			Memory:                 &memory,
			IncludeMemoryBaseEdges: true,
			Versions: []schemas.ObjectVersion{{
				ObjectID:   memoryID,
				ObjectType: string(schemas.ObjectTypeMemory),
				Version:    int64(index + 1),
			}},
			Edges: []schemas.Edge{
				{EdgeID: fmt.Sprintf("%s%s_%s_%s", schemas.IDPrefixEdge, memoryID, schemas.EdgeTypeBelongsToSession, memory.SessionID), SrcObjectID: memoryID, EdgeType: string(schemas.EdgeTypeBelongsToSession), DstObjectID: memory.SessionID},
				{EdgeID: fmt.Sprintf("%s%s_%s_%s", schemas.IDPrefixEdge, memoryID, schemas.EdgeTypeOwnedByAgent, memory.AgentID), SrcObjectID: memoryID, EdgeType: string(schemas.EdgeTypeOwnedByAgent), DstObjectID: memory.AgentID},
				{EdgeID: "edge-caused-" + memoryID, SrcObjectID: memoryID, EdgeType: string(schemas.EdgeTypeCausedBy), DstObjectID: eventID},
			},
		}
		operationStarted := time.Now()
		if err := store.ApplyCanonicalProjection(projection); err != nil {
			t.Fatalf("projection %d: %v", index, err)
		}
		latency := time.Since(operationStarted)
		if latency > maxLatency {
			maxLatency = latency
		}
		if latency > 250*time.Millisecond {
			over250ms++
		}
	}
	t.Logf("events=%d payload_bytes=%d elapsed=%s max_projection=%s over_250ms=%d", events, payloadBytes, time.Since(started), maxLatency, over250ms)
	if maxLatency > time.Second {
		t.Fatalf("maximum canonical projection latency %s exceeded 1s", maxLatency)
	}
}

func TestBadgerOptionsSeparateValuesWithoutExpandingMemoryBuffers(t *testing.T) {
	opts := badgerOptions(t.TempDir())
	if opts.NumMemtables != 5 {
		t.Fatalf("NumMemtables = %d, want Badger default 5", opts.NumMemtables)
	}
	if opts.NumLevelZeroTables != 5 {
		t.Fatalf("NumLevelZeroTables = %d, want Badger default 5", opts.NumLevelZeroTables)
	}
	if opts.NumLevelZeroTablesStall != 15 {
		t.Fatalf("NumLevelZeroTablesStall = %d, want Badger default 15", opts.NumLevelZeroTablesStall)
	}
	if opts.ValueThreshold > 1024 {
		t.Fatalf("ValueThreshold = %d, want at most 1024", opts.ValueThreshold)
	}
}

func TestBadgerValueThresholdCanBeConfigured(t *testing.T) {
	get := func(key string) string {
		if key == EnvBadgerValueThreshold {
			return "4096"
		}
		return ""
	}
	if got := badgerValueThreshold(get); got != 4096 {
		t.Fatalf("ValueThreshold = %d, want 4096", got)
	}
}

func TestCompositeRuntimeStorageApplyCanonicalProjection(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	memory := schemas.Memory{
		MemoryID:       "memory-1",
		AgentID:        "agent-1",
		SessionID:      "session-1",
		SourceEventIDs: []string{"event-1"},
		IsActive:       true,
	}
	version := schemas.ObjectVersion{
		ObjectID:   memory.MemoryID,
		ObjectType: string(schemas.ObjectTypeMemory),
		Version:    1,
	}
	artifact := schemas.Artifact{
		ArtifactID:        "artifact-1",
		SessionID:         memory.SessionID,
		OwnerAgentID:      memory.AgentID,
		ProducedByEventID: "event-1",
	}
	edge := schemas.Edge{
		EdgeID:      "edge-custom",
		SrcObjectID: memory.MemoryID,
		DstObjectID: artifact.ArtifactID,
		EdgeType:    string(schemas.EdgeTypeGroundedOnResource),
	}

	err := store.ApplyCanonicalProjection(CanonicalProjection{
		Memory:                   &memory,
		Artifact:                 &artifact,
		Versions:                 []schemas.ObjectVersion{version},
		Edges:                    []schemas.Edge{edge},
		IncludeMemoryBaseEdges:   true,
		IncludeArtifactBaseEdges: true,
	})
	if err != nil {
		t.Fatalf("ApplyCanonicalProjection: %v", err)
	}
	if _, ok := store.Objects().GetMemory(memory.MemoryID); !ok {
		t.Fatal("canonical memory was not persisted")
	}
	if _, ok := store.Objects().GetArtifact(artifact.ArtifactID); !ok {
		t.Fatal("canonical artifact was not persisted")
	}
	if got := store.Versions().GetVersions(memory.MemoryID); len(got) != 1 {
		t.Fatalf("memory versions = %d, want 1", len(got))
	}
	if _, ok := store.Edges().GetEdge(edge.EdgeID); !ok {
		t.Fatal("explicit edge was not persisted")
	}
	for _, baseEdge := range append(schemas.BuildMemoryBaseEdges(memory), schemas.BuildArtifactBaseEdges(artifact)...) {
		if _, ok := store.Edges().GetEdge(baseEdge.EdgeID); !ok {
			t.Fatalf("base edge %q was not persisted", baseEdge.EdgeID)
		}
	}
}

func TestCompositeRuntimeStorageApplyCanonicalProjectionIsAtomic(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	memory := schemas.Memory{MemoryID: "memory-rollback", IsActive: true}
	invalidEdge := schemas.Edge{
		EdgeID:      "edge-invalid",
		SrcObjectID: memory.MemoryID,
		DstObjectID: "event-rollback",
		Properties:  map[string]any{"invalid": make(chan int)},
	}

	err := store.ApplyCanonicalProjection(CanonicalProjection{
		Memory: &memory,
		Edges:  []schemas.Edge{invalidEdge},
	})
	if err == nil {
		t.Fatal("ApplyCanonicalProjection succeeded with an unserializable edge")
	}
	if _, ok := store.Objects().GetMemory(memory.MemoryID); ok {
		t.Fatal("memory remained after the projection transaction failed")
	}
	if _, ok := store.Edges().GetEdge(invalidEdge.EdgeID); ok {
		t.Fatal("edge remained after the projection transaction failed")
	}
}

func TestCanonicalProjectionDeduplicatesEquivalentRelations(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	memory := schemas.Memory{
		MemoryID:       "memory-dedup",
		AgentID:        "agent-dedup",
		SessionID:      "session-dedup",
		SourceEventIDs: []string{"event-dedup"},
	}
	duplicateSession := schemas.Edge{
		EdgeID:      schemas.BuildMemoryBaseEdges(memory)[0].EdgeID,
		SrcObjectID: memory.MemoryID,
		EdgeType:    string(schemas.EdgeTypeBelongsToSession),
		DstObjectID: memory.SessionID,
		CreatedTS:   "2026-07-17T00:00:00Z",
	}
	duplicateAgent := schemas.Edge{
		EdgeID:      schemas.BuildMemoryBaseEdges(memory)[1].EdgeID,
		SrcObjectID: memory.MemoryID,
		EdgeType:    string(schemas.EdgeTypeOwnedByAgent),
		DstObjectID: memory.AgentID,
	}
	causedBy := schemas.Edge{
		EdgeID:      "edge-caused-by",
		SrcObjectID: memory.MemoryID,
		EdgeType:    string(schemas.EdgeTypeCausedBy),
		DstObjectID: "event-dedup",
	}

	err := store.ApplyCanonicalProjection(CanonicalProjection{
		Memory:                 &memory,
		IncludeMemoryBaseEdges: true,
		Edges:                  []schemas.Edge{duplicateSession, duplicateAgent, causedBy},
	})
	if err != nil {
		t.Fatalf("ApplyCanonicalProjection: %v", err)
	}
	if got := len(store.Edges().ListEdges()); got != 4 {
		t.Fatalf("persisted edges = %d, want 4 unique relations", got)
	}
	if _, ok := store.Edges().GetEdge(duplicateSession.EdgeID); !ok {
		t.Fatal("merged session base relation was not persisted")
	}
	if _, ok := store.Edges().GetEdge(duplicateAgent.EdgeID); !ok {
		t.Fatal("merged agent base relation was not persisted")
	}
	if _, ok := store.Edges().GetEdge(causedBy.EdgeID); !ok {
		t.Fatal("distinct caused-by relation was dropped")
	}
	baseSession := schemas.BuildMemoryBaseEdges(memory)[0]
	if baseSession.CreatedTS == "" {
		persisted, ok := store.Edges().GetEdge(baseSession.EdgeID)
		if !ok || persisted.CreatedTS == "" {
			t.Fatal("base relation did not retain metadata from the materialized duplicate")
		}
	}
}

func TestCanonicalProjectionPreservesDistinctParallelEdges(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	memory := schemas.Memory{MemoryID: "src", SessionID: "dst"}
	edges := []schemas.Edge{
		{EdgeID: "parallel-1", SrcObjectID: "src", EdgeType: string(schemas.EdgeTypeBelongsToSession), DstObjectID: "dst", Weight: 0.2},
		{EdgeID: "parallel-2", SrcObjectID: "src", EdgeType: string(schemas.EdgeTypeBelongsToSession), DstObjectID: "dst", Weight: 0.8},
	}
	if err := store.ApplyCanonicalProjection(CanonicalProjection{Memory: &memory, IncludeMemoryBaseEdges: true, Edges: edges}); err != nil {
		t.Fatalf("ApplyCanonicalProjection: %v", err)
	}
	if got := len(store.Edges().EdgesFrom("src")); got != 3 {
		t.Fatalf("base plus parallel edges = %d, want 3", got)
	}
}

func TestBadgerGraphEdgeStorePutEdgeReplacesIndexes(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	first := schemas.Edge{EdgeID: "public-moving-edge", SrcObjectID: "old-src", EdgeType: "related", DstObjectID: "old-dst"}
	second := schemas.Edge{EdgeID: "public-moving-edge", SrcObjectID: "new-src", EdgeType: "related", DstObjectID: "new-dst"}
	store.Edges().PutEdge(first)
	store.Edges().PutEdge(second)
	if got := store.Edges().EdgesFrom("old-src"); len(got) != 0 {
		t.Fatalf("old public source index retained %d edges", len(got))
	}
	if got := store.Edges().EdgesTo("old-dst"); len(got) != 0 {
		t.Fatalf("old public destination index retained %d edges", len(got))
	}
}

func TestBadgerCanonicalProjectionEdgeUpsertReplacesIndexes(t *testing.T) {
	store := newBadgerProjectionStorage(t)
	first := schemas.Edge{EdgeID: "moving-edge", SrcObjectID: "old-src", EdgeType: "related", DstObjectID: "old-dst"}
	second := schemas.Edge{EdgeID: "moving-edge", SrcObjectID: "new-src", EdgeType: "related", DstObjectID: "new-dst"}
	if err := store.ApplyCanonicalProjection(CanonicalProjection{Edges: []schemas.Edge{first}}); err != nil {
		t.Fatalf("first projection: %v", err)
	}
	if err := store.ApplyCanonicalProjection(CanonicalProjection{Edges: []schemas.Edge{second}}); err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if got := store.Edges().EdgesFrom("old-src"); len(got) != 0 {
		t.Fatalf("old source index retained %d edges", len(got))
	}
	if got := store.Edges().EdgesTo("old-dst"); len(got) != 0 {
		t.Fatalf("old destination index retained %d edges", len(got))
	}
	if got := store.Edges().EdgesFrom("new-src"); len(got) != 1 || got[0].DstObjectID != "new-dst" {
		t.Fatalf("new source index = %#v", got)
	}
}

func TestCompositeCanonicalProjectionRejectsMixedDurabilityBoundary(t *testing.T) {
	db, err := openBadgerInMemory()
	if err != nil {
		t.Fatalf("openBadgerInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	memory := NewMemoryRuntimeStorage()
	store := NewCompositeRuntimeStorage(
		memory.Segments(), memory.Indexes(), newBadgerObjectStore(db), memory.Edges(),
		newBadgerSnapshotVersionStore(db), memory.Policies(), memory.Contracts(), memory.HotCache(),
	)
	err = store.ApplyCanonicalProjection(CanonicalProjection{Memory: &schemas.Memory{MemoryID: "mixed"}})
	if err == nil {
		t.Fatal("mixed canonical durability boundary was accepted")
	}
}
