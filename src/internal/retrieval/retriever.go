package retrieval

import (
	"math"
	"sort"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

const (
	// rrfK is the Reciprocal Rank Fusion constant (k=60 matches Python / C++).
	rrfK = 60.0

	// DefaultSeedThreshold is the normalised score threshold for seed marking.
	// Seeding uses relative normalisation: each candidate's final_score is
	// divided by the max final_score in the result set, producing a value in
	// [0, 1].  Candidates whose normalised score >= threshold become seeds.
	//
	// 0.5 means "top half of this result set"; 0.7 means "top 30%".
	// A raw absolute floor (SeedAbsoluteFloor) guards against seeding when
	// all results have negligible scores (e.g. all metadata near zero).
	DefaultSeedThreshold = 0.5

	// DefaultSeedAbsoluteFloor is the minimum raw final_score a candidate must
	// reach before it is even eligible for seed normalisation.  This prevents
	// low-quality results from becoming seeds just because they beat other
	// equally-poor candidates.
	//
	// Given rrf_score_max ≈ 1/(60+1) ≈ 0.0164, a floor of 1e-4 means the
	// candidate needs at least importance * freshness * confidence > 0.006.
	DefaultSeedAbsoluteFloor = 1e-4

	// DefaultMaxRetries controls retry attempts when Search panics or returns
	// an empty result unexpectedly.
	DefaultMaxRetries = 3
)

// Retriever is the Go replacement for the Python retrieval service.
//
// It orchestrates:
//   - TieredDataPlane.Search (lexical + CGO Knowhere vector)
//   - ObjectStore.GetMemory   (metadata fetch for scoring / filtering)
//   - SafetyFilter            (7 governance rules)
//   - RRF reranking           (final_score = rrf × importance × freshness × confidence)
//   - Seed marking            (relative normalisation: normalised_score ≥ SeedThreshold)
//   - for_graph mode          (returns TopK×2 candidates)
//   - filter_only mode        (skips dense+sparse, ranks by importance)
//
// Seed threshold semantics:
//
//	Each candidate's final_score is divided by the max final_score in the
//	result set, yielding a normalised score in [0, 1].  Candidates whose
//	normalised score >= SeedThreshold (default 0.5) are marked as seeds.
//	SeedAbsoluteFloor is an additional guard: a candidate whose raw
//	final_score is below the floor is never seeded, even if it wins the
//	relative comparison.  This avoids seeding in uniformly low-quality results.
type Retriever struct {
	plane              dataplane.DataPlane
	objects            storage.ObjectStore
	filter             SafetyFilter
	SeedThreshold      float64
	SeedAbsoluteFloor  float64
}

// New returns a Retriever wired to the given DataPlane and ObjectStore.
func New(plane dataplane.DataPlane, objects storage.ObjectStore) *Retriever {
	return &Retriever{
		plane:             plane,
		objects:           objects,
		filter:            SafetyFilter{},
		SeedThreshold:     DefaultSeedThreshold,
		SeedAbsoluteFloor: DefaultSeedAbsoluteFloor,
	}
}

// Retrieve executes the full retrieval pipeline for the given request and
// returns a CandidateList ready for consumption by QueryChain.
//
// When the caller already holds ranked ObjectIDs from a separate search (e.g.
// nodeManager.DispatchQuery), prefer EnrichAndRank to avoid a duplicate search.
func (r *Retriever) Retrieve(req RetrievalRequest) CandidateList {
	start := time.Now()

	effectiveTopK := req.TopK
	if req.ForGraph {
		effectiveTopK = req.TopK * 2
	}
	if effectiveTopK <= 0 {
		effectiveTopK = 10
	}

	var candidates []Candidate
	if req.EnableFilterOnly {
		candidates = r.filterOnlySearch(req, effectiveTopK)
	} else {
		candidates = r.hybridSearch(req, effectiveTopK)
	}

	// Safety filter
	passed := candidates[:0]
	for _, c := range candidates {
		mem, ok := r.objects.GetMemory(c.ObjectID)
		if !ok {
			continue
		}
		if !r.filter.Apply(mem, req) {
			continue
		}
		c = enrichFromMemory(c, mem)
		passed = append(passed, c)
	}

	// Rerank and mark seeds
	markFinalScores(passed)
	seedIDs := markSeeds(passed, r.SeedThreshold, r.SeedAbsoluteFloor)

	// Sort by final_score descending
	sort.Slice(passed, func(i, j int) bool {
		return passed[i].FinalScore > passed[j].FinalScore
	})

	// Truncate to effective top-K
	if len(passed) > effectiveTopK {
		passed = passed[:effectiveTopK]
	}

	meta := buildMeta(start, passed)
	return CandidateList{
		Candidates:  passed,
		TotalFound:  len(passed),
		RetrievedAt: time.Now().UTC(),
		Meta:        meta,
		SeedIDs:     seedIDs,
	}
}

// EnrichAndRank takes pre-searched ObjectIDs (from nodeManager.DispatchQuery or
// any DataPlane.Search caller) and enriches them with Memory metadata, applies
// the safety filter, computes final scores, and marks seeds.
//
// This is the preferred integration point in Runtime.ExecuteQuery so the
// existing nodeManager query-node dispatch (including registered query nodes)
// is preserved while adding the full scoring + governance pipeline.
func (r *Retriever) EnrichAndRank(objectIDs []string, req RetrievalRequest) CandidateList {
	start := time.Now()

	effectiveTopK := req.TopK
	if req.ForGraph {
		effectiveTopK = req.TopK * 2
	}
	if effectiveTopK <= 0 {
		effectiveTopK = 10
	}

	// Build initial candidates with RRF scores from rank position
	raw := make([]Candidate, 0, len(objectIDs))
	for rank, id := range objectIDs {
		rrf := 1.0 / (rrfK + float64(rank+1))
		raw = append(raw, Candidate{
			ObjectID:       id,
			RRFScore:       rrf,
			SourceChannels: []string{"dense", "sparse"},
		})
	}

	// Metadata fetch + safety filter
	passed := raw[:0]
	for _, c := range raw {
		mem, ok := r.objects.GetMemory(c.ObjectID)
		if !ok {
			// Non-memory objects (State, Artifact) pass through unfiltered
			passed = append(passed, c)
			continue
		}
		if !r.filter.Apply(mem, req) {
			continue
		}
		c = enrichFromMemory(c, mem)
		passed = append(passed, c)
	}

	markFinalScores(passed)
	seedIDs := markSeeds(passed, r.SeedThreshold, r.SeedAbsoluteFloor)

	sort.Slice(passed, func(i, j int) bool {
		return passed[i].FinalScore > passed[j].FinalScore
	})
	if len(passed) > effectiveTopK {
		passed = passed[:effectiveTopK]
	}

	meta := buildMeta(start, passed)
	return CandidateList{
		Candidates:  passed,
		TotalFound:  len(passed),
		RetrievedAt: time.Now().UTC(),
		Meta:        meta,
		SeedIDs:     seedIDs,
	}
}

// ─── Search paths ─────────────────────────────────────────────────────────────

// hybridSearch calls TieredDataPlane.Search (lexical + vector) and converts
// the ranked ObjectIDs into Candidate structs with RRF scores.
func (r *Retriever) hybridSearch(req RetrievalRequest, topK int) []Candidate {
	input := dataplane.SearchInput{
		QueryText:      req.QueryText,
		TopK:           topK,
		Namespace:      namespaceFrom(req),
		Constraints:    req.ObjectTypes,
		IncludeGrowing: true,
		ObjectTypes:    req.ObjectTypes,
		MemoryTypes:    req.MemoryTypes,
	}
	if !req.TimeFrom.IsZero() {
		input.TimeFromUnixTS = req.TimeFrom.Unix()
	}
	if !req.TimeTo.IsZero() {
		input.TimeToUnixTS = req.TimeTo.Unix()
	}

	out := r.plane.Search(input)

	candidates := make([]Candidate, 0, len(out.ObjectIDs))
	for rank, id := range out.ObjectIDs {
		rrf := 1.0 / (rrfK + float64(rank+1))
		candidates = append(candidates, Candidate{
			ObjectID:       id,
			RRFScore:       rrf,
			SourceChannels: []string{"dense", "sparse"},
		})
	}
	return candidates
}

// filterOnlySearch skips the vector/lexical search and returns all memories
// for the agent/session, ranked by importance.  Mirrors the Python
// enable_filter_only=True behaviour.
func (r *Retriever) filterOnlySearch(req RetrievalRequest, topK int) []Candidate {
	mems := r.objects.ListMemories(req.AgentID, req.SessionID)

	// Sort by importance descending
	sort.Slice(mems, func(i, j int) bool {
		return mems[i].Importance > mems[j].Importance
	})

	candidates := make([]Candidate, 0, min(topK, len(mems)))
	for i, m := range mems {
		if i >= topK*3 { // fetch 3× buffer before safety filter
			break
		}
		rrf := 1.0 / (rrfK + float64(i+1))
		candidates = append(candidates, Candidate{
			ObjectID:       m.MemoryID,
			RRFScore:       rrf,
			SourceChannels: []string{"filter"},
		})
	}
	return candidates
}

// ─── Scoring helpers ──────────────────────────────────────────────────────────

// enrichFromMemory copies metadata fields from a Memory into a Candidate.
func enrichFromMemory(c Candidate, m schemas.Memory) Candidate {
	c.ObjectType = "memory"
	c.AgentID = m.AgentID
	c.SessionID = m.SessionID
	c.Scope = m.Scope
	c.Version = m.Version
	c.ProvenanceRef = m.ProvenanceRef
	c.Content = m.Content
	c.Summary = m.Summary
	c.Confidence = m.Confidence
	c.Importance = m.Importance
	c.FreshnessScore = m.FreshnessScore
	c.Level = m.Level
	c.MemoryType = m.MemoryType
	c.IsActive = m.IsActive
	c.TTL = m.TTL
	c.ValidFrom = m.ValidFrom
	c.ValidTo = m.ValidTo
	c.LifecycleState = m.LifecycleState
	c.SourceEventIDs = m.SourceEventIDs
	if hasTag(m.PolicyTags, "quarantine") {
		c.QuarantineFlag = true
	}
	return c
}

// markFinalScores applies the reranking formula in-place:
//
//	final_score = rrf_score × max(importance, 0.01) × max(freshness, 0.01) × max(confidence, 0.01)
func markFinalScores(cs []Candidate) {
	for i := range cs {
		imp := math.Max(cs[i].Importance, 0.01)
		fresh := math.Max(cs[i].FreshnessScore, 0.01)
		conf := math.Max(cs[i].Confidence, 0.01)
		cs[i].FinalScore = cs[i].RRFScore * imp * fresh * conf
	}
}

// markSeeds uses relative normalisation to mark seeds:
//
//  1. Find the max raw final_score across all candidates.
//  2. Divide each candidate's final_score by that max → normalised in [0,1].
//  3. Mark as seed when normalised >= threshold AND raw >= absoluteFloor.
//
// This makes the threshold intuitive (0.5 = top half of result set) and
// independent of the RRF constant or metadata score distribution.
func markSeeds(cs []Candidate, threshold, absoluteFloor float64) []string {
	if len(cs) == 0 {
		return nil
	}

	// Step 1: find max raw final_score
	maxScore := 0.0
	for _, c := range cs {
		if c.FinalScore > maxScore {
			maxScore = c.FinalScore
		}
	}

	// If max is effectively zero, nothing qualifies.
	if maxScore < absoluteFloor {
		return nil
	}

	var ids []string
	for i := range cs {
		if cs[i].FinalScore < absoluteFloor {
			continue
		}
		normalised := cs[i].FinalScore / maxScore
		cs[i].SeedScore = normalised
		if normalised >= threshold {
			cs[i].IsSeed = true
			ids = append(ids, cs[i].ObjectID)
		}
	}
	return ids
}

// ─── Utilities ────────────────────────────────────────────────────────────────

// namespaceFrom builds a DataPlane namespace string from the request isolation
// fields (tenant_id / workspace_id), falling back to agent_id.
func namespaceFrom(req RetrievalRequest) string {
	if req.WorkspaceID != "" {
		return req.TenantID + "/" + req.WorkspaceID
	}
	if req.AgentID != "" {
		return req.AgentID
	}
	return ""
}

// buildMeta constructs QueryMeta from a completed candidate list.
func buildMeta(start time.Time, cs []Candidate) QueryMeta {
	latency := time.Since(start).Milliseconds()
	channels := make(map[string]bool)
	for _, c := range cs {
		for _, ch := range c.SourceChannels {
			channels[ch] = true
		}
	}
	used := make([]string, 0, len(channels))
	for ch := range channels {
		used = append(used, ch)
	}
	return QueryMeta{
		LatencyMs:    latency,
		ChannelsUsed: used,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
