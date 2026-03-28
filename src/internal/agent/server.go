package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"
)

// ─── AgentGateway ─────────────────────────────────────────────────────────────

// AgentGateway is a standalone HTTP server that exposes the Agent SDK as REST
// endpoints. Agents connect via HTTP instead of importing the Go module.
//
// Unlike AgentSession (which is the module-level client), AgentGateway acts as
// the HTTP facade. Both share the same underlying logic — AgentSession methods
// are called from each HTTP handler.
//
// Example:
//
//	cfg := agent.LoadFromEnv()
//	gw := agent.NewAgentGateway(cfg)
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	if err := gw.Serve(ctx); err != nil {
//	    log.Fatalf("gateway error: %v", err)
//	}
type AgentGateway struct {
	httpAddr string
	session  *AgentSession
	mux      *http.ServeMux
	srv      *http.Server
}

// NewAgentGateway creates a gateway with the given config.
// The CogDB endpoint and agent identity come from cfg.
func NewAgentGateway(cfg Config) (*AgentGateway, error) {
	session, err := NewAgentSession(cfg.AgentID, cfg.TenantID, cfg.WorkspaceID, cfg)
	if err != nil {
		return nil, err
	}

	addr := cfg.HTTPAddr
	if addr == "" {
		addr = ":9090"
	}

	gw := &AgentGateway{
		httpAddr: addr,
		session:  session,
		mux:      http.NewServeMux(),
	}

	gw.registerRoutes()
	gw.srv = &http.Server{
		Addr:         gw.httpAddr,
		Handler:      gw.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return gw, nil
}

// Serve starts the HTTP server and blocks until ctx is cancelled.
// It returns any fatal server error.
func (g *AgentGateway) Serve(ctx context.Context) error {
	log.Printf("[agent-gateway] listening on %s", g.httpAddr)
	errCh := make(chan error, 1)
	go func() {
		errCh <- g.srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		g.srv.Shutdown(shutdownCtx)
		return nil
	}
}

// Close shuts down the gateway and ends the active session.
func (g *AgentGateway) Close(ctx context.Context) error {
	return g.session.Close(ctx)
}

// ─── route registration ───────────────────────────────────────────────────────

func (g *AgentGateway) registerRoutes() {
	// Health
	g.mux.HandleFunc("/healthz", writeJSONFn(func(w http.ResponseWriter, r *http.Request) any {
		return map[string]string{"status": "ok", "provider": "cogdb-agent-gateway"}
	}))

	// Session — no session required
	g.mux.HandleFunc("/agent/session/start", g.handleSessionStart)
	g.mux.HandleFunc("/agent/session/end", g.handleSessionEnd)
	g.mux.HandleFunc("/agent/session/heartbeat", g.handleSessionHeartbeat)
	g.mux.HandleFunc("/agent/session", g.handleSessionGet)
	g.mux.HandleFunc("/agent/session/pause", g.handleSessionPause)
	g.mux.HandleFunc("/agent/session/resume", g.handleSessionResume)

	// Events — wrapped with per-request session check
	g.mux.HandleFunc("/agent/events/message", g.wrapRequireSession(g.handleEventMessage))
	g.mux.HandleFunc("/agent/events/thought", g.wrapRequireSession(g.handleEventThought))
	g.mux.HandleFunc("/agent/events/tool-call", g.wrapRequireSession(g.handleEventToolCall))
	g.mux.HandleFunc("/agent/events/tool-result", g.wrapRequireSession(g.handleEventToolResult))
	g.mux.HandleFunc("/agent/events/checkpoint", g.wrapRequireSession(g.handleEventCheckpoint))
	g.mux.HandleFunc("/agent/events/reflection", g.wrapRequireSession(g.handleEventReflection))

	// Memory / Query
	g.mux.HandleFunc("/agent/query", g.wrapRequireSession(g.handleQuery))
	g.mux.HandleFunc("/agent/memories", g.wrapRequireSession(g.handleMemories))
	g.mux.HandleFunc("/agent/memory/recall", g.wrapRequireSession(g.handleMemoryRecall))
	g.mux.HandleFunc("/agent/memory/ingest", g.wrapRequireSession(g.handleMemoryIngest))
	g.mux.HandleFunc("/agent/memory/compress", g.wrapRequireSession(g.handleMemoryCompress))
	g.mux.HandleFunc("/agent/memory/summarize", g.wrapRequireSession(g.handleMemorySummarize))
	g.mux.HandleFunc("/agent/memory/decay", g.wrapRequireSession(g.handleMemoryDecay))

	// Collaboration
	g.mux.HandleFunc("/agent/share", g.wrapRequireSession(g.handleShare))
	g.mux.HandleFunc("/agent/shared", g.wrapRequireSession(g.handleShared))
	g.mux.HandleFunc("/agent/conflict/resolve", g.wrapRequireSession(g.handleConflictResolve))
}

// writeJSONFn wraps a handler function that returns a value into an http.HandlerFunc.
func writeJSONFn(fn func(http.ResponseWriter, *http.Request) any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, fn(w, r))
	}
}

