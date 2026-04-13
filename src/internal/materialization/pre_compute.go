package materialization

import (
	"strings"
	"time"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/evidence"
	"plasmod/src/internal/schemas"
)

// PreComputeService runs at ingest time and builds an EvidenceFragment for
// every ingested object.  This implements the "DB-side pre-computation" design:
// the database pre-assembles partial evidence chains so that the query path can
// merge cached + delta rather than deriving from scratch on every request.
//
// All tunable parameters are sourced from cfg (schemas.AlgorithmConfig).
// Use NewPreComputeService (defaults) or NewPreComputeServiceWithConfig (custom).
type PreComputeService struct {
	cache *evidence.Cache
	cfg    schemas.AlgorithmConfig
}

var defaultPreComputeCfg = schemas.DefaultAlgorithmConfig()

// NewPreComputeService creates a PreComputeService with all defaults.
// Convenience wrapper for NewPreComputeServiceWithConfig(schemas.DefaultAlgorithmConfig()).
func NewPreComputeService(cache *evidence.Cache) *PreComputeService {
	if cache == nil {
		cache = evidence.NewCache(defaultPreComputeCfg.EvidenceCacheSize)
	}
	return NewPreComputeServiceWithConfig(cache, defaultPreComputeCfg)
}

// NewPreComputeServiceWithConfig creates a PreComputeService with an explicit cfg.
// Use schemas.DefaultAlgorithmConfig() as a baseline and override specific fields.
func NewPreComputeServiceWithConfig(cache *evidence.Cache, cfg schemas.AlgorithmConfig) *PreComputeService {
	if cache == nil {
		cache = evidence.NewCache(cfg.EvidenceCacheSize)
	}
	return &PreComputeService{cache: cache, cfg: cfg}
}

// Compute builds and caches an EvidenceFragment for the given event + ingest record.
// It is called immediately after materialization so the fragment is available for
// the first query that touches this object.
func (s *PreComputeService) Compute(ev schemas.Event, rec dataplane.IngestRecord) evidence.EvidenceFragment {
	tokens := tokenize(rec.Text)
	related := collectRelated(ev)
	filters := deriveFilters(ev)

	salience := s.computeSalience(ev, tokens)

	frag := evidence.EvidenceFragment{
		ObjectID:      rec.ObjectID,
		ObjectType:    ev.EventType,
		Namespace:     rec.Namespace,
		TextTokens:    tokens,
		RelatedIDs:    related,
		EdgeTypes:     []string{string(schemas.EdgeTypeDerivedFrom), string(schemas.EdgeTypeCausedBy)},
		PolicyFilters: filters,
		SalienceScore: salience,
		Level:         0,
		ComputedAt:    time.Now().UTC(),
		LogicalTS:     ev.LogicalTS,
	}

	if s.cache != nil {
		s.cache.Put(frag)
	}
	return frag
}

// Recompute invalidates and rebuilds the fragment for an already-known object.
// Called by the ReflectionPolicyWorker when policy or salience changes.
func (s *PreComputeService) Recompute(ev schemas.Event, rec dataplane.IngestRecord) evidence.EvidenceFragment {
	if s.cache != nil {
		s.cache.Invalidate(rec.ObjectID)
	}
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

// computeSalience scores the importance of an object for retrieval ranking.
// All thresholds and bonuses are sourced from s.cfg (schemas.AlgorithmConfig).
func (s *PreComputeService) computeSalience(ev schemas.Event, tokens []string) float64 {
	cfg := s.cfg
	base := ev.Importance
	if base == 0 {
		base = cfg.DefaultImportance
	}
	if len(tokens) > cfg.TokenCountThreshold {
		base += cfg.TokenBonus
	}
	if len(ev.CausalRefs) > 0 {
		base += cfg.CausalRefBonus
	}
	if ev.Visibility == "global" {
		base += cfg.GlobalVisibilityBonus
	}
	if base > cfg.SalienceCap {
		base = cfg.SalienceCap
	}
	return base
}
