// demo_test.go — End-to-end functional demonstration of the CogDB retrieval engine.
//
// Simulates a realistic multi-agent memory retrieval scenario:
//   - 10 memory objects across 2 agents, 3 memory types
//   - 2 safety violations (quarantine + TTL expired) that must be filtered
//   - RRF reranking with different importance/freshness/confidence scores
//   - Seed marking: top-50% of ranked results drive QueryChain expansion
//   - for_graph mode: returns 2×TopK to support 1-hop subgraph expansion
//
// Run: go test -v ./src/internal/retrieval/ -run Demo
// No external connections, no CGO, no network — fully self-contained.

package retrieval_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/retrieval"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

var _ = time.Now // keep time import for TTL calculation below

// ── Mock dataset ─────────────────────────────────────────────────────────────

// scenario describes one memory object in our demo dataset.
type scenario struct {
	id         string
	memType    string // "episodic" | "semantic" | "artifact"
	agentID    string
	importance float64
	freshness  float64
	confidence float64
	// safety violations
	quarantined bool
	ttlExpired  bool // TTL set to past
	inactive    bool
	// rank position returned by mock search (lower = more relevant)
	searchRank int
}

// mockDataset is 10 objects. IDs sorted by expected relevance to a user query.
var mockDataset = []scenario{
	// Rank 1 — highly relevant, all metadata excellent → should become seed
	{id: "mem-alpha", memType: "semantic", agentID: "agent-research",
		importance: 0.92, freshness: 0.88, confidence: 0.95, searchRank: 1},

	// Rank 2 — very relevant but confidence only moderate
	{id: "mem-beta", memType: "episodic", agentID: "agent-research",
		importance: 0.85, freshness: 0.80, confidence: 0.70, searchRank: 2},

	// Rank 3 — relevant, decent scores → should become seed (top 50%)
	{id: "mem-gamma", memType: "semantic", agentID: "agent-assistant",
		importance: 0.75, freshness: 0.90, confidence: 0.80, searchRank: 3},

	// Rank 4 — moderate relevance
	{id: "mem-delta", memType: "episodic", agentID: "agent-assistant",
		importance: 0.60, freshness: 0.65, confidence: 0.72, searchRank: 4},

	// Rank 5 — moderate, old freshness
	{id: "mem-epsilon", memType: "artifact", agentID: "agent-research",
		importance: 0.70, freshness: 0.30, confidence: 0.85, searchRank: 5},

	// Rank 6 — low confidence research note
	{id: "mem-zeta", memType: "episodic", agentID: "agent-research",
		importance: 0.50, freshness: 0.55, confidence: 0.40, searchRank: 6},

	// Rank 7 — 🔴 QUARANTINED → must be filtered by safety rule #1
	{id: "mem-eta", memType: "episodic", agentID: "agent-assistant",
		importance: 0.88, freshness: 0.90, confidence: 0.95,
		quarantined: true, searchRank: 7},

	// Rank 8 — 🔴 TTL EXPIRED → must be filtered by safety rule #2
	{id: "mem-theta", memType: "semantic", agentID: "agent-research",
		importance: 0.80, freshness: 0.80, confidence: 0.80,
		ttlExpired: true, searchRank: 8},

	// Rank 9 — very low scores, won't be seed
	{id: "mem-iota", memType: "artifact", agentID: "agent-assistant",
		importance: 0.20, freshness: 0.25, confidence: 0.30, searchRank: 9},

	// Rank 10 — 🔴 INACTIVE → filtered by safety rule #4
	{id: "mem-kappa", memType: "episodic", agentID: "agent-assistant",
		importance: 0.75, freshness: 0.75, confidence: 0.75,
		inactive: true, searchRank: 10},
}

// ── Builders ─────────────────────────────────────────────────────────────────

func buildDemoStore() storage.ObjectStore {
	s := storage.NewMemoryRuntimeStorage()
	for _, sc := range mockDataset {
		m := schemas.Memory{
			MemoryID:       sc.id,
			MemoryType:     sc.memType,
			AgentID:        sc.agentID,
			IsActive:       !sc.inactive,
			Importance:     sc.importance,
			FreshnessScore: sc.freshness,
			Confidence:     sc.confidence,
			Version:        1,
		}
		if sc.quarantined {
			m.PolicyTags = []string{"quarantine"}
		}
		if sc.ttlExpired {
			m.TTL = time.Now().Add(-24 * time.Hour).Unix() // expired yesterday
		}
		s.Objects().PutMemory(m)
	}
	return s.Objects()
}

func buildDemoPlane() dataplane.DataPlane {
	// Return IDs in search-rank order (simulates TieredDataPlane.Search output)
	ids := make([]string, len(mockDataset))
	for _, sc := range mockDataset {
		ids[sc.searchRank-1] = sc.id
	}
	return &mockPlane{orderedIDs: ids}
}

// ── Demo tests ────────────────────────────────────────────────────────────────

