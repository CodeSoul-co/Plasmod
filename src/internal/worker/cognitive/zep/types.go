package zep

import "plasmod/src/internal/schemas"

type episodeNode struct {
	ID        string
	MemoryID  string
	AgentID   string
	SessionID string
	Content   string
	TSValid   string
	TSTx      string
}

type entityNode struct {
	ID          string
	Name        string
	Summary     string
	EpisodeRefs map[string]struct{}
}

type factEdge struct {
	ID           string
	SrcEntityID  string
	DstEntityID  string
	Predicate    string
	Fact         string
	ValidAt      string
	InvalidAt    string
	CreatedAtTx  string
	ExpiredAtTx  string
	EpisodeRefID string
}

type communityNode struct {
	ID        string
	EntityIDs []string
	Summary   string
}

type graphState struct {
	episodes          map[string]episodeNode
	entities          map[string]entityNode
	entityByName      map[string]string
	facts             map[string]factEdge
	factsByEntity     map[string][]string
	communities       map[string]communityNode
	communityByEntity map[string]string
}

type candidateScore struct {
	MemoryID string
	Score    float64
	Signal   string
}

type rankedList []candidateScore

func toScoredMemories(rank rankedList, byID map[string]schemas.Memory) []schemas.ScoredMemory {
	out := make([]schemas.ScoredMemory, 0, len(rank))
	for _, c := range rank {
		mem, ok := byID[c.MemoryID]
		if !ok {
			continue
		}
		out = append(out, schemas.ScoredMemory{
			Memory: mem,
			Score:  c.Score,
			Signal: c.Signal,
		})
	}
	return out
}