// writeJSON is a helper that encodes v as JSON with the correct content type.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a structured error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// wrapRequireSession returns an http.HandlerFunc that first checks the session is
// active before delegating to fn. It defers the session check to request time.
func (g *AgentGateway) wrapRequireSession(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.session.SessionID() == "" {
			writeError(w, http.StatusBadRequest, "session_not_started",
				"call POST /agent/session/start first")
			return
		}
		fn(w, r)
	}
}

// ─── session handlers ─────────────────────────────────────────────────────────

func (g *AgentGateway) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Goal         string `json:"goal"`
		TaskType     string `json:"task_type"`
		BudgetToken  int64  `json:"budget_token"`
		BudgetTimeMS int64  `json:"budget_time_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	id, err := g.session.StartSession(r.Context(), req.Goal, req.TaskType, req.BudgetToken, req.BudgetTimeMS)
	if err != nil {
		writeError(w, http.StatusBadRequest, "start_session", err.Error())
		return
	}
	writeJSON(w, map[string]any{"session_id": id, "status": "started"})
}

func (g *AgentGateway) handleSessionEnd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		FinalStatus string `json:"final_status"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore decode error; empty is fine
	if err := g.session.EndSession(r.Context(), req.FinalStatus); err != nil {
		writeError(w, http.StatusBadRequest, "end_session", err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ended"})
}

func (g *AgentGateway) handleSessionHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ack, err := g.session.Heartbeat(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "heartbeat", err.Error())
		return
	}
	writeJSON(w, ack)
}

func (g *AgentGateway) handleSessionGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, err := g.session.GetSession(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "get_session", err.Error())
		return
	}
	writeJSON(w, sess)
}

func (g *AgentGateway) handleSessionPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := g.session.PauseSession(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, "pause_session", err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "paused"})
}

func (g *AgentGateway) handleSessionResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := g.session.ResumeSession(r.Context(), req.SessionID); err != nil {
		writeError(w, http.StatusBadRequest, "resume_session", err.Error())
		return
	}
	writeJSON(w, map[string]any{"session_id": req.SessionID, "status": "resumed"})
}

// ─── event handlers ───────────────────────────────────────────────────────────

func (g *AgentGateway) handleEventMessage(w http.ResponseWriter, r *http.Request) {
	deliverEvent(g.session.SubmitUserMessage, w, r)
}
func (g *AgentGateway) handleEventThought(w http.ResponseWriter, r *http.Request) {
	deliverSingleFieldEvent("thought", func(ctx context.Context, v string) (*IngestAck, error) {
		return g.session.SubmitAgentThought(ctx, v)
	}, w, r)
}
func (g *AgentGateway) handleEventToolCall(w http.ResponseWriter, r *http.Request) {
	deliverToolEvent(g.session.SubmitToolCall, g.session.SubmitToolResult, w, r)
}
func (g *AgentGateway) handleEventToolResult(w http.ResponseWriter, r *http.Request) {
	deliverToolEvent(g.session.SubmitToolCall, g.session.SubmitToolResult, w, r)
}
func (g *AgentGateway) handleEventCheckpoint(w http.ResponseWriter, r *http.Request) {
	deliverSingleFieldEvent("reason", func(ctx context.Context, v string) (*IngestAck, error) {
		return g.session.SubmitCheckpoint(ctx, v)
	}, w, r)
}
func (g *AgentGateway) handleEventReflection(w http.ResponseWriter, r *http.Request) {
	deliverSingleFieldEvent("content", func(ctx context.Context, v string) (*IngestAck, error) {
		return g.session.SubmitReflection(ctx, v)
	}, w, r)
}

