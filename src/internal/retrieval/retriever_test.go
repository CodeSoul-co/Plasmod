package retrieval_test

import (
	"fmt"
	"testing"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/retrieval"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// ─── Mock DataPlane ───────────────────────────────────────────────────────────

// mockPlane is a DataPlane stub that returns a pre-configured ordered list of
// object IDs, simulating the output of TieredDataPlane.Search.
type mockPlane struct {
	orderedIDs []string
}

func (m *mockPlane) Search(_ dataplane.SearchInput) dataplane.SearchOutput {
	return dataplane.SearchOutput{ObjectIDs: m.orderedIDs}
}
func (m *mockPlane) Ingest(_ dataplane.IngestRecord) error { return nil }
func (m *mockPlane) Flush() error                          { return nil }

// ─── Test helpers ─────────────────────────────────────────────────────────────

// makeStore returns a fresh in-memory store pre-populated with the given memories.
func makeStore(mems []schemas.Memory) storage.ObjectStore {
	s := storage.NewMemoryRuntimeStorage()
	for _, m := range mems {
		s.Objects().PutMemory(m)
	}
	return s.Objects()
}

// mem builds a minimal schemas.Memory with the fields the retriever uses.
func mem(id string, importance, freshness, confidence float64) schemas.Memory {
	return schemas.Memory{
		MemoryID:       id,
		MemoryType:     "episodic",
		AgentID:        "agent-1",
		SessionID:      "sess-1",
		IsActive:       true,
		Importance:     importance,
		FreshnessScore: freshness,
		Confidence:     confidence,
		Version:        1,
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestEnrichAndRank_BasicScoring verifies the reranking formula:
//
//	final_score = rrf_score × importance × freshness × confidence
//
// and that results are sorted descending by final_score.
func TestEnrichAndRank_BasicScoring(t *testing.T) {
	// Three objects ranked by the mock DataPlane (rank 0 = best).
	// "obj-high" is rank 0 but has low metadata scores.
	// "obj-low" is rank 2 but has high metadata scores → should win after reranking.
	mems := []schemas.Memory{
		func() schemas.Memory { m := mem("obj-high", 0.1, 0.1, 0.1); return m }(),
		func() schemas.Memory { m := mem("obj-mid", 0.5, 0.5, 0.5); return m }(),
		func() schemas.Memory { m := mem("obj-low", 1.0, 1.0, 1.0); return m }(),
	}
	store := makeStore(mems)
	plane := &mockPlane{orderedIDs: []string{"obj-high", "obj-mid", "obj-low"}}

	r := retrieval.New(plane, store)
	req := retrieval.DefaultRetrievalRequest("test query", 10)
	cl := r.EnrichAndRank([]string{"obj-high", "obj-mid", "obj-low"}, req)

	if len(cl.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(cl.Candidates))
	}

	// After reranking, obj-low (importance=1.0) should be first.
	if cl.Candidates[0].ObjectID != "obj-low" {
		t.Errorf("expected obj-low to rank first after reranking, got %s (final_score=%.4f)",
			cl.Candidates[0].ObjectID, cl.Candidates[0].FinalScore)
	}

	// Verify descending order
	for i := 1; i < len(cl.Candidates); i++ {
		if cl.Candidates[i].FinalScore > cl.Candidates[i-1].FinalScore {
			t.Errorf("results not sorted: idx %d (%.4f) > idx %d (%.4f)",
				i, cl.Candidates[i].FinalScore, i-1, cl.Candidates[i-1].FinalScore)
		}
	}

	t.Logf("Ranking after reranking:")
	for i, c := range cl.Candidates {
		t.Logf("  [%d] %s  rrf=%.4f final=%.4f imp=%.2f fresh=%.2f conf=%.2f",
			i, c.ObjectID, c.RRFScore, c.FinalScore, c.Importance, c.FreshnessScore, c.Confidence)
	}
}

// TestSafetyFilter_Quarantine verifies that quarantined objects are excluded.
func TestSafetyFilter_Quarantine(t *testing.T) {
	quarantined := mem("obj-quarantine", 1.0, 1.0, 1.0)
	quarantined.PolicyTags = []string{"quarantine"}

	store := makeStore([]schemas.Memory{
		mem("obj-ok", 0.8, 0.8, 0.8),
		quarantined,
	})

	r := retrieval.New(nil, store) // plane unused by EnrichAndRank
	req := retrieval.DefaultRetrievalRequest("query", 10)
	req.ExcludeQuarantined = true

	cl := r.EnrichAndRank([]string{"obj-ok", "obj-quarantine"}, req)

	for _, c := range cl.Candidates {
		if c.ObjectID == "obj-quarantine" {
			t.Errorf("quarantined object should have been excluded but appeared in results")
		}
	}
	if len(cl.Candidates) != 1 {
		t.Errorf("expected 1 candidate after filtering, got %d", len(cl.Candidates))
	}
	t.Logf("SafetyFilter quarantine: PASS — only obj-ok returned")
}

// TestSafetyFilter_TTL verifies that TTL-expired objects are excluded.
func TestSafetyFilter_TTL(t *testing.T) {
	expired := mem("obj-expired", 1.0, 1.0, 1.0)
	expired.TTL = time.Now().Add(-1 * time.Hour).Unix() // expired 1 hour ago

	active := mem("obj-active", 0.5, 0.5, 0.5)
	active.TTL = time.Now().Add(24 * time.Hour).Unix() // expires tomorrow

	store := makeStore([]schemas.Memory{expired, active})
	r := retrieval.New(nil, store)
	cl := r.EnrichAndRank([]string{"obj-expired", "obj-active"}, retrieval.DefaultRetrievalRequest("q", 10))

	for _, c := range cl.Candidates {
		if c.ObjectID == "obj-expired" {
			t.Errorf("TTL-expired object should have been excluded")
		}
	}
	t.Logf("SafetyFilter TTL: PASS — expired object excluded")
}

// TestSafetyFilter_IsActive verifies that inactive objects are excluded.
func TestSafetyFilter_IsActive(t *testing.T) {
	inactive := mem("obj-inactive", 1.0, 1.0, 1.0)
	inactive.IsActive = false

	store := makeStore([]schemas.Memory{inactive, mem("obj-active", 0.5, 0.5, 0.5)})
	r := retrieval.New(nil, store)
	cl := r.EnrichAndRank([]string{"obj-inactive", "obj-active"}, retrieval.DefaultRetrievalRequest("q", 10))

	for _, c := range cl.Candidates {
		if c.ObjectID == "obj-inactive" {
			t.Errorf("inactive object should have been excluded")
		}
	}
	t.Logf("SafetyFilter IsActive: PASS — inactive object excluded")
}

// TestSafetyFilter_MinVersion verifies that version-gated objects are excluded.
func TestSafetyFilter_MinVersion(t *testing.T) {
	oldVer := mem("obj-v1", 1.0, 1.0, 1.0)
	oldVer.Version = 1

	newVer := mem("obj-v5", 0.5, 0.5, 0.5)
	newVer.Version = 5

	store := makeStore([]schemas.Memory{oldVer, newVer})
	r := retrieval.New(nil, store)
	req := retrieval.DefaultRetrievalRequest("q", 10)
	req.MinVersion = 3

	cl := r.EnrichAndRank([]string{"obj-v1", "obj-v5"}, req)
	for _, c := range cl.Candidates {
		if c.ObjectID == "obj-v1" {
			t.Errorf("version-1 object should have been excluded (min_version=3)")
		}
	}
	t.Logf("SafetyFilter MinVersion: PASS — v1 object excluded (min_version=3)")
}

// TestSeedMarking verifies relative-normalised seed marking.
//
// With default threshold=0.5, any candidate whose normalised score >= 0.5
// (i.e. at least half as good as the best candidate) becomes a seed.
// obj-seed has metadata scores 10× higher than obj-no-seed, so after
// reranking obj-seed's normalised score = 1.0 (best) and obj-no-seed's
// normalised score ≈ 0.0001 — only obj-seed should be seeded.
func TestSeedMarking(t *testing.T) {
	mems := []schemas.Memory{
		mem("obj-seed", 1.0, 1.0, 1.0),      // high metadata → normalised ≈ 1.0
		mem("obj-no-seed", 0.01, 0.01, 0.01), // low metadata → normalised ≈ tiny
	}
	store := makeStore(mems)
	r := retrieval.New(nil, store)
	// Default threshold=0.5, floor=1e-4: no manual override needed.

	req := retrieval.DefaultRetrievalRequest("q", 10)
	cl := r.EnrichAndRank([]string{"obj-seed", "obj-no-seed"}, req)

	seedCount := 0
	for _, c := range cl.Candidates {
		if c.IsSeed {
			seedCount++
			t.Logf("Seed: %s  raw=%.6f  normalised(seed_score)=%.4f", c.ObjectID, c.FinalScore, c.SeedScore)
		} else {
			t.Logf("Non-seed: %s  raw=%.6f  normalised(seed_score)=%.4f", c.ObjectID, c.FinalScore, c.SeedScore)
		}
	}

	if len(cl.SeedIDs) != seedCount {
		t.Errorf("SeedIDs length (%d) != IsSeed count (%d)", len(cl.SeedIDs), seedCount)
	}
	if seedCount == 0 {
		t.Errorf("expected at least one seed (obj-seed has max score, normalised=1.0 >= threshold=0.5)")
	}
	// obj-no-seed must not be seeded — its normalised score should be << 0.5
	for _, sid := range cl.SeedIDs {
		if sid == "obj-no-seed" {
			t.Errorf("obj-no-seed should NOT be a seed (low metadata scores → low normalised score)")
		}
	}
	t.Logf("SeedIDs: %v", cl.SeedIDs)
}

// TestForGraphMode verifies that for_graph=true returns TopK*2 candidates.
func TestForGraphMode(t *testing.T) {
	const topK = 3
	mems := make([]schemas.Memory, topK*3)
	ids := make([]string, topK*3)
	for i := range mems {
		id := fmt.Sprintf("obj-%02d", i)
		ids[i] = id
		mems[i] = mem(id, 0.5, 0.5, 0.5)
	}
	store := makeStore(mems)
	r := retrieval.New(nil, store)

	req := retrieval.DefaultRetrievalRequest("q", topK)
	req.ForGraph = true

	cl := r.EnrichAndRank(ids, req)

	// for_graph should allow up to topK*2 = 6 results
	if len(cl.Candidates) > topK*2 {
		t.Errorf("for_graph: expected ≤ %d candidates, got %d", topK*2, len(cl.Candidates))
	}
	if len(cl.Candidates) < topK {
		t.Errorf("for_graph: expected ≥ %d candidates, got %d", topK, len(cl.Candidates))
	}
	t.Logf("ForGraph (topK=%d): returned %d candidates (max=%d)", topK, len(cl.Candidates), topK*2)
}

// TestFilterOnlyMode verifies that filter_only skips the DataPlane and returns
// results ranked purely by importance.
func TestFilterOnlyMode(t *testing.T) {
	mems := []schemas.Memory{
		func() schemas.Memory { m := mem("low-imp", 0.1, 1.0, 1.0); m.AgentID = "agent-x"; m.SessionID = ""; return m }(),
		func() schemas.Memory { m := mem("high-imp", 0.9, 1.0, 1.0); m.AgentID = "agent-x"; m.SessionID = ""; return m }(),
		func() schemas.Memory { m := mem("mid-imp", 0.5, 1.0, 1.0); m.AgentID = "agent-x"; m.SessionID = ""; return m }(),
	}
	store := makeStore(mems)

	// Pass a plane that would return wrong order — filter_only should ignore it.
	plane := &mockPlane{orderedIDs: []string{"low-imp", "mid-imp", "high-imp"}}
	r := retrieval.New(plane, store)

	req := retrieval.DefaultRetrievalRequest("q", 10)
	req.AgentID = "agent-x"
	req.EnableFilterOnly = true

	cl := r.Retrieve(req)

	if len(cl.Candidates) == 0 {
		t.Fatal("filter_only returned 0 candidates")
	}

	t.Logf("FilterOnly ranking by importance:")
	for i, c := range cl.Candidates {
		t.Logf("  [%d] %s  importance=%.2f  final=%.5f", i, c.ObjectID, c.Importance, c.FinalScore)
	}

	// First result should have highest importance (after reranking by importance-driven rrf).
	if cl.Candidates[0].ObjectID != "high-imp" {
		t.Errorf("expected high-imp to be first in filter_only mode, got %s", cl.Candidates[0].ObjectID)
	}
}

// TestQueryChainAlignment verifies that SeedIDs from CandidateList are a subset
// of all candidate ObjectIDs — i.e. the contract QueryChain expects.
func TestQueryChainAlignment(t *testing.T) {
	mems := []schemas.Memory{
		mem("alpha", 1.0, 1.0, 1.0),
		mem("beta", 0.5, 0.5, 0.5),
		mem("gamma", 0.1, 0.1, 0.1),
	}
	store := makeStore(mems)
	r := retrieval.New(nil, store)
	// Default threshold=0.5 (relative normalisation): alpha has max score so
	// normalised=1.0 >= 0.5, guaranteed to be a seed.

	cl := r.EnrichAndRank([]string{"alpha", "beta", "gamma"}, retrieval.DefaultRetrievalRequest("q", 10))

	// All SeedIDs must appear in Candidates
	candidateSet := make(map[string]bool)
	for _, c := range cl.Candidates {
		candidateSet[c.ObjectID] = true
	}
	for _, sid := range cl.SeedIDs {
		if !candidateSet[sid] {
			t.Errorf("SeedID %q not found in Candidates — QueryChain would get a dangling ID", sid)
		}
	}

	// All IsSeed=true candidates must appear in SeedIDs
	seedSet := make(map[string]bool)
	for _, sid := range cl.SeedIDs {
		seedSet[sid] = true
	}
	for _, c := range cl.Candidates {
		if c.IsSeed && !seedSet[c.ObjectID] {
			t.Errorf("candidate %q has IsSeed=true but not in SeedIDs", c.ObjectID)
		}
	}

	t.Logf("QueryChain alignment: %d candidates, %d seeds → %v", len(cl.Candidates), len(cl.SeedIDs), cl.SeedIDs)
}

// TestNonExistentObjectPassThrough verifies that object IDs not found in
// ObjectStore (e.g. State or Artifact objects) pass through unfiltered.
func TestNonExistentObjectPassThrough(t *testing.T) {
	// Only "obj-known" is in the store; "obj-state" is not (simulates a State object).
	store := makeStore([]schemas.Memory{mem("obj-known", 0.5, 0.5, 0.5)})
	r := retrieval.New(nil, store)

	cl := r.EnrichAndRank([]string{"obj-known", "obj-state"}, retrieval.DefaultRetrievalRequest("q", 10))

	found := map[string]bool{}
	for _, c := range cl.Candidates {
		found[c.ObjectID] = true
	}
	if !found["obj-state"] {
		t.Errorf("obj-state (non-memory object) should pass through unfiltered, but was excluded")
	}
	t.Logf("PassThrough: %v", cl.Candidates)
}
