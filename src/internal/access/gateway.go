package access

import (
	"encoding/json"
	"net/http"

	"andb/src/internal/coordinator"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker"
)

type Gateway struct {
	coord   *coordinator.Hub
	runtime *worker.Runtime
	store   storage.RuntimeStorage
}

func NewGateway(coord *coordinator.Hub, runtime *worker.Runtime, store storage.RuntimeStorage) *Gateway {
	return &Gateway{coord: coord, runtime: runtime, store: store}
}

func (g *Gateway) RegisterRoutes(mux *http.ServeMux) {
	// System
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/v1/admin/topology", g.handleTopology)

	// Event ingest & query
	mux.HandleFunc("/v1/ingest/events", g.handleIngest)
	mux.HandleFunc("/v1/query", g.handleQuery)

	// Canonical object CRUD
	mux.HandleFunc("/v1/agents", g.handleAgents)
	mux.HandleFunc("/v1/sessions", g.handleSessions)
	mux.HandleFunc("/v1/memory", g.handleMemory)
	mux.HandleFunc("/v1/states", g.handleStates)
	mux.HandleFunc("/v1/artifacts", g.handleArtifacts)
	mux.HandleFunc("/v1/edges", g.handleEdges)
	mux.HandleFunc("/v1/policies", g.handlePolicies)
	mux.HandleFunc("/v1/share-contracts", g.handleShareContracts)
}

func (g *Gateway) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev schemas.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ack, err := g.runtime.SubmitIngest(ev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ack)
}

func (g *Gateway) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req schemas.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := g.runtime.ExecuteQuery(req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g.runtime.Topology())
}

// ─── helper ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ─── /v1/agents ───────────────────────────────────────────────────────────────

func (g *Gateway) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, g.store.Objects().ListAgents())
	case http.MethodPost:
		var obj schemas.Agent
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutAgent(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "agent_id": obj.AgentID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/sessions ─────────────────────────────────────────────────────────────

func (g *Gateway) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		writeJSON(w, g.store.Objects().ListSessions(agentID))
	case http.MethodPost:
		var obj schemas.Session
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutSession(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "session_id": obj.SessionID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/memory ───────────────────────────────────────────────────────────────

func (g *Gateway) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListMemories(agentID, sessionID))
	case http.MethodPost:
		var obj schemas.Memory
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Memory.Put(obj)
		writeJSON(w, map[string]string{"status": "ok", "memory_id": obj.MemoryID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/states ───────────────────────────────────────────────────────────────

func (g *Gateway) handleStates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agentID := r.URL.Query().Get("agent_id")
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListStates(agentID, sessionID))
	case http.MethodPost:
		var obj schemas.State
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutState(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "state_id": obj.StateID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/artifacts ────────────────────────────────────────────────────────────

func (g *Gateway) handleArtifacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessionID := r.URL.Query().Get("session_id")
		writeJSON(w, g.store.Objects().ListArtifacts(sessionID))
	case http.MethodPost:
		var obj schemas.Artifact
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Object.PutArtifact(obj, "")
		writeJSON(w, map[string]string{"status": "ok", "artifact_id": obj.ArtifactID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/edges ────────────────────────────────────────────────────────────────

func (g *Gateway) handleEdges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, g.store.Edges().ListEdges())
	case http.MethodPost:
		var obj schemas.Edge
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.store.Edges().PutEdge(obj)
		writeJSON(w, map[string]string{"status": "ok", "edge_id": obj.EdgeID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/policies ─────────────────────────────────────────────────────────────

func (g *Gateway) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		objectID := r.URL.Query().Get("object_id")
		if objectID != "" {
			writeJSON(w, g.store.Policies().GetPolicies(objectID))
		} else {
			writeJSON(w, g.store.Policies().ListPolicies())
		}
	case http.MethodPost:
		var obj schemas.PolicyRecord
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.coord.Policy.Append(obj)
		writeJSON(w, map[string]string{"status": "ok", "policy_id": obj.PolicyID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ─── /v1/share-contracts ──────────────────────────────────────────────────────

func (g *Gateway) handleShareContracts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scope := r.URL.Query().Get("scope")
		if scope != "" {
			writeJSON(w, g.store.Contracts().ContractsByScope(scope))
		} else {
			writeJSON(w, g.store.Contracts().ListContracts())
		}
	case http.MethodPost:
		var obj schemas.ShareContract
		if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		g.store.Contracts().PutContract(obj)
		writeJSON(w, map[string]string{"status": "ok", "contract_id": obj.ContractID})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
