package evidence

import (
	"fmt"
	"sort"
	"strings"

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
	cache        *Cache
	edgeStore    storage.GraphEdgeStore
	versionStore storage.SnapshotVersionStore
	objectStore  storage.ObjectStore
	policyStore  storage.PolicyStore
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

// WithVersionStore attaches a SnapshotVersionStore so the Assembler can
// populate QueryResponse.Versions for every returned object.
func (a *Assembler) WithVersionStore(vs storage.SnapshotVersionStore) *Assembler {
	a.versionStore = vs
	return a
}

// WithObjectStore attaches an ObjectStore so the Assembler can validate
// object types against canonical records and apply ObjectTypes filtering.
func (a *Assembler) WithObjectStore(os storage.ObjectStore) *Assembler {
	a.objectStore = os
	return a
}

// WithPolicyStore attaches a PolicyStore so the Assembler can annotate the
// proof trace with TTL-expiry and quarantine status for returned objects.
func (a *Assembler) WithPolicyStore(ps storage.PolicyStore) *Assembler {
	a.policyStore = ps
	return a
}

// Build assembles a QueryResponse.
// input carries the query filter context (ObjectTypes, MemoryTypes) and result
// carries the raw retrieval output.  ObjectTypes and MemoryTypes are applied as
// post-filters before building the final response.
func (a *Assembler) Build(input dataplane.SearchInput, result dataplane.SearchOutput, filters []string) schemas.QueryResponse {
	// post-filter by ObjectTypes / MemoryTypes using ID-prefix heuristic +
	// optional ObjectStore confirmation.
	outputIDs := a.filterByObjectTypes(result.ObjectIDs, input.ObjectTypes, input.MemoryTypes)

	// base trace steps (always present)
	trace := []string{"planner", "retrieval_search", "policy_filter", "response"}

	if len(input.ObjectTypes) > 0 {
		trace = append(trace, fmt.Sprintf("object_type_filter:%s", strings.Join(input.ObjectTypes, ",")))
	}
	if len(input.MemoryTypes) > 0 {
		trace = append(trace, fmt.Sprintf("memory_type_filter:%s", strings.Join(input.MemoryTypes, ",")))
	}

	// annotate which tier(s) were hit
	if result.Tier != "" {
		trace = append(trace, "tier:"+result.Tier)
	}

	for _, seg := range result.PlannedSegments {
		trace = append(trace, "shard:"+seg.ID+":"+seg.State)
	}

	// merge pre-computed evidence fragments
	if a.cache != nil && len(outputIDs) > 0 {
		frags := a.cache.GetMany(outputIDs)
		trace = append(trace, a.assembleFromFragments(frags)...)
	}

	// delta evidence: scanned shards not already in trace
	trace = append(trace, result.ScannedSegments...)

	// 1-hop graph expansion: load all edges incident to retrieved objects
	edges := a.expandEdges(outputIDs)
	if len(edges) > 0 {
		trace = append(trace, fmt.Sprintf("graph_expansion:edges=%d", len(edges)))
	}

	// governance annotations: TTL-expired / quarantined objects are flagged
	// in the proof trace (not removed — callers may opt to exclude them).
	if a.policyStore != nil {
		trace = append(trace, a.governanceAnnotations(outputIDs)...)
	}

	// populate ObjectVersions for returned objects
	versions := a.resolveVersions(outputIDs)

	return schemas.QueryResponse{
		Objects:        outputIDs,
		Edges:          edges,
		Provenance:     a.resolveProvenance(outputIDs, edges, versions),
		Versions:       versions,
		AppliedFilters: filters,
		ProofTrace:     trace,
		ChainTraces:    schemas.ChainTraceSlots{},
	}
}

// filterByObjectTypes removes IDs whose inferred type is not in the allow-list.
// Uses ID-prefix heuristics as the primary signal; falls back to ObjectStore
// lookup when available.  Empty allow-lists mean no filtering.
func (a *Assembler) filterByObjectTypes(ids []string, allowedTypes []string, allowedMemTypes []string) []string {
	if len(allowedTypes) == 0 && len(allowedMemTypes) == 0 {
		return ids
	}
	typeSet := make(map[string]bool, len(allowedTypes))
	for _, t := range allowedTypes {
		typeSet[t] = true
	}
	memTypeSet := make(map[string]bool, len(allowedMemTypes))
	for _, t := range allowedMemTypes {
		memTypeSet[t] = true
	}

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		objType := inferObjectTypeFromID(id)
		if len(typeSet) > 0 && !typeSet[objType] {
			continue
		}
		// For memory objects, optionally filter by MemoryType.
		if len(memTypeSet) > 0 && objType == "memory" && a.objectStore != nil {
			if mem, ok := a.objectStore.GetMemory(id); ok && !memTypeSet[mem.MemoryType] {
				continue
			}
		}
		out = append(out, id)
	}
	return out
}

// inferObjectTypeFromID infers the canonical object type from the well-known
// ID prefix scheme (IDPrefixMemory = "mem_", IDPrefixState = "state_", …).
func inferObjectTypeFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "mem_") || strings.HasPrefix(id, "summary_") || strings.HasPrefix(id, "shared_"):
		return "memory"
	case strings.HasPrefix(id, "evt_"):
		return "event"
	case strings.HasPrefix(id, "state_"):
		return "state"
	case strings.HasPrefix(id, "art_") || strings.HasPrefix(id, "tool_trace_"):
		return "artifact"
	default:
		return "memory" // default: treat unknown IDs as memory
	}
}

// governanceAnnotations returns proof-trace steps that flag policy violations
// (TTL-expired or quarantined) on the returned objects.  Objects are flagged
// but NOT removed here; callers decide whether to act on the annotations.
func (a *Assembler) governanceAnnotations(objectIDs []string) []string {
	steps := []string{}
	for _, id := range objectIDs {
		pols := a.policyStore.GetPolicies(id)
		for _, pol := range pols {
			if pol.QuarantineFlag {
				steps = append(steps, fmt.Sprintf("governance:quarantined:%s", id))
			}
			if pol.VerifiedState == string(schemas.VerifiedStateRetracted) {
				steps = append(steps, fmt.Sprintf("governance:retracted:%s", id))
			}
		}
	}
	return steps
}

// resolveVersions looks up the latest ObjectVersion for each returned object.
// Returns an empty slice when no VersionStore is wired in.
func (a *Assembler) resolveVersions(objectIDs []string) []schemas.ObjectVersion {
	if a.versionStore == nil {
		return []schemas.ObjectVersion{}
	}
	versions := make([]schemas.ObjectVersion, 0, len(objectIDs))
	for _, id := range objectIDs {
		if v, ok := a.versionStore.LatestVersion(id); ok {
			versions = append(versions, v)
		}
	}
	return versions
}

func (a *Assembler) resolveProvenance(objectIDs []string, edges []schemas.Edge, versions []schemas.ObjectVersion) []string {
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}

	for _, e := range edges {
		add(e.ProvenanceRef)
	}
	for _, v := range versions {
		add(v.MutationEventID)
	}
	if a.objectStore != nil {
		for _, id := range objectIDs {
			if m, ok := a.objectStore.GetMemory(id); ok {
				for _, src := range m.SourceEventIDs {
					add(src)
				}
				add(m.ProvenanceRef)
				continue
			}
			if s, ok := a.objectStore.GetState(id); ok {
				add(s.DerivedFromEventID)
				continue
			}
			if art, ok := a.objectStore.GetArtifact(id); ok {
				add(art.ProducedByEventID)
			}
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
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
