package segmentstore

import (
	"sort"
	"strings"
)

type SearchRequest struct {
	Query          string
	TopK           int
	Namespace      string
	MinEventUnixTS int64
	MaxEventUnixTS int64
	IncludeGrowing bool
}

type SearchHit struct {
	ObjectID  string
	Score     float64
	Partition string
}

type SearchResult struct {
	Hits            []SearchHit
	ScannedSegments []string
	PlannedMeta     []PartitionMeta
}

// Searcher executes a simple lexical scan over planned partitions.
type Searcher struct{}

func NewSearcher() *Searcher {
	return &Searcher{}
}

func (s *Searcher) Execute(req SearchRequest, plan Plan) SearchResult {
	query := strings.ToLower(strings.TrimSpace(req.Query))
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	hits := make([]SearchHit, 0, topK)
	scanned := NewSet[string]()
	for _, partition := range plan.CandidatePartitions {
		scanned.Insert(partition.ID)
		for _, row := range partition.SnapshotRows() {
			if req.MinEventUnixTS > 0 && row.EventUnixTS > 0 && row.EventUnixTS < req.MinEventUnixTS {
				continue
			}
			if req.MaxEventUnixTS > 0 && row.EventUnixTS > 0 && row.EventUnixTS > req.MaxEventUnixTS {
				continue
			}
			score := lexicalScore(query, strings.ToLower(row.Text))
			if score <= 0 {
				continue
			}
			hits = append(hits, SearchHit{ObjectID: row.ObjectID, Score: score, Partition: partition.ID})
		}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > topK {
		hits = hits[:topK]
	}

	return SearchResult{Hits: hits, ScannedSegments: scanned.Collect(), PlannedMeta: plan.Meta}
}

func lexicalScore(query string, text string) float64 {
	if query == "" || text == "" {
		return 0
	}
	if strings.Contains(text, query) {
		return 1.0
	}

	parts := strings.Fields(query)
	if len(parts) == 0 {
		return 0
	}

	matches := 0
	for _, part := range parts {
		if strings.Contains(text, part) {
			matches++
		}
	}
	return float64(matches) / float64(len(parts))
}
