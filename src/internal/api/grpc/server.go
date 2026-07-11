package grpcapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"plasmod/src/internal/access"
	plasmodv1 "plasmod/src/internal/api/grpc/pb/plasmod/v1"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/consistency"
)

// APIServer implements plasmod.v1.PlasmodAPIService over the shared Gateway service layer.
type APIServer struct {
	plasmodv1.UnimplementedPlasmodAPIServiceServer
	Gateway *access.Gateway
}

func (s *APIServer) Health(context.Context, *plasmodv1.HealthRequest) (*plasmodv1.HealthResponse, error) {
	return &plasmodv1.HealthResponse{Status: "ok", Transport: "grpc"}, nil
}

func (s *APIServer) IngestEvent(ctx context.Context, req *plasmodv1.IngestEventRequest) (*plasmodv1.IngestEventResponse, error) {
	if req == nil || strings.TrimSpace(req.GetEventJson()) == "" {
		return nil, status.Error(codes.InvalidArgument, "event_json is required")
	}
	var ev schemas.Event
	if err := json.Unmarshal([]byte(req.GetEventJson()), &ev); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid event_json: %v", err)
	}
	ack, err := s.Gateway.ServiceIngestEventContext(ctx, ev)
	if err != nil {
		return nil, mapServiceError(err)
	}
	raw, err := json.Marshal(ack)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal ack: %v", err)
	}
	return &plasmodv1.IngestEventResponse{AckJson: string(raw)}, nil
}

func (s *APIServer) IngestVectors(_ context.Context, req *plasmodv1.IngestVectorsRequest) (*plasmodv1.IngestVectorsResponse, error) {
	if req == nil || len(req.GetVectors()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "vectors is required")
	}
	vectors := make([][]float32, len(req.GetVectors()))
	for i, row := range req.GetVectors() {
		vectors[i] = append([]float32(nil), row.GetValues()...)
	}
	out, err := s.Gateway.ServiceIngestVectors(schemas.WarmVectorsIngestRequest{
		SegmentID: req.GetSegmentId(),
		ObjectIDs: append([]string(nil), req.GetObjectIds()...),
		Vectors:   vectors,
		IndexType: req.GetIndexType(),
		IVFNlist:  int(req.GetIvfNlist()),
		IVFNprobe: int(req.GetIvfNprobe()),
		IVFM:      int(req.GetIvfM()),
		IVFNbits:  int(req.GetIvfNbits()),
		IVFSqType: req.GetIvfSqType(),
	})
	if err != nil {
		return nil, mapServiceError(err)
	}
	return &plasmodv1.IngestVectorsResponse{
		Status:     out.Status,
		SegmentId:  out.SegmentID,
		Ingested:   int32(out.Ingested),
		VectorDim:  int32(out.VectorDim),
		IndexType:  out.IndexType,
		DirectWarm: out.DirectWarm,
	}, nil
}

func (s *APIServer) IngestVectorsFlat(_ context.Context, req *plasmodv1.IngestVectorsFlatRequest) (*plasmodv1.IngestVectorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	n := int(req.GetN())
	dim := int(req.GetDim())
	if err := validateFlatShape(n, dim, grpcMaxBatchVectors); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	vectors, err := float32sFromLittleEndianBytes(req.GetVectorsLe(), n*dim)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid vectors_le: %v", err)
	}
	out, err := s.Gateway.ServiceIngestVectorsFlat(access.WarmFlatVectorsIngestRequest{
		SegmentID:   req.GetSegmentId(),
		ObjectIDs:   append([]string(nil), req.GetObjectIds()...),
		FlatVectors: vectors,
		N:           n,
		Dim:         dim,
		IndexType:   req.GetIndexType(),
		IVFNlist:    int(req.GetIvfNlist()),
		IVFNprobe:   int(req.GetIvfNprobe()),
		IVFM:        int(req.GetIvfM()),
		IVFNbits:    int(req.GetIvfNbits()),
		IVFSqType:   req.GetIvfSqType(),
	})
	if err != nil {
		return nil, mapServiceError(err)
	}
	return &plasmodv1.IngestVectorsResponse{
		Status:     out.Status,
		SegmentId:  out.SegmentID,
		Ingested:   int32(out.Ingested),
		VectorDim:  int32(out.VectorDim),
		IndexType:  out.IndexType,
		DirectWarm: out.DirectWarm,
	}, nil
}

func (s *APIServer) Query(ctx context.Context, req *plasmodv1.QueryRequest) (*plasmodv1.QueryResponse, error) {
	if req == nil || strings.TrimSpace(req.GetQueryJson()) == "" {
		return nil, status.Error(codes.InvalidArgument, "query_json is required")
	}
	var q schemas.QueryRequest
	if err := json.Unmarshal([]byte(req.GetQueryJson()), &q); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid query_json: %v", err)
	}
	resp, err := s.Gateway.ServiceQueryContext(ctx, q)
	if err != nil {
		return nil, mapServiceError(err)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal query response: %v", err)
	}
	return &plasmodv1.QueryResponse{ResultJson: string(raw)}, nil
}

