package zep

import (
	"strings"
	"sync"

	"plasmod/src/internal/schemas"
)

// Algorithm implements a paper-aligned Zep-style memory plugin:
// - episodic + semantic + community graph state
// - bi-temporal fact edges
// - hybrid search (cosine/bm25/bfs) + rerank (rrf/mmr/episode/node-distance)
// Dispatcher remains responsible for persistence and lifecycle application.
type Algorithm struct {
	id         string
	cfg        Config
	mu         sync.RWMutex
	states     map[string]schemas.MemoryAlgorithmState
	graph      graphState
	memoryByID map[string]schemas.Memory
}

func New(id string, cfg Config) *Algorithm {
	return &Algorithm{
		id:     id,
		cfg:    cfg,
		states: make(map[string]schemas.MemoryAlgorithmState),
		graph: graphState{
			episodes:          map[string]episodeNode{},
			entities:          map[string]entityNode{},
			entityByName:      map[string]string{},
			facts:             map[string]factEdge{},
			factsByEntity:     map[string][]string{},
			communities:       map[string]communityNode{},
			communityByEntity: map[string]string{},
		},
		memoryByID: map[string]schemas.Memory{},
	}
}

func NewDefault(id string) *Algorithm {
	cfg, _ := LoadFromYAML() // fallback to defaults on error
	return New(id, cfg)
}

func (a *Algorithm) AlgorithmID() string { return a.id }

func (a *Algorithm) Ingest(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.MemoryAlgorithmState {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		a.ingestOneMemoryToGraph(m, now)
		st := schemas.MemoryAlgorithmState{
			MemoryID:                m.MemoryID,
			AlgorithmID:             a.id,
			Strength:                clamp01(0.4 + 0.3*m.Confidence + 0.3*m.Importance),
			RetentionScore:          clamp01(0.5 + 0.2*m.Confidence + 0.2*m.FreshnessScore + 0.1*m.Importance),
			SuggestedLifecycleState: string(schemas.MemoryLifecycleActive),
			UpdatedAt:               now,
		}
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

func (a *Algorithm) Update(memories []schemas.Memory, signals map[string]float64) []schemas.MemoryAlgorithmState {
	now := tsNow()
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		st := a.getOrInitLocked(m.MemoryID, now)
		st.Strength = clamp01(st.Strength + signals[m.MemoryID])
		st.RetentionScore = clamp01(0.7*st.RetentionScore + 0.3*st.Strength)
		st.UpdatedAt = now
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

func (a *Algorithm) Recall(query string, candidates []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.ScoredMemory {
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	byID := map[string]schemas.Memory{}
	for _, m := range candidates {
		byID[m.MemoryID] = m
	}
	searchRanks := a.runSearch(query, candidates, now)
	finalRank := a.runRerank(searchRanks, byID)

	if topK := a.cfg.Zep.Retrieval.TopK; topK > 0 && len(finalRank) > topK {
		finalRank = finalRank[:topK]
	}
	for _, c := range finalRank {
		m, ok := byID[c.MemoryID]
		if !ok {
			continue
		}
		st := a.getOrInitLocked(m.MemoryID, now)
		st.RecallCount++
		st.LastRecalledAt = now
		st.RetentionScore = clamp01(0.7*st.RetentionScore + 0.3*c.Score)
		st.UpdatedAt = now
		a.states[m.MemoryID] = st
	}
	return toScoredMemories(finalRank, byID)
}

func (a *Algorithm) Compress(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	if len(memories) < 2 {
		return nil
	}
	now := ctx.Timestamp
	if now == "" {
		now = tsNow()
	}
	summary := make([]string, 0, len(memories))
	src := make([]string, 0, len(memories))
	for _, m := range memories {
		if m.Summary != "" {
			summary = append(summary, m.Summary)
		} else if m.Content != "" {
			summary = append(summary, m.Content)
		}
		src = append(src, m.MemoryID)
	}
	if len(summary) == 0 {
		return nil
	}
	return []schemas.Memory{{
		MemoryID:       "zep_summary_" + strings.ReplaceAll(now, ":", "_"),
		MemoryType:     string(schemas.MemoryTypeSemantic),
		AgentID:        ctx.AgentID,
		SessionID:      ctx.SessionID,
		Content:        strings.Join(summary, " | "),
		Summary:        "zep summary",
		SourceEventIDs: src,
		Confidence:     0.8,
		Importance:     0.7,
		FreshnessScore: 0.6,
		IsActive:       true,
		LifecycleState: string(schemas.MemoryLifecycleCompressed),
		ValidFrom:      now,
	}}
}

func (a *Algorithm) Decay(memories []schemas.Memory, nowTS string) []schemas.MemoryAlgorithmState {
	now := nowTS
	if now == "" {
		now = tsNow()
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	out := make([]schemas.MemoryAlgorithmState, 0, len(memories))
	for _, m := range memories {
		st := a.getOrInitLocked(m.MemoryID, now)
		st.Strength = clamp01(st.Strength * 0.98)
		st.RetentionScore = clamp01(st.RetentionScore * 0.97)
		if st.RetentionScore < 0.15 {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleArchived)
		} else if st.RetentionScore < 0.30 {
			st.SuggestedLifecycleState = string(schemas.MemoryLifecycleStale)
		}
		st.UpdatedAt = now
		a.states[m.MemoryID] = st
		out = append(out, st)
	}
	return out
}

func (a *Algorithm) Summarize(memories []schemas.Memory, ctx schemas.AlgorithmContext) []schemas.Memory {
	return a.Compress(memories, ctx)
}

func (a *Algorithm) ExportState(memoryID string) (schemas.MemoryAlgorithmState, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	st, ok := a.states[memoryID]
	return st, ok
}

func (a *Algorithm) LoadState(state schemas.MemoryAlgorithmState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.states[state.MemoryID] = state
}

func (a *Algorithm) getOrInitLocked(memoryID, now string) schemas.MemoryAlgorithmState {
	if st, ok := a.states[memoryID]; ok {
		return st
	}
	st := schemas.MemoryAlgorithmState{
		MemoryID:                memoryID,
		AlgorithmID:             a.id,
		Strength:                0.5,
		RetentionScore:          0.5,
		SuggestedLifecycleState: string(schemas.MemoryLifecycleActive),
		UpdatedAt:               now,
	}
	a.states[memoryID] = st
	return st
}
