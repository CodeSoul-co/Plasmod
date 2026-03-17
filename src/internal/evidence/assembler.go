package evidence

import (
	"fmt"

	"andb/src/internal/dataplane"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
)

// Assembler converts retrieval output into the evidence-oriented response
// contract exposed by the current API.
//
// When a Cache is attached, the Assembler merges pre-computed EvidenceFragments
// (built at ingest time by the PreComputeService) with any delta evidence
// derived at query time.  This is the "DB-side pre-computation" design:
// most of the proof-trace work is amortised over ingest so queries are fast.
type Assembler struct {
	cache     *Cache
	edgeStore storage.GraphEdgeStore
}

// NewAssembler creates an Assembler without a cache (backward-compatible).
func NewAssembler() *Assembler { return &Assembler{} }

// NewCachedAssembler creates an Assembler that uses the provided EvidenceCache.
func NewCachedAssembler(cache *Cache) *Assembler {
	return &Assembler{cache: cache}
}

// WithEdgeStore attaches a GraphEdgeStore so the Assembler can perform
// 1-hop graph expansion over the retrieved object IDs and return typed edges
// in the QueryResponse.  Without this the Edges field is always empty.
func (a *Assembler) WithEdgeStore(es storage.GraphEdgeStore) *Assembler {
	a.edgeStore = es
	return a
}

// Build assembles a QueryResponse.
// If a cache is attached it merges pre-computed fragments; otherwise it falls
// back to the simple derivation path.
func (a *Assembler) Build(result dataplane.SearchOutput, filters []string) schemas.QueryResponse {
	// base trace steps (always present)
	trace := []string{"planner", "retrieval_search", "policy_filter", "response"}

	// annotate which tier(s) were hit
	if result.Tier != "" {
		trace = append(trace, "tier:"+result.Tier)
	}

	for _, seg := range result.PlannedSegments {
		trace = append(trace, "shard:"+seg.ID+":"+seg.State)
	}

	// merge pre-computed evidence fragments
	if a.cache != nil && len(result.ObjectIDs) > 0 {
		frags := a.cache.GetMany(result.ObjectIDs)
		trace = append(trace, a.assembleFromFragments(frags)...)
	}

	// delta evidence: scanned shards not already in trace
	trace = append(trace, result.ScannedSegments...)

	// 1-hop graph expansion: load all edges incident to retrieved objects
	edges := a.expandEdges(result.ObjectIDs)
	if len(edges) > 0 {
		trace = append(trace, fmt.Sprintf("graph_expansion:edges=%d", len(edges)))
	}

	return schemas.QueryResponse{
		Objects:        result.ObjectIDs,
		Edges:          edges,
		Provenance:     []string{"event_projection", "retrieval_projection", "fragment_cache", "graph_expansion"},
		Versions:       []schemas.ObjectVersion{},
		AppliedFilters: filters,
		ProofTrace:     trace,
	}
}

// expandEdges performs a 1-hop expansion over the retrieved object IDs.
// Returns nil when no edge store is attached (backward-compatible).
func (a *Assembler) expandEdges(objectIDs []string) []schemas.Edge {
	if a.edgeStore == nil || len(objectIDs) == 0 {
		return nil
	}
	return a.edgeStore.BulkEdges(objectIDs)
}

// assembleFromFragments converts pre-computed EvidenceFragments into proof-trace
// steps.  Called only when cache is available (hot path).
func (a *Assembler) assembleFromFragments(frags []EvidenceFragment) []string {
	steps := []string{}
	for _, f := range frags {
		if f.ObjectID == "" {
			continue
		}
		steps = append(steps, fmt.Sprintf("fragment:%s:level=%d:salience=%.2f",
			f.ObjectID, f.Level, f.SalienceScore))
		for _, rel := range f.RelatedIDs {
			steps = append(steps, fmt.Sprintf("edge:%s->%s", f.ObjectID, rel))
		}
		for _, filter := range f.PolicyFilters {
			steps = append(steps, "policy:"+filter)
		}
	}
	return steps
}