func (s *APIServer) QueryBatch(_ context.Context, req *plasmodv1.QueryBatchRequest) (*plasmodv1.QueryBatchResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	vectors := make([][]float32, len(req.GetVectors()))
	for i, row := range req.GetVectors() {
		vectors[i] = append([]float32(nil), row.GetValues()...)
	}
	lineage := make([][]int, len(req.GetRowLineage()))
	for i, refs := range req.GetRowLineage() {
		if refs == nil {
			continue
		}
		lineage[i] = append([]int(nil), intSliceFromInt32(refs.GetSourceIndices())...)
	}
	out, err := s.Gateway.ServiceQueryBatch(schemas.VectorWarmBatchQueryRequest{
		AgentMode:     req.GetAgentMode(),
		WarmSegmentID: req.GetWarmSegmentId(),
		TopK:          int(req.GetTopK()),
		Vectors:       vectors,
		SourceIDs:     append([]string(nil), req.GetSourceIds()...),
		RowLineage:    lineage,
		SearchRaw:     req.GetSearchRaw(),
	})
	if err != nil {
		return nil, mapServiceError(err)
	}
	resp := &plasmodv1.QueryBatchResponse{
		Status:        out.Status,
		AgentMode:     out.AgentMode,
		WarmSegmentId: out.WarmSegmentID,
		SourceIds:     append([]string(nil), out.SourceIDs...),
		BySource:      make(map[string]*plasmodv1.StringList, len(out.BySource)),
	}
	for k, ids := range out.BySource {
		resp.BySource[k] = &plasmodv1.StringList{Values: append([]string(nil), ids...)}
	}
	resp.Rows = make([]*plasmodv1.QueryBatchRow, len(out.Rows))
	for i, row := range out.Rows {
		resp.Rows[i] = &plasmodv1.QueryBatchRow{
			RowIndex:   int32(row.RowIndex),
			ObjectIds:  append([]string(nil), row.ObjectIDs...),
			Distances:  append([]float32(nil), row.Distances...),
			SourceRefs: int32SliceFromInt(row.SourceRefs),
		}
	}
	return resp, nil
}

func (s *APIServer) QueryBatchFlat(_ context.Context, req *plasmodv1.QueryBatchFlatRequest) (*plasmodv1.QueryBatchFlatResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	nq := int(req.GetNq())
	dim := int(req.GetDim())
	topK := int(req.GetTopK())
	if err := validateFlatShape(nq, dim, grpcMaxQueryBatch); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if topK <= 0 || topK > grpcMaxTopK {
		return nil, status.Errorf(codes.InvalidArgument, "invalid top_k=%d", topK)
	}
	queries, err := float32sFromLittleEndianBytes(req.GetQueriesLe(), nq*dim)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid queries_le: %v", err)
	}
	out, err := s.Gateway.ServiceQueryBatchFlat(access.WarmBatchFlatQueryRequest{
		WarmSegmentID: req.GetWarmSegmentId(),
		TopK:          topK,
		NQ:            nq,
		Dim:           dim,
		Queries:       queries,
		SearchRaw:     req.GetSearchRaw(),
		Serial:        req.GetSerial(),
	})
	if err != nil {
		return nil, mapServiceError(err)
	}
	return &plasmodv1.QueryBatchFlatResponse{
		Status:        out.Status,
		WarmSegmentId: out.WarmSegmentID,
		Nq:            int32(out.NQ),
		TopK:          int32(out.TopK),
		Dim:           int32(out.Dim),
		IdsLe:         int64sToLittleEndianBytes(out.IDs),
		DistancesLe:   float32sToLittleEndianBytes(out.Distances),
	}, nil
}

func mapServiceError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, msg)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, msg)
	}
	var acceptedNotVisible *consistency.AcceptedNotVisibleError
	var projectionFailure *consistency.ProjectionFailureError
	if errors.Is(err, consistency.ErrBackpressure) ||
		errors.Is(err, consistency.ErrPaused) ||
		errors.Is(err, consistency.ErrNotStarted) ||
		errors.As(err, &acceptedNotVisible) ||
		errors.As(err, &projectionFailure) ||
		strings.Contains(msg, "too many concurrent writes") {
		return status.Error(codes.Unavailable, msg)
	}
	return status.Error(codes.InvalidArgument, msg)
}

func intSliceFromInt32(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

func int32SliceFromInt(in []int) []int32 {
	out := make([]int32, len(in))
	for i, v := range in {
		out[i] = int32(v)
	}
	return out
}
