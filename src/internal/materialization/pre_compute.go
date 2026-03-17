package materialization

import (
	"strings"
	"time"

	"andb/src/internal/dataplane"
	"andb/src/internal/evidence"
	"andb/src/internal/schemas"
)

// PreComputeService runs at ingest time and builds an EvidenceFragment for
// every ingested object.  This implements the "DB-side pre-computation" design:
// the database pre-assembles partial evidence chains so that the query path can
// merge cached + delta rather than deriving from scratch on every request.
type PreComputeService struct {
	cache     *evidence.Cache
	policyTag string
}

func NewPreComputeService(cache *evidence.Cache) *PreComputeService {
	return &PreComputeService{cache: cache, policyTag: "default"}
}

// Compute builds and caches an EvidenceFragment for the given event + ingest record.
// It is called immediately after materialization so the fragment is available for
// the first query that touches this object.
func (s *PreComputeService) Compute(ev schemas.Event, rec dataplane.IngestRecord) evidence.EvidenceFragment {
	tokens := tokenize(rec.Text)
	related := collectRelated(ev)
	filters := deriveFilters(ev)

	salience := computeSalience(ev, tokens)

	frag := evidence.EvidenceFragment{
		ObjectID:      rec.ObjectID,
		ObjectType:    ev.EventType,
		Namespace:     rec.Namespace,
		TextTokens:    tokens,
		RelatedIDs:    related,
		EdgeTypes:     []string{"derived_from", "causal"},
		PolicyFilters: filters,
		SalienceScore: salience,
		Level:         0,
		ComputedAt:    time.Now().UTC(),
		LogicalTS:     ev.LogicalTS,
	}

	s.cache.Put(frag)
	return frag
}

// Recompute invalidates and rebuilds the fragment for an already-known object.
// Called by the ReflectionPolicyWorker when policy or salience changes.
func (s *PreComputeService) Recompute(ev schemas.Event, rec dataplane.IngestRecord) evidence.EvidenceFragment {
	s.cache.Invalidate(rec.ObjectID)
	return s.Compute(ev, rec)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func tokenize(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return []string{}
	}
	parts := strings.Fields(text)
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, ".,!?;:")
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func collectRelated(ev schemas.Event) []string {
	ids := []string{}
	if ev.AgentID != "" {
		ids = append(ids, ev.AgentID)
	}
	if ev.SessionID != "" {
		ids = append(ids, ev.SessionID)
	}
	// CausalRefs is a slice; include all referenced events.
	ids = append(ids, ev.CausalRefs...)
	return ids
}

func deriveFilters(ev schemas.Event) []string {
	filters := []string{}
	if ev.AgentID != "" {
		filters = append(filters, "agent_id:"+ev.AgentID)
	}
	if ev.Visibility != "" {
		filters = append(filters, "visibility:"+ev.Visibility)
	}
	if ev.Source != "" {
		filters = append(filters, "source:"+ev.Source)
	}
	return filters
}

func computeSalience(ev schemas.Event, tokens []string) float64 {
	// Start from Importance if provided, else default to 0.5.
	base := ev.Importance
	if base == 0 {
		base = 0.5
	}
	if len(tokens) > 10 {
		base += 0.1
	}
	if len(ev.CausalRefs) > 0 {
		base += 0.1
	}
	if ev.Visibility == "global" {
		base += 0.2
	}
	if base > 1.0 {
		base = 1.0
	}
	return base
}
