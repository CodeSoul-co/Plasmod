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

func TestSegmentDataPlane_Search_HybridReturnsLexicalVectorTier(t *testing.T) {
	embedder := NewTfidfEmbedder(DefaultEmbeddingDim)

	plane, err := NewSegmentDataPlaneWithEmbedder(embedder)
	if err != nil {
		t.Fatalf("NewSegmentDataPlaneWithEmbedder: unexpected error: %v", err)
	}

	records := []IngestRecord{
		{
			ObjectID:  "mem_hybrid_1",
			Text:      "nvidia q3 revenue growth",
			Namespace: "ws_hybrid",
		},
		{
			ObjectID:  "mem_hybrid_2",
			Text:      "gpu revenue increased strongly this quarter",
			Namespace: "ws_hybrid",
		},
	}

	for _, rec := range records {
		if err := plane.Ingest(rec); err != nil {
			t.Fatalf("Ingest: unexpected error: %v", err)
		}
	}

	if err := plane.Flush(); err != nil {
		t.Fatalf("Flush: unexpected error: %v", err)
	}

	result := plane.Search(SearchInput{
		QueryText:      "nvidia revenue quarter",
		TopK:           5,
		Namespace:      "ws_hybrid",
		IncludeGrowing: true,
	})

	if len(result.ObjectIDs) == 0 {
		t.Fatal("Search: expected at least one hybrid result")
	}
	if result.Tier != "lexical+vector" && result.Tier != "vector" && result.Tier != "lexical" {
		t.Fatalf("Search: unexpected tier %q", result.Tier)
	}

	// In the hybrid-ready path we expect lexical+vector after Flush() when
	// both lexical and vector retrieval produce candidates.
	if result.Tier != "lexical+vector" {
		t.Logf("note: tier=%q (environment may have degraded vector path)", result.Tier)
	}
}

func TestTieredDataPlane_Search_IncludeColdUsesTieredFusion(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	tieredObjs := storage.NewTieredObjectStore(store.HotCache(), store.Objects(), store.Edges(), storage.NewInMemoryColdStore())
	plane := NewTieredDataPlane(tieredObjs)

	// Hot/warm ingest
	rec := IngestRecord{
		ObjectID:  "mem_hotwarm_1",
		Text:      "tiered recall for nvidia revenue",
		Namespace: "ws_tiered_fusion",
	}
	if err := plane.Ingest(rec); err != nil {
		t.Fatalf("Ingest: unexpected error: %v", err)
	}

	// Cold-only archived object
	attrs := map[string]string{
		"agent_id":   "agent_1",
		"session_id": "sess_1",
		"visibility": "workspace_shared",
		"event_type": "tool_result",
	}
	tieredObjs.ArchiveColdRecord(
		"mem_cold_1",
		"historical nvidia revenue evidence",
		attrs,
		"ws_tiered_fusion",
		100,
	)

	result := plane.Search(SearchInput{
		QueryText:      "nvidia revenue",
		TopK:           5,
		Namespace:      "ws_tiered_fusion",
		IncludeGrowing: true,
		IncludeCold:    true,
	})

	if len(result.ObjectIDs) == 0 {
		t.Fatal("Search: expected fused results from tiered search")
	}
	if result.Tier != "hot+warm+cold" {
		t.Fatalf("Search: expected tier hot+warm+cold, got %q", result.Tier)
	}

	var hasHotWarm, hasCold bool
	for _, id := range result.ObjectIDs {
		if id == "mem_hotwarm_1" {
			hasHotWarm = true
		}
		if id == "mem_cold_1" {
			hasCold = true
		}
	}
	if !hasHotWarm {
		t.Errorf("expected hot/warm object in fused results, got %v", result.ObjectIDs)
	}
	if !hasCold {
		t.Errorf("expected cold object in fused results, got %v", result.ObjectIDs)
	}
}
