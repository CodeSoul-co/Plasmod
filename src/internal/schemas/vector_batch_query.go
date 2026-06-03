package schemas

import (
	"fmt"
	"strings"
)

const (
	AgentModeSingleAgent = "single_agent"
	AgentModeMultiAgent  = "multi_agent"
)

// VectorWarmBatchQueryRequest is the JSON body for POST /v1/query/batch.
// Vectors are the matrix rows sent to the warm-segment ANN batch path (flat row-major internally).
type VectorWarmBatchQueryRequest struct {
	AgentMode     string `json:"agent_mode"`
	WarmSegmentID string `json:"warm_segment_id"`
	TopK          int    `json:"top_k"`
	// Vectors: one embedding per batch row (nq rows), all same dimension.
	Vectors [][]float32 `json:"vectors"`
	// SourceIDs label logical result sinks (e.g. chain step id, or per-agent query key).
	// For single_agent, may be omitted to auto-generate c_0..c_{nq-1} when row_lineage is omitted.
	SourceIDs []string `json:"source_ids,omitempty"`
	// RowLineage[r] holds indices into SourceIDs that should receive hits from vectors[r].
	// Omitted means identity mapping when len(SourceIDs)==len(Vectors).
	RowLineage [][]int `json:"row_lineage,omitempty"`
	// SearchRaw uses the raw Knowhere batch path (no L2NormSort batch plugin).
	SearchRaw bool `json:"search_raw,omitempty"`
}

// VectorWarmBatchRowResult is one matrix row after ANN + id resolution.
type VectorWarmBatchRowResult struct {
	RowIndex   int       `json:"row_index"`
	ObjectIDs  []string  `json:"object_ids"`
	Distances  []float32 `json:"distances,omitempty"`
	SourceRefs []int     `json:"source_refs"`
}

// VectorWarmBatchQueryResponse is returned from POST /v1/query/batch.
type VectorWarmBatchQueryResponse struct {
	Status        string                     `json:"status"`
	AgentMode     string                     `json:"agent_mode"`
	WarmSegmentID string                     `json:"warm_segment_id"`
	SourceIDs     []string                   `json:"source_ids"`
	Rows          []VectorWarmBatchRowResult `json:"rows"`
	BySource      map[string][]string        `json:"by_source"`
}

// ResolveWarmVectorBatchLineage validates dimensions and returns effective source ids and lineage.
func ResolveWarmVectorBatchLineage(agentMode string, nq int, sourceIDs []string, rowLineage [][]int) (sources []string, lineage [][]int, err error) {
	mode := strings.TrimSpace(strings.ToLower(agentMode))
	switch mode {
	case AgentModeSingleAgent, AgentModeMultiAgent:
	default:
		return nil, nil, fmt.Errorf("agent_mode must be %q or %q", AgentModeSingleAgent, AgentModeMultiAgent)
	}
	if nq <= 0 {
		return nil, nil, fmt.Errorf("vectors must contain at least one row")
	}

	if len(rowLineage) > 0 {
		if len(rowLineage) != nq {
			return nil, nil, fmt.Errorf("row_lineage length %d must equal number of vector rows %d", len(rowLineage), nq)
		}
		if len(sourceIDs) == 0 {
			return nil, nil, fmt.Errorf("source_ids is required when row_lineage is set")
		}
		ns := len(sourceIDs)
		lineage = rowLineage
		for r, refs := range lineage {
			if len(refs) == 0 {
				return nil, nil, fmt.Errorf("row_lineage[%d] must be non-empty", r)
			}
			for _, si := range refs {
				if si < 0 || si >= ns {
					return nil, nil, fmt.Errorf("row_lineage[%d] contains invalid source index %d (max %d)", r, si, ns-1)
				}
			}
		}
		return append([]string(nil), sourceIDs...), lineage, nil
	}

	// No explicit lineage: identity when |sources| == nq.
	if len(sourceIDs) == nq {
		lineage = make([][]int, nq)
		for i := range lineage {
			lineage[i] = []int{i}
		}
		return append([]string(nil), sourceIDs...), lineage, nil
	}

	if mode == AgentModeSingleAgent && len(sourceIDs) == 0 {
		sources = make([]string, nq)
		lineage = make([][]int, nq)
		for i := 0; i < nq; i++ {
			sources[i] = fmt.Sprintf("c_%d", i)
			lineage[i] = []int{i}
		}
		return sources, lineage, nil
	}

	return nil, nil, fmt.Errorf("provide row_lineage, or set len(source_ids)==%d (vector rows)", nq)
}

// MergeWarmBatchLineage fans out row hits to sources with stable de-duplication per source.
func MergeWarmBatchLineage(rowHits [][]string, lineage [][]int, nSources int) [][]string {
	out := make([][]string, nSources)
	seen := make([]map[string]struct{}, nSources)
	for i := range seen {
		seen[i] = make(map[string]struct{})
	}
	for r, hits := range rowHits {
		if r >= len(lineage) {
			continue
		}
		for _, si := range lineage[r] {
			if si < 0 || si >= nSources {
				continue
			}
			for _, id := range hits {
				if id == "" {
					continue
				}
				if _, ok := seen[si][id]; ok {
					continue
				}
				seen[si][id] = struct{}{}
				out[si] = append(out[si], id)
			}
		}
	}
	return out
}
