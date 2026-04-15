package evidence

import (
	"testing"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

func TestAssembler_Build_Basic(t *testing.T) {
	a := NewAssembler()
	input := dataplane.SearchInput{TopK: 10}
	result := dataplane.SearchOutput{
		ObjectIDs:       []string{"mem_1", "mem_2"},
		ScannedSegments: []string{"shard_a"},
		Tier:            "warm",
	}
	resp := a.Build(input, result, []string{"visibility:private"})

	if len(resp.Objects) != 2 {
		t.Errorf("Build: Objects len: want 2, got %d", len(resp.Objects))
	}
	if len(resp.AppliedFilters) != 1 {
		t.Errorf("Build: AppliedFilters len: want 1, got %d", len(resp.AppliedFilters))
	}
	if len(resp.ProofTrace) == 0 {
		t.Error("Build: ProofTrace should not be empty")
	}

	tierFound := false
	for _, step := range resp.ProofTrace {
		if step.StepType == "tier" && step.Description == "tier:warm" {
			tierFound = true
		}
	}
	if !tierFound {
		t.Errorf("Build: ProofTrace should contain structured tier:warm step, got: %+v", resp.ProofTrace)
	}
}

func TestCachedAssembler_FragmentMerge(t *testing.T) {
	cache := NewCache(100)
	a := NewCachedAssembler(cache)

	cache.Put(EvidenceFragment{
		ObjectID:      "mem_1",
		SalienceScore: 0.9,
		Level:         1,
	})

	input := dataplane.SearchInput{TopK: 5}
	result := dataplane.SearchOutput{
		ObjectIDs: []string{"mem_1"},
		Tier:      "hot",
	}
	resp := a.Build(input, result, nil)

	fragFound := false
	for _, step := range resp.ProofTrace {
		if step.StepType == "fragment" && step.Operation == "fragment_cache" {
			fragFound = true
		}
	}
	if !fragFound {
		t.Errorf("CachedAssembler: expected structured fragment step in ProofTrace after cache hit, got: %+v", resp.ProofTrace)
	}
}

func TestEvidenceCache_PutAndGet(t *testing.T) {
	c := NewCache(10)

	frag := EvidenceFragment{
		ObjectID:      "obj_1",
		ObjectType:    "memory",
		SalienceScore: 0.75,
		TextTokens:    []string{"hello", "world"},
	}
	c.Put(frag)

	got, ok := c.Get("obj_1")
	if !ok {
		t.Fatal("Cache.Get: expected to find obj_1")
	}
	if got.SalienceScore != 0.75 {
		t.Errorf("SalienceScore: want 0.75, got %f", got.SalienceScore)
	}
}

func TestEvidenceCache_GetMany(t *testing.T) {
	c := NewCache(100)
	c.Put(EvidenceFragment{ObjectID: "a", SalienceScore: 0.5})
	c.Put(EvidenceFragment{ObjectID: "b", SalienceScore: 0.6})

	frags := c.GetMany([]string{"a", "b", "missing"})
	// GetMany always returns len(input) entries; missing ones have ObjectID == "".
	if len(frags) != 3 {
		t.Errorf("GetMany: want 3 entries (same length as input), got %d", len(frags))
	}
	hits := 0
	for _, f := range frags {
		if f.ObjectID != "" {
			hits++
		}
	}
	if hits != 2 {
		t.Errorf("GetMany: want 2 populated fragments, got %d", hits)
	}
}

func TestAssembler_ObjectTypesFilter(t *testing.T) {
	a := NewAssembler()
	input := dataplane.SearchInput{
		TopK:        10,
		ObjectTypes: []string{"memory"},
	}
	result := dataplane.SearchOutput{
		ObjectIDs: []string{"mem_1", "state_x", "art_y", "mem_2"},
		Tier:      "warm",
	}
	resp := a.Build(input, result, nil)

	if len(resp.Objects) != 2 {
		t.Errorf("ObjectTypesFilter: want 2 memory objects, got %d: %v", len(resp.Objects), resp.Objects)
	}
	for _, id := range resp.Objects {
		if id != "mem_1" && id != "mem_2" {
			t.Errorf("ObjectTypesFilter: unexpected non-memory object in result: %s", id)
		}
	}

	filterFound := false
	for _, step := range resp.ProofTrace {
		if step.StepType == "filter" && step.Operation == "object_type_filter" {
			filterFound = true
		}
	}
	if !filterFound {
		t.Errorf("ObjectTypesFilter: expected structured object_type_filter step in ProofTrace, got: %+v", resp.ProofTrace)
	}
}

func TestAssembler_StateTypeFilter(t *testing.T) {
	a := NewAssembler()
	input := dataplane.SearchInput{
		TopK:        10,
		ObjectTypes: []string{"state"},
	}
	result := dataplane.SearchOutput{
		ObjectIDs: []string{"mem_1", "state_x", "art_y"},
	}
	resp := a.Build(input, result, nil)
	if len(resp.Objects) != 1 || resp.Objects[0] != "state_x" {
		t.Errorf("StateTypeFilter: want [state_x], got %v", resp.Objects)
	}
}

func TestAssembler_NoFilterPassthrough(t *testing.T) {
	a := NewAssembler()
	input := dataplane.SearchInput{TopK: 10} // no ObjectTypes
	result := dataplane.SearchOutput{
		ObjectIDs: []string{"mem_1", "state_x", "art_y"},
	}
	resp := a.Build(input, result, nil)
	if len(resp.Objects) != 3 {
		t.Errorf("NoFilter: want 3 objects, got %d", len(resp.Objects))
	}
}

func TestAssembler_ProvenanceFromCanonicalObjectsAndEdges(t *testing.T) {
	objStore := storage.NewMemoryObjectStore()
	edgeStore := storage.NewMemoryGraphEdgeStore()
	verStore := storage.NewMemorySnapshotVersionStore()

	mem := schemas.Memory{
		MemoryID:       "mem_evt_p1",
		SourceEventIDs: []string{"evt_p1"},
		ProvenanceRef:  "evt_p1",
	}
	objStore.PutMemory(mem)
	edgeStore.PutEdge(schemas.Edge{
		EdgeID:        "edge_mem_evt_p1_event",
		SrcObjectID:   "mem_evt_p1",
		SrcType:       "memory",
		EdgeType:      "caused_by",
		DstObjectID:   "evt_p1",
		DstType:       "event",
		ProvenanceRef: "evt_p1",
	})
	verStore.PutVersion(schemas.ObjectVersion{
		ObjectID:        "mem_evt_p1",
		ObjectType:      "memory",
		Version:         1,
		MutationEventID: "evt_p1",
	})

	a := NewAssembler().
		WithObjectStore(objStore).
		WithEdgeStore(edgeStore).
		WithVersionStore(verStore)
	resp := a.Build(dataplane.SearchInput{TopK: 5}, dataplane.SearchOutput{ObjectIDs: []string{"mem_evt_p1"}}, nil)

	if len(resp.Provenance) == 0 {
		t.Fatalf("expected non-empty provenance, got empty")
	}
	if len(resp.Provenance) != 1 || resp.Provenance[0] != "evt_p1" {
		t.Fatalf("expected provenance [evt_p1], got %v", resp.Provenance)
	}
}

func TestEvidenceCache_Invalidate(t *testing.T) {
	c := NewCache(10)
	c.Put(EvidenceFragment{ObjectID: "x", SalienceScore: 0.8})
	c.Invalidate("x")

	_, ok := c.Get("x")
	if ok {
		t.Error("Cache.Get after Invalidate: should return false")
	}
}

func TestAssembler_Build_ColdTierEvidenceCacheStatsAndTrace(t *testing.T) {
	cache := NewCache(100)
	a := NewCachedAssembler(cache)

	// one cache hit, one cache miss
	cache.Put(EvidenceFragment{
		ObjectID:      "mem_1",
		SalienceScore: 0.9,
		Level:         1,
	})

	input := dataplane.SearchInput{TopK: 5}
	result := dataplane.SearchOutput{
		ObjectIDs:      []string{"mem_1", "mem_2"},
		ColdObjectIDs:  []string{"mem_1", "mem_2"},
		Tier:           "hot+warm+cold",
		ColdSearchMode: "vector",
	}

	resp := a.Build(input, result, nil)

	if resp.EvidenceCache == nil {
		t.Fatal("expected EvidenceCache stats to be non-nil")
	}

	if resp.EvidenceCache.LookedUp != 2 {
		t.Fatalf("expected LookedUp=2, got %d", resp.EvidenceCache.LookedUp)
	}
	if resp.EvidenceCache.Hits != 1 {
		t.Fatalf("expected Hits=1, got %d", resp.EvidenceCache.Hits)
	}
	if resp.EvidenceCache.Misses != 1 {
		t.Fatalf("expected Misses=1, got %d", resp.EvidenceCache.Misses)
	}

	if resp.EvidenceCache.ColdHits != 1 {
		t.Fatalf("expected ColdHits=1, got %d", resp.EvidenceCache.ColdHits)
	}
	if resp.EvidenceCache.ColdMisses != 1 {
		t.Fatalf("expected ColdMisses=1, got %d", resp.EvidenceCache.ColdMisses)
	}

	// provenance should include cold_tier
	coldProvFound := false
	for _, p := range resp.Provenance {
		if p == "cold_tier" {
			coldProvFound = true
			break
		}
	}
	if !coldProvFound {
		t.Fatalf("expected provenance to include cold_tier, got %v", resp.Provenance)
	}

	// proof trace should include cold-tier steps
	foundFetch := false
	foundRerank := false
	for _, step := range resp.ProofTrace {
		if step.Operation == "cold_embedding_fetch" {
			foundFetch = true
		}
		if step.Operation == "cold_rerank" {
			foundRerank = true
		}
	}
	if !foundFetch {
		t.Fatalf("expected proof trace to include cold_embedding_fetch, got %+v", resp.ProofTrace)
	}
	if !foundRerank {
		t.Fatalf("expected proof trace to include cold_rerank, got %+v", resp.ProofTrace)
	}
}

func TestAssembler_Build_ColdTierProofTrace_HNSWMode(t *testing.T) {
	cache := NewCache(100)
	a := NewCachedAssembler(cache)

	result := dataplane.SearchOutput{
		ObjectIDs:      []string{"mem_1"},
		Tier:           "hot+warm+cold",
		ColdSearchMode: "hnsw",
	}

	resp := a.Build(dataplane.SearchInput{TopK: 5}, result, nil)

	foundHNSW := false
	foundVectorFetch := false
	for _, step := range resp.ProofTrace {
		if step.Operation == "cold_hnsw_search" {
			foundHNSW = true
		}
		if step.Operation == "cold_embedding_fetch" {
			foundVectorFetch = true
		}
	}

	if !foundHNSW {
		t.Fatalf("expected proof trace to include cold_hnsw_search, got %+v", resp.ProofTrace)
	}
	if foundVectorFetch {
		t.Fatalf("did not expect cold_embedding_fetch in hnsw mode, got %+v", resp.ProofTrace)
	}
}

func TestAssembler_Build_ColdTierProofTrace_LexicalMode(t *testing.T) {
	cache := NewCache(100)
	a := NewCachedAssembler(cache)

	result := dataplane.SearchOutput{
		ObjectIDs:      []string{"mem_1"},
		Tier:           "hot+warm+cold",
		ColdSearchMode: "lexical",
	}

	resp := a.Build(dataplane.SearchInput{TopK: 5}, result, nil)

	foundLexical := false
	foundVectorFetch := false
	for _, step := range resp.ProofTrace {
		if step.Operation == "cold_lexical_search" {
			foundLexical = true
		}
		if step.Operation == "cold_embedding_fetch" {
			foundVectorFetch = true
		}
	}

	if !foundLexical {
		t.Fatalf("expected proof trace to include cold_lexical_search, got %+v", resp.ProofTrace)
	}
	if foundVectorFetch {
		t.Fatalf("did not expect cold_embedding_fetch in lexical mode, got %+v", resp.ProofTrace)
	}
}
