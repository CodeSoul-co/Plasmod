package dataplane

import (
	"testing"

	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// Regression: when the hot tier already returns TopK hits, cold must still run if IncludeCold is set.
func TestTieredDataPlane_Search_IncludeColdWhenHotSatisfiesTopK(t *testing.T) {
	cold := storage.NewInMemoryColdStore()
	cold.PutMemory(schemas.Memory{
		MemoryID: "mem_only_cold",
		Content:  "foo cold tier marker",
	})
	objs := storage.NewTieredObjectStore(nil, nil, nil, cold)
	p := NewTieredDataPlane(objs)

	p.hot.InsertObject("mem_hot_1", "foo warm one", nil, "", 1)
	p.hot.InsertObject("mem_hot_2", "foo warm two", nil, "", 2)

	// TopK=2 and two hot hits triggers the hot fast-path; cold is still merged for tier/proof trace.
	out := p.Search(SearchInput{
		QueryText:   "foo",
		TopK:        2,
		IncludeCold: true,
	})
	if out.Tier != "hot+cold" {
		t.Fatalf("tier: got %q, want hot+cold", out.Tier)
	}
	// Merged list is truncated to TopK, so cold IDs may not appear when hot already fills the page.
}