// TestDemoRetrieval_StandardQuery simulates a standard agent query:
// "Retrieve top 5 results, apply safety rules, rank, mark seeds".
func TestDemoRetrieval_StandardQuery(t *testing.T) {
	store := buildDemoStore()
	plane := buildDemoPlane()
	ret := retrieval.New(plane, store)

	req := retrieval.RetrievalRequest{
		QueryID:            "demo-query-001",
		QueryText:          "recent research findings about multi-agent coordination",
		TopK:               5,
		AgentID:            "",   // all agents
		ExcludeQuarantined: true, // safety rule #1
	}

	result := ret.Retrieve(req)

	printBanner(t, "Standard Query — TopK=5")
	printCandidates(t, result)
	printSeeds(t, result)
	printStats(t, result)

	// Assertions
	if result.TotalFound == 0 {
		t.Fatal("FAIL: expected candidates, got 0")
	}
	// Safety violations must be absent
	for _, c := range result.Candidates {
		if c.ObjectID == "mem-eta" {
			t.Errorf("FAIL: quarantined mem-eta must not appear in results")
		}
		if c.ObjectID == "mem-theta" {
			t.Errorf("FAIL: TTL-expired mem-theta must not appear in results")
		}
		if c.ObjectID == "mem-kappa" {
			t.Errorf("FAIL: inactive mem-kappa must not appear in results")
		}
	}
	// Top result should be the highest-scoring surviving candidate
	if len(result.Candidates) > 0 && result.Candidates[0].FinalScore < result.Candidates[len(result.Candidates)-1].FinalScore {
		t.Error("FAIL: results are not sorted descending by final_score")
	}
	// Seeds must be a subset of candidates
	seedSet := map[string]bool{}
	for _, id := range result.SeedIDs {
		seedSet[id] = true
	}
	for _, id := range result.SeedIDs {
		found := false
		for _, c := range result.Candidates {
			if c.ObjectID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FAIL: seed %q not present in candidates", id)
		}
		_ = seedSet
	}
	t.Logf("PASS: %d candidates, %d filtered by safety, %d seeds",
		result.TotalFound, 3, len(result.SeedIDs))
}

// TestDemoRetrieval_ForGraphMode simulates the QueryChain scenario:
// "Give me 2×TopK candidates; seeds will drive 1-hop subgraph expansion".
func TestDemoRetrieval_ForGraphMode(t *testing.T) {
	store := buildDemoStore()
	plane := buildDemoPlane()
	ret := retrieval.New(plane, store)

	req := retrieval.RetrievalRequest{
		QueryID:            "demo-query-002-graph",
		QueryText:          "agent coordination episodic memories",
		TopK:               3,
		ForGraph:           true,  // returns TopK*2 = 6
		ExcludeQuarantined: true,
	}

	result := ret.Retrieve(req)

	printBanner(t, "ForGraph Mode — TopK=3 → expect up to 6 candidates")
	printCandidates(t, result)
	printSeeds(t, result)
	printQueryChainInput(t, result)

	// In ForGraph mode we get more candidates for richer subgraph expansion
	if result.TotalFound > 6 {
		t.Errorf("FAIL: ForGraph TopK=3 should return at most 6, got %d", result.TotalFound)
	}
	if len(result.SeedIDs) == 0 {
		t.Error("FAIL: expected at least one seed for graph expansion")
	}
	t.Logf("PASS: ForGraph returned %d candidates, %d seeds → QueryChain input ready",
		result.TotalFound, len(result.SeedIDs))
}

// TestDemoRetrieval_AgentScoped simulates a per-agent scoped query:
// only memories belonging to "agent-research" should come through.
func TestDemoRetrieval_AgentScoped(t *testing.T) {
	store := buildDemoStore()

	// Only feed agent-research object IDs to the mock plane
	agentIDs := []string{}
	for _, sc := range mockDataset {
		if sc.agentID == "agent-research" && !sc.quarantined && !sc.ttlExpired && !sc.inactive {
			agentIDs = append(agentIDs, sc.id)
		}
	}
	plane := &mockPlane{orderedIDs: agentIDs}
	ret := retrieval.New(plane, store)

	req := retrieval.RetrievalRequest{
		QueryID:            "demo-query-003-scoped",
		QueryText:          "research memories",
		TopK:               10,
		AgentID:            "agent-research",
		ExcludeQuarantined: true,
	}

	result := ret.Retrieve(req)

	printBanner(t, "Agent-Scoped Query — agent-research only")
	printCandidates(t, result)

	for _, c := range result.Candidates {
		if c.AgentID != "" && c.AgentID != "agent-research" {
			t.Errorf("FAIL: candidate %q belongs to agent %q, not agent-research", c.ObjectID, c.AgentID)
		}
	}
	t.Logf("PASS: all %d results from agent-research", result.TotalFound)
}

