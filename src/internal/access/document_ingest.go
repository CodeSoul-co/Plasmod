package access

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

type documentRequest struct {
	TenantID       string  `json:"tenant_id"`
	WorkspaceID    string  `json:"workspace_id"`
	AgentID        string  `json:"agent_id"`
	SessionID      string  `json:"session_id"`
	SourceID       string  `json:"source_id"`
	IdempotencyKey string  `json:"idempotency_key"`
	Title          string  `json:"title"`
	Text           string  `json:"text"`
	ChunkSize      int     `json:"chunk_size"`
	Overlap        int     `json:"overlap"`
	Importance     float64 `json:"importance"`
	UploadBatchID  string  `json:"upload_batch_id"`
	SegmentIndex   int     `json:"segment_index"`
	SegmentTotal   int     `json:"segment_total"`
}

func documentDigest(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}
func (g *Gateway) handleIngestDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req documentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text is required", 400)
		return
	}
	if req.ChunkSize <= 0 {
		req.ChunkSize = 1000
	}
	if req.Overlap < 0 {
		req.Overlap = 0
	}
	if req.Overlap >= req.ChunkSize {
		http.Error(w, "overlap must be less than chunk_size", 400)
		return
	}
	if req.Importance <= 0 {
		req.Importance = .5
	}
	if req.SegmentTotal <= 0 {
		req.SegmentTotal = 1
	}
	if req.SegmentTotal > 10000 || req.SegmentIndex < 0 || req.SegmentIndex >= req.SegmentTotal {
		http.Error(w, "invalid segment_index or segment_total", 400)
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	source := req.SourceID
	if source == "" {
		source = req.UploadBatchID
	}
	if source == "" {
		source = req.IdempotencyKey
	}
	if source == "" {
		source = documentDigest(req.Text)
	}
	if req.SegmentTotal > 1 && req.UploadBatchID == "" {
		http.Error(w, "upload_batch_id is required for segmented upload", 400)
		return
	}
	scope := []string{req.TenantID, req.WorkspaceID, req.AgentID, req.SessionID, source}
	batchID := "batch_" + documentDigest(scope)
	// Serializes manifest validation and chunk writes within the single runtime.
	g.documentMu.Lock()
	defer g.documentMu.Unlock()
	save := func(id, kind, value string) error {
		st := schemas.State{StateID: id, TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, AgentID: req.AgentID, SessionID: req.SessionID, StateType: kind, DerivedFromEventID: source, StateValue: value, Version: 1, Access: schemas.CanonicalAccess{TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, OwnerAgentID: req.AgentID, SessionID: req.SessionID, Visibility: "private"}}
		return g.store.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st})
	}
	meta := req
	meta.Text = ""
	meta.SegmentIndex = 0
	fingerprint := documentDigest(meta)
	manifestID := "document_manifest:" + batchID
	if saved, ok := g.store.Objects().GetState(manifestID); ok && saved.StateValue != fingerprint {
		http.Error(w, "document source/idempotency key reused with different metadata", 409)
		return
	}
	if err := save(manifestID, "document_manifest", fingerprint); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	partID := "document_part:" + batchID + ":" + strconv.Itoa(req.SegmentIndex)
	if saved, ok := g.store.Objects().GetState(partID); ok && saved.StateValue != req.Text {
		http.Error(w, "segment already exists with different text", 409)
		return
	}
	if int64(len(req.Text)) > resolveDocumentAssembledMaxBytes() {
		http.Error(w, "document exceeds size limit", 413)
		return
	}
	if err := save(partID, "document_segment", req.Text); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var full strings.Builder
	var missing []int
	var totalBytes int64
	for i := 0; i < req.SegmentTotal; i++ {
		part, ok := g.store.Objects().GetState("document_part:" + batchID + ":" + strconv.Itoa(i))
		if !ok {
			missing = append(missing, i)
			continue
		}
		totalBytes += int64(len(part.StateValue))
		if totalBytes > resolveDocumentAssembledMaxBytes() {
			http.Error(w, "assembled document exceeds size limit", 413)
			return
		}
		full.WriteString(part.StateValue)
	}
	if len(missing) > 0 {
		writeJSON(w, map[string]any{"status": "accumulating", "batch_id": batchID, "upload_batch_id": req.UploadBatchID, "segments_received": req.SegmentTotal - len(missing), "segments_total": req.SegmentTotal, "missing_segments": missing})
		return
	}
	fullText := full.String()
	chunks := splitTextIntoChunks(fullText, req.ChunkSize, req.Overlap)
	var memoryIDs []string
	var receipts []map[string]any
	failed := 0
	offset := 0
	for i, chunk := range chunks {
		id := "evt_doc_" + documentDigest([]string{batchID, strconv.Itoa(i)})
		// Locate trimmed chunks against the original Unicode text, retaining overlap.
		runes := []rune(fullText)
		start := offset
		if byteOffset := strings.Index(string(runes[offset:]), chunk); byteOffset >= 0 {
			start = offset + len([]rune(string(runes[offset:])[:byteOffset]))
		}
		end := start + len([]rune(chunk))
		offset = end - req.Overlap
		if offset < start+1 {
			offset = start + 1
		}
		if offset > len(runes) {
			offset = len(runes)
		}
		ev := schemas.Event{Identity: schemas.EventIdentity{EventID: id, TenantID: req.TenantID, WorkspaceID: req.WorkspaceID, ImportBatchID: batchID, Source: source}, Actor: schemas.EventActor{AgentID: req.AgentID, SessionID: req.SessionID}, Time: schemas.EventTime{EventTime: time.Now().UnixMilli()}, EventInfo: schemas.EventDescriptor{EventType: string(schemas.EventTypeMemoryWriteRequested), Importance: &req.Importance}, Access: schemas.EventAccess{Consistency: "strict"}, Payload: map[string]any{"text": chunk, "title": req.Title, "chunk_index": i, "chunk_total": len(chunks), "import_batch": batchID, "source_id": source, "source_start": start, "source_end": end}}
		ack, err := g.runtime.SubmitIngestContext(r.Context(), ev)
		receipt := map[string]any{"index": i, "event_id": id, "start": start, "end": end, "status": "ok"}
		if err != nil {
			failed++
			receipt["status"] = "failed"
			receipt["error"] = err.Error()
		} else {
			receipt["ack"] = ack
			if mid, ok := ack["memory_id"].(string); ok {
				memoryIDs = append(memoryIDs, mid)
			}
		}
		receipts = append(receipts, receipt)
	}
	status := "ok"
	if failed > 0 {
		status = "partial"
	}
	result := map[string]any{"status": status, "batch_id": batchID, "source_id": source, "chain_name": "main_chain", "chunks": len(chunks), "memory_ids": memoryIDs, "failed_count": failed, "receipts": receipts}
	data, _ := json.Marshal(result)
	if err := save("document_receipt:"+batchID, "document_receipt", string(data)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, result)
}
