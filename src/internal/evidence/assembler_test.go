package evidence

import (
	"testing"

	"andb/src/internal/dataplane"
)

func TestAssembler_Build_Basic(t *testing.T) {
	a := NewAssembler()
	result := dataplane.SearchOutput{
		ObjectIDs:      []string{"mem_1", "mem_2"},
		ScannedSegments: []string{"shard_a"},
		Tier:           "warm",
	}
	resp := a.Build(result, []string{"visibility:private"})

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
		if step == "tier:warm" {
			tierFound = true
		}
	}
	if !tierFound {
		t.Error("Build: ProofTrace should contain 'tier:warm'")
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

	result := dataplane.SearchOutput{
		ObjectIDs: []string{"mem_1"},
		Tier:      "hot",
	}
	resp := a.Build(result, nil)

	fragFound := false
	for _, step := range resp.ProofTrace {
		if len(step) > 9 && step[:9] == "fragment:" {
			fragFound = true
		}
	}
	if !fragFound {
		t.Error("CachedAssembler: expected fragment step in ProofTrace after cache hit")
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

func TestEvidenceCache_Invalidate(t *testing.T) {
	c := NewCache(10)
	c.Put(EvidenceFragment{ObjectID: "x", SalienceScore: 0.8})
	c.Invalidate("x")

	_, ok := c.Get("x")
	if ok {
		t.Error("Cache.Get after Invalidate: should return false")
	}
}