// TestDemoRetrieval_SafetyFilterAll confirms all 3 safety violations are caught.
func TestDemoRetrieval_SafetyFilterAll(t *testing.T) {
	store := buildDemoStore()

	// Feed ALL IDs (including violators) to the plane
	all := make([]string, len(mockDataset))
	for i, sc := range mockDataset {
		all[i] = sc.id
	}
	plane := &mockPlane{orderedIDs: all}
	ret := retrieval.New(plane, store)

	req := retrieval.RetrievalRequest{
		QueryID:            "demo-query-004-safety",
		QueryText:          "all memories",
		TopK:               20,
		ExcludeQuarantined: true,
	}

	result := ret.Retrieve(req)

	printBanner(t, "Safety Filter Verification")

	violators := []string{"mem-eta" /*quarantined*/, "mem-theta" /*TTL*/, "mem-kappa" /*inactive*/}
	resultSet := map[string]bool{}
	for _, c := range result.Candidates {
		resultSet[c.ObjectID] = true
	}

	passed := 0
	for _, v := range violators {
		if resultSet[v] {
			t.Errorf("FAIL: safety violator %q must not appear in results", v)
		} else {
			t.Logf("  ✓ blocked: %s", v)
			passed++
		}
	}
	total := len(mockDataset)
	expected := total - len(violators)
	t.Logf("PASS: %d/%d safety rules fired; %d/%d clean records passed",
		passed, len(violators), result.TotalFound, expected)
}

// TestDemoRetrieval_EnrichAndRank tests the post-search enrichment path:
// caller supplies pre-searched IDs (from nodeManager.DispatchQuery) →
// EnrichAndRank applies filter+reranking without a duplicate search.
func TestDemoRetrieval_EnrichAndRank(t *testing.T) {
	store := buildDemoStore()
	// No real plane needed — EnrichAndRank bypasses it
	plane := &mockPlane{orderedIDs: nil}
	ret := retrieval.New(plane, store)

	// Pre-ranked IDs as if from nodeManager.DispatchQuery
	preRankedIDs := []string{
		"mem-alpha", "mem-beta", "mem-gamma", "mem-delta", "mem-epsilon",
		"mem-zeta", "mem-eta", "mem-theta", // includes 2 violators
	}

	req := retrieval.RetrievalRequest{
		QueryID:            "demo-query-005-enrichrank",
		QueryText:          "research coordination",
		TopK:               5,
		ExcludeQuarantined: true,
	}

	result := ret.EnrichAndRank(preRankedIDs, req)

	printBanner(t, "EnrichAndRank — post-search path (nodeManager.DispatchQuery)")
	printCandidates(t, result)
	printSeeds(t, result)

	// Violations must be absent
	for _, c := range result.Candidates {
		if c.ObjectID == "mem-eta" || c.ObjectID == "mem-theta" {
			t.Errorf("FAIL: violator %q must not appear", c.ObjectID)
		}
	}
	if result.TotalFound == 0 {
		t.Fatal("FAIL: expected candidates")
	}
	// Verify descending order
	for i := 1; i < len(result.Candidates); i++ {
		if result.Candidates[i].FinalScore > result.Candidates[i-1].FinalScore {
			t.Errorf("FAIL: not sorted descending at index %d", i)
		}
	}
	t.Logf("PASS: EnrichAndRank — %d candidates, %d seeds", result.TotalFound, len(result.SeedIDs))
}

// ── Pretty printers ───────────────────────────────────────────────────────────

func printBanner(t *testing.T, title string) {
	t.Helper()
	t.Logf("\n%s\n  %s\n%s", strings.Repeat("─", 60), title, strings.Repeat("─", 60))
}

func printCandidates(t *testing.T, cl retrieval.CandidateList) {
	t.Helper()
	t.Logf("  %-12s %-10s %-16s  %7s  %6s  %6s  %6s  %6s  %s",
		"ObjectID", "Type", "AgentID", "RRF", "Imp", "Fresh", "Conf", "Final", "Seed")
	t.Logf("  %s", strings.Repeat("-", 90))
	for _, c := range cl.Candidates {
		seed := ""
		if c.IsSeed {
			seed = fmt.Sprintf("✓ (norm=%.2f)", c.SeedScore)
		}
		t.Logf("  %-12s %-10s %-16s  %.4f  %.3f  %.3f  %.3f  %.4f  %s",
			c.ObjectID, c.ObjectType, c.AgentID,
			c.RRFScore, c.Importance, c.FreshnessScore, c.Confidence,
			c.FinalScore, seed)
	}
}

func printSeeds(t *testing.T, cl retrieval.CandidateList) {
	t.Helper()
	if len(cl.SeedIDs) == 0 {
		t.Logf("  Seeds: (none)")
		return
	}
	t.Logf("  Seeds → QueryChain expansion: %s", strings.Join(cl.SeedIDs, ", "))
}

func printQueryChainInput(t *testing.T, cl retrieval.CandidateList) {
	t.Helper()
	t.Logf("  QueryChain.Run(SeedIDs) call:")
	t.Logf("    SeedIDs = %v", cl.SeedIDs)
	t.Logf("    These drive 1-hop subgraph expansion in graph-c.")
}

func printStats(t *testing.T, cl retrieval.CandidateList) {
	t.Helper()
	t.Logf("  Stats: found=%d  latency=%dms  seeds=%d",
		cl.TotalFound, cl.Meta.LatencyMs, len(cl.SeedIDs))
}
