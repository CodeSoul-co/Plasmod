package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"plasmod/src/internal/eventbackbone"
)

// RuntimeAPI is the minimal surface of *worker.Runtime needed by the transport
// layer.  Defining it as an interface here avoids an import cycle.
type RuntimeAPI interface {
	IngestVectorsToWarmSegment(segmentID string, objectIDs []string, vectors [][]float32) (int, error)
	UnloadWarmSegment(segmentID string) error
	SearchWarmSegment(segmentID, queryText string, topK int, queryVec []float32) ([]string, error)
	RegisterWarmSegment(segmentID string, objectIDs []string) error
	SearchWarmSegmentBatch(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error)
	SearchWarmSegmentBatchRaw(segmentID string, nq int, topK int, queries []float32) ([]int64, []float32, error)
}

// Server hosts the binary and streaming transport endpoints that complement
// the public HTTP/REST gateway.
type Server struct {
	runtime RuntimeAPI
	wal     eventbackbone.WAL
	bus     eventbackbone.Bus
}

// NewServer builds a transport server bound to the runtime/WAL/bus.
func NewServer(runtime RuntimeAPI, wal eventbackbone.WAL, bus eventbackbone.Bus) *Server {
	return &Server{runtime: runtime, wal: wal, bus: bus}
}

// RegisterRoutes wires all transport endpoints onto mux.
//
// Endpoints:
//
//	POST /v1/internal/rpc/ingest_batch      (binary, application/octet-stream)
//
//	POST /v1/internal/rpc/unload_segment   (JSON, {segment_id: string})
//	POST /v1/internal/rpc/query_warm          (binary, application/octet-stream)
//	POST /v1/internal/rpc/query_warm_batch    (binary, application/octet-stream)
//	POST /v1/internal/rpc/query_warm_batch_raw (binary, application/octet-stream, no plugin)
//	POST /v1/internal/rpc/register_warm      (JSON, convenience)
//	GET  /v1/wal/stream                       (SSE, text/event-stream)
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/internal/rpc/ingest_batch", s.handleIngestBatch)
	mux.HandleFunc("/v1/internal/rpc/unload_segment", s.handleUnloadSegment)
	mux.HandleFunc("/v1/internal/rpc/query_warm", s.handleQueryWarm)
	mux.HandleFunc("/v1/internal/rpc/query_warm_batch", s.handleQueryWarmBatch)
	mux.HandleFunc("/v1/internal/rpc/query_warm_batch_raw", s.handleQueryWarmBatchRaw)
	mux.HandleFunc("/v1/internal/rpc/register_warm", s.handleRegisterWarm)
	mux.HandleFunc("/v1/wal/stream", s.handleWALStream)
}

func (s *Server) handleIngestBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	req, err := DecodeIngestBatch(r.Body)
	if err != nil {
		http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	segID := strings.TrimSpace(req.SegmentID)
	if segID == "" {
		segID = "warm.default"
	}
	if len(req.ObjectIDs) != len(req.Vectors) {
		http.Error(w, "object_ids/vectors length mismatch", http.StatusBadRequest)
		return
	}
	for i, id := range req.ObjectIDs {
		if id == "" {
			req.ObjectIDs[i] = fmt.Sprintf("%s_%d", segID, i)
		}
	}
	t0 := time.Now()
	n, err := s.runtime.IngestVectorsToWarmSegment(segID, req.ObjectIDs, req.Vectors)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"segment_id": segID,
		"ingested":   n,
		"vector_dim": req.Dim,
		"elapsed_ms": float64(time.Since(t0).Microseconds()) / 1000.0,
	})
}

func (s *Server) handleQueryWarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	req, err := DecodeQueryWarm(r.Body)
	if err != nil {
		http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SegmentID) == "" {
		http.Error(w, "segment_id required", http.StatusBadRequest)
		return
	}
	ids, err := s.runtime.SearchWarmSegment(req.SegmentID, "", req.TopK, req.Vector)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if err := EncodeQueryWarmResponse(w, ids); err != nil {
		// Best effort; headers already written.
		return
	}
}

