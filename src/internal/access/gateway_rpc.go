package access

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"plasmod/src/internal/metrics"
	"plasmod/src/internal/schemas"
)

// WarmVectorsIngestResult is the JSON-shaped result of POST /v1/ingest/vectors.
type WarmVectorsIngestResult struct {
	Status     string `json:"status"`
	SegmentID  string `json:"segment_id"`
	Ingested   int    `json:"ingested"`
	VectorDim  int    `json:"vector_dim"`
	IndexType  string `json:"index_type"`
	DirectWarm bool   `json:"direct_warm"`
}

// ServiceIngestEvent ingests a cognitive event (POST /v1/ingest/events semantics).
func (g *Gateway) ServiceIngestEvent(ev schemas.Event) (any, error) {
	if err := g.acquireWriteSlot(); err != nil {
		return nil, err
	}
	defer g.releaseWriteSlot()

	if strings.TrimSpace(ev.EventID) == "" {
		ev.EventID = generateObjectID("evt")
	}
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		metrics.Global().RecordRetrievalError()
		return nil, err
	}
	return ack, nil
}

func (g *Gateway) acquireWriteSlot() error {
	select {
	case g.writeSem <- struct{}{}:
		atomic.AddInt32(&g.writeSemActive, 1)
		return nil
	default:
		return fmt.Errorf("too many concurrent writes; try again later")
	}
}

func (g *Gateway) releaseWriteSlot() {
	atomic.AddInt32(&g.writeSemActive, -1)
	<-g.writeSem
}

// ServiceIngestVectors builds a warm segment from caller-supplied vectors.
func (g *Gateway) ServiceIngestVectors(req schemas.WarmVectorsIngestRequest) (*WarmVectorsIngestResult, error) {
	req.SegmentID = strings.TrimSpace(req.SegmentID)
	if req.SegmentID == "" {
		req.SegmentID = "warm.default"
	}
	if len(req.Vectors) == 0 {
		return nil, fmt.Errorf("vectors is required")
	}
	indexType, err := schemas.NormalizeWarmIndexType(req.IndexType)
	if err != nil {
		return nil, err
	}
	if len(req.ObjectIDs) == 0 {
		req.ObjectIDs = make([]string, len(req.Vectors))
		for i := range req.Vectors {
			req.ObjectIDs[i] = fmt.Sprintf("%s_%d", req.SegmentID, i)
		}
	}
	if len(req.ObjectIDs) != len(req.Vectors) {
		return nil, fmt.Errorf("object_ids/vectors length mismatch")
	}
	var n int
	if indexType == schemas.WarmIndexHNSW && req.IVFNlist == 0 && req.IVFNprobe == 0 &&
		req.IVFM == 0 && req.IVFNbits == 0 && strings.TrimSpace(req.IVFSqType) == "" {
		n, err = g.runtime.IngestVectorsToWarmSegment(req.SegmentID, req.ObjectIDs, req.Vectors)
	} else {
		n, err = g.runtime.IngestVectorsToWarmSegmentWithType(
			req.SegmentID, req.ObjectIDs, req.Vectors,
			indexType, req.IVFNlist, req.IVFNprobe, req.IVFM, req.IVFNbits, req.IVFSqType,
		)
	}
	if err != nil {
		return nil, err
	}
	return &WarmVectorsIngestResult{
		Status:     "ok",
		SegmentID:  req.SegmentID,
		Ingested:   n,
		VectorDim:  len(req.Vectors[0]),
		IndexType:  indexType,
		DirectWarm: true,
	}, nil
}

// ServiceQuery executes POST /v1/query semantics.
func (g *Gateway) ServiceQuery(req schemas.QueryRequest) (any, error) {
	if strings.TrimSpace(req.WarmSegmentID) != "" {
		ids, err := g.runtime.SearchWarmSegment(req.WarmSegmentID, req.QueryText, req.TopK, req.EmbeddingVector)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"status":          "ok",
			"objects":         ids,
			"warm_segment_id": req.WarmSegmentID,
			"tier":            "warm_segment",
		}, nil
	}
	if req.LatestBatchOnly {
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		datasetName := strings.TrimSpace(req.DatasetName)
		sourceFileName := strings.TrimSpace(req.SourceFileName)
		if workspaceID == "" {
			return nil, fmt.Errorf("latest_batch_only requires workspace_id")
		}
		if datasetName == "" && sourceFileName == "" {
			return nil, fmt.Errorf("latest_batch_only requires dataset_name or source_file_name")
		}
	}
	resp := g.runtime.ExecuteQuery(req)
	return resp, nil
}

// ServiceQueryBatch executes POST /v1/query/batch warm-segment batch ANN.
func (g *Gateway) ServiceQueryBatch(req schemas.VectorWarmBatchQueryRequest) (*schemas.VectorWarmBatchQueryResponse, error) {
	segID := strings.TrimSpace(req.WarmSegmentID)
	if segID == "" {
		return nil, fmt.Errorf("warm_segment_id is required")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	nq := len(req.Vectors)
	if nq == 0 {
		return nil, fmt.Errorf("vectors must contain at least one row")
	}
	dim := len(req.Vectors[0])
	if dim <= 0 {
		return nil, fmt.Errorf("each vector row must be non-empty")
	}
	for i, row := range req.Vectors {
		if len(row) != dim {
			return nil, fmt.Errorf("vectors[%d] length %d must match dim %d", i, len(row), dim)
		}
	}
	sources, lineage, err := schemas.ResolveWarmVectorBatchLineage(req.AgentMode, nq, req.SourceIDs, req.RowLineage)
	if err != nil {
		return nil, err
	}
	flat := make([]float32, 0, nq*dim)
	for _, row := range req.Vectors {
		flat = append(flat, row...)
	}
	rowObjIDs, rowDists, err := g.runtime.SearchWarmSegmentBatchObjectIDs(segID, nq, topK, flat, req.SearchRaw)
	if err != nil {
		return nil, err
	}
	byIdx := schemas.MergeWarmBatchLineage(rowObjIDs, lineage, len(sources))
	bySource := make(map[string][]string, len(sources))
	for i, sid := range sources {
		bySource[sid] = byIdx[i]
	}
	rows := make([]schemas.VectorWarmBatchRowResult, nq)
	for i := 0; i < nq; i++ {
		d := rowDists[i]
		if len(d) == 0 {
			d = nil
		}
		rows[i] = schemas.VectorWarmBatchRowResult{
			RowIndex:   i,
			ObjectIDs:  rowObjIDs[i],
			Distances:  d,
			SourceRefs: append([]int(nil), lineage[i]...),
		}
	}
	out := &schemas.VectorWarmBatchQueryResponse{
		Status:        "ok",
		AgentMode:     strings.TrimSpace(strings.ToLower(req.AgentMode)),
		WarmSegmentID: segID,
		SourceIDs:     sources,
		Rows:          rows,
		BySource:      bySource,
	}
	return out, nil
}

// MarshalJSONResponse encodes any service result as JSON bytes.
func MarshalJSONResponse(v any) ([]byte, error) {
	return json.Marshal(v)
}