// deliverEvent parses a JSON body with a "text" or "content" field and calls submit.
func deliverEvent(submit func(context.Context, string, []string) (*IngestAck, error), w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Text       string   `json:"text"`
		Content    string   `json:"content"`
		CausalRefs []string `json:"causal_refs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	text := req.Text
	if text == "" {
		text = req.Content
	}
	ack, err := submit(r.Context(), text, req.CausalRefs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "submit_event", err.Error())
		return
	}
	writeJSON(w, ack)
}

// deliverSingleFieldEvent parses a JSON body, extracts the named field, and calls submit.
func deliverSingleFieldEvent(fieldName string, submit func(context.Context, string) (*IngestAck, error), w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	v, ok := req[fieldName].(string)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "field '"+fieldName+"' not found or not a string")
		return
	}
	ack, err := submit(r.Context(), v)
	if err != nil {
		writeError(w, http.StatusBadRequest, "submit_event", err.Error())
		return
	}
	writeJSON(w, ack)
}

// deliverToolEvent handles tool_call and tool_result events.
func deliverToolEvent(submitToolCall func(context.Context, string, string) (*IngestAck, error), submitToolResult func(context.Context, string, string) (*IngestAck, error), w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ToolName string `json:"tool_name"`
		Args     string `json:"args"`
		Result   string `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var ack *IngestAck
	var err error
	if req.Result != "" {
		ack, err = submitToolResult(r.Context(), req.ToolName, req.Result)
	} else {
		ack, err = submitToolCall(r.Context(), req.ToolName, req.Args)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "submit_event", err.Error())
		return
	}
	writeJSON(w, ack)
}

// ─── memory/query handlers ─────────────────────────────────────────────────────

func (g *AgentGateway) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		QueryText string `json:"query"`
		Scope     string `json:"scope"`
		TopK      int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	view, err := g.session.Query(r.Context(), req.QueryText, req.Scope, req.TopK)
	if err != nil {
		writeError(w, http.StatusBadRequest, "query", err.Error())
		return
	}
	writeJSON(w, view)
}

func (g *AgentGateway) handleMemories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Limit int `json:"limit"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req) // ignore error; limit=0 is fine
	}
	mems, err := g.session.GetConversationHistory(r.Context(), req.Limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "get_memories", err.Error())
		return
	}
	writeJSON(w, mems)
}

func (g *AgentGateway) handleMemoryRecall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
		TopK  int    `json:"top_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	view, err := g.session.Query(r.Context(), req.Query, req.Scope, req.TopK)
	if err != nil {
		writeError(w, http.StatusBadRequest, "memory_recall", err.Error())
		return
	}
	writeJSON(w, view)
}

func (g *AgentGateway) handleMemoryIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MemoryIDs []string `json:"memory_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	// Delegate to MemoryManager via Ingest (not exposed directly on AgentSession).
	// Use the internal memory endpoint as a passthrough.
	if g.session.MemoryManager() == nil {
		writeError(w, http.StatusBadRequest, "memory_ingest", "no MemoryManager configured")
		return
	}
	if err := g.session.MemoryManager().Ingest(r.Context(), req.MemoryIDs); err != nil {
		writeError(w, http.StatusBadRequest, "memory_ingest", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "ingested": len(req.MemoryIDs)})
}

func (g *AgentGateway) handleMemoryCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := g.session.Compress(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, "memory_compress", err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (g *AgentGateway) handleMemorySummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MaxLevel int `json:"max_level"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore error; maxLevel=0 is fine
	mems, err := g.session.Summarize(r.Context(), req.MaxLevel)
	if err != nil {
		writeError(w, http.StatusBadRequest, "memory_summarize", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "summaries": mems})
}

func (g *AgentGateway) handleMemoryDecay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := g.session.Decay(r.Context()); err != nil {
		writeError(w, http.StatusBadRequest, "memory_decay", err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// ─── collaboration handlers ─────────────────────────────────────────────────────

func (g *AgentGateway) handleShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		MemoryID       string `json:"memory_id"`
		TargetAgentID  string `json:"target_agent_id"`
		ContractScope  string `json:"contract_scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var contract *ShareContract
	if req.ContractScope != "" {
		contract = &ShareContract{Scope: req.ContractScope}
	}
	id, err := g.session.ShareMemory(r.Context(), req.MemoryID, req.TargetAgentID, contract)
	if err != nil {
		writeError(w, http.StatusBadRequest, "share_memory", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "shared_memory_id": id})
}

func (g *AgentGateway) handleShared(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mems, err := g.session.ReceiveSharedMemories(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, "receive_shared", err.Error())
		return
	}
	writeJSON(w, mems)
}

func (g *AgentGateway) handleConflictResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	winner, err := g.session.ResolveConflict(r.Context(), req.LeftID, req.RightID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "resolve_conflict", err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "winner_id": winner})
}