func (s *Server) handleQueryWarmBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	req, err := DecodeQueryWarmBatch(r.Body)
	if err != nil {
		http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SegmentID) == "" {
		http.Error(w, "segment_id required", http.StatusBadRequest)
		return
	}
	if req.NQ <= 0 || req.TopK <= 0 {
		http.Error(w, "nq and topk must be positive", http.StatusBadRequest)
		return
	}
	ids, dists, err := s.runtime.SearchWarmSegmentBatch(req.SegmentID, req.NQ, req.TopK, req.Queries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	EncodeQueryWarmBatchResponse(w, &QueryWarmBatchResponse{
		NQ:    req.NQ,
		TopK:  req.TopK,
		IDs:   ids,
		Dists: dists,
	})
}

func (s *Server) handleQueryWarmBatchRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	req, err := DecodeQueryWarmBatch(r.Body)
	if err != nil {
		http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SegmentID) == "" {
		http.Error(w, "segment_id required", http.StatusBadRequest)
		return
	}
	if req.NQ <= 0 || req.TopK <= 0 {
		http.Error(w, "nq and topk must be positive", http.StatusBadRequest)
		return
	}
	ids, dists, err := s.runtime.SearchWarmSegmentBatchRaw(req.SegmentID, req.NQ, req.TopK, req.Queries)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	EncodeQueryWarmBatchResponse(w, &QueryWarmBatchResponse{
		NQ:    req.NQ,
		TopK:  req.TopK,
		IDs:   ids,
		Dists: dists,
	})
}

func (s *Server) handleUnloadSegment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	type body struct {
		SegmentID string `json:"segment_id"`
	}
	var req body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SegmentID == "" {
		http.Error(w, "segment_id required", http.StatusBadRequest)
		return
	}
	if err := s.runtime.UnloadWarmSegment(req.SegmentID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "segment_id": req.SegmentID})
}

func (s *Server) handleRegisterWarm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type body struct {
		SegmentID string   `json:"segment_id"`
		ObjectIDs []string `json:"object_ids"`
	}
	var req body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.SegmentID) == "" || len(req.ObjectIDs) == 0 {
		http.Error(w, "segment_id and object_ids required", http.StatusBadRequest)
		return
	}
	if err := s.runtime.RegisterWarmSegment(req.SegmentID, req.ObjectIDs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"segment_id": req.SegmentID,
		"count":      len(req.ObjectIDs),
	})
}

// handleWALStream provides a Server-Sent Events feed of WAL entries.
//
// Query parameters:
//
//	from_lsn  (optional) — replay entries with LSN >= from_lsn before tailing
//	heartbeat (optional, default 15s) — comment-line heartbeat interval
//
// Each event is encoded as:
//
//	event: wal
//	data: {"lsn":123,"event":{...}}
func (s *Server) handleWALStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	fromLSN := int64(0)
	if v := strings.TrimSpace(r.URL.Query().Get("from_lsn")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			fromLSN = n
		}
	}
	heartbeat := 15 * time.Second
	if v := strings.TrimSpace(r.URL.Query().Get("heartbeat")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			heartbeat = d
		}
	}

	ctx := r.Context()
	ch := s.bus.Subscribe("wal.events")

	// Replay buffered entries first.
	if s.wal != nil {
		entries := s.wal.Scan(fromLSN)
		for _, e := range entries {
			if err := writeWALEvent(w, e); err != nil {
				return
			}
		}
		flusher.Flush()
	}

	hb := time.NewTicker(heartbeat)
	defer hb.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			entry, ok := msg.Body.(eventbackbone.WALEntry)
			if !ok {
				continue
			}
			if entry.LSN < fromLSN {
				continue
			}
			if err := writeWALEvent(w, entry); err != nil {
				return
			}
			flusher.Flush()
		case <-hb.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeWALEvent(w http.ResponseWriter, e eventbackbone.WALEntry) error {
	payload, err := json.Marshal(map[string]any{
		"lsn":   e.LSN,
		"event": e.Event,
	})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: wal\ndata: %s\n\n", payload); err != nil {
		return err
	}
	return nil
}

// Compile-time assertion that context import is used (for future ctx-aware ops).
var _ = context.Background
