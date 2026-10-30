package access

import (
	"encoding/json"
	"net/http"
	"strings"

	"plasmod/src/internal/worker"
)

func (g *Gateway) handleMaintenance(w http.ResponseWriter, r *http.Request) {
	g.handleMaintenanceOperation(w, r, "")
}
func (g *Gateway) handleMaintenanceOperation(w http.ResponseWriter, r *http.Request, operation string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req worker.MaintenanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if operation != "" {
		req.Operation = operation
	}
	if req.OperationID == "" {
		req.OperationID = r.Header.Get("Idempotency-Key")
	}
	if req.OperationID == "" {
		req.OperationID = generateObjectID("op")
	}
	out, err := g.runtime.Maintain(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, out)
}
func (g *Gateway) handleMaintenanceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	q := r.URL.Query()
	if q.Get("tenant_id") == "" || q.Get("workspace_id") == "" {
		http.Error(w, "tenant_id and workspace_id are required", 400)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/internal/memory/operations/")
	out, ok := g.runtime.MaintenanceStatus(q.Get("tenant_id"), q.Get("workspace_id"), id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, out)
}

func (g *Gateway) handleMemoryMutation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req worker.MemoryMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	out, err := g.runtime.MutateMemory(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, out)
}
func (g *Gateway) handleMutationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	q := r.URL.Query()
	if q.Get("tenant_id") == "" || q.Get("workspace_id") == "" {
		http.Error(w, "tenant_id and workspace_id are required", 400)
		return
	}
	out, ok := g.runtime.MutationStatus(q.Get("tenant_id"), q.Get("workspace_id"), strings.TrimPrefix(r.URL.Path, "/v1/internal/memory/mutations/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, out)
}

func (g *Gateway) handleConflictDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req worker.ConflictResolutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	out, err := g.runtime.ResolveMemoryConflict(req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, out)
}
