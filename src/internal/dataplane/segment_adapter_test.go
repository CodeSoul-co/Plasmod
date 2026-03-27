package dataplane

import (
	"testing"

	"andb/src/internal/storage"
)

func TestSegmentDataPlane_IngestAndSearch(t *testing.T) {
	plane := NewSegmentDataPlane()

	rec := IngestRecord{
		ObjectID:  "mem_test_1",
		Text:      "agent memory recall test",
		Namespace: "ws_test",
	}
	if err := plane.Ingest(rec); err != nil {
		t.Fatalf("Ingest: unexpected error: %v", err)
	}

	result := plane.Search(SearchInput{
		QueryText:      "memory recall",
		TopK:           5,
		Namespace:      "ws_test",
		IncludeGrowing: true,
	})
	if len(result.ObjectIDs) == 0 {
		t.Fatal("Search: expected at least one result for 'memory recall'")
	}
	if result.ObjectIDs[0] != "mem_test_1" {
		t.Errorf("Search: top result should be mem_test_1, got %q", result.ObjectIDs[0])
	}
}

func TestSegmentDataPlane_Flush(t *testing.T) {
	plane := NewSegmentDataPlane()
	if err := plane.Flush(); err != nil {
		t.Errorf("Flush: unexpected error: %v", err)
	}
}

func TestTieredDataPlane_IngestAndSearch(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	plane := NewTieredDataPlane(tieredObjs)

	rec := IngestRecord{
		ObjectID:  "mem_tiered_1",
		Text:      "tiered hot path recall",
		Namespace: "ws_tiered",
	}
	if err := plane.Ingest(rec); err != nil {
		t.Fatalf("TieredDataPlane.Ingest: unexpected error: %v", err)
	}

	result := plane.Search(SearchInput{
		QueryText:      "tiered hot",
		TopK:           5,
		Namespace:      "ws_tiered",
		IncludeGrowing: true,
	})
	if len(result.ObjectIDs) == 0 {
		t.Fatal("TieredDataPlane.Search: expected at least one result")
	}
	if result.Tier == "" {
		t.Error("TieredDataPlane.Search: Tier field should be non-empty")
	}
}
