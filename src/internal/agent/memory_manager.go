package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"andb/src/internal/schemas"
)

// MemoryManager is the pluggable algorithm interface for memory lifecycle.
// Implementations communicate with CogDB via HTTP to drive the algorithm pipeline.
//
// Default implementation: BaselineMemoryManager (pure consolidation + summarization).
// Future implementations: MemoryBank, VectorGraph, etc.
type MemoryManager interface {
	// Name returns the algorithm identifier, e.g. "baseline", "memorybank".
	Name() string

	// Recall searches for memories relevant to the query and returns a scored
	// result set. scope controls the visibility boundary (e.g. "workspace",
	// "session", "agent"). topK limits the result count.
	Recall(ctx context.Context, query, scope string, topK int) (*schemas.MemoryView, error)

	// Ingest notifies the algorithm of new memories so it can initialize
	// per-memory AlgorithmicState. memoryIDs must reference existing Memory objects.
	Ingest(ctx context.Context, memoryIDs []string) error

	// Compress triggers level-0 → level-1 memory consolidation for agent+session.
	// Returns nil if the algorithm does not support consolidation.
	Compress(ctx context.Context, agentID, sessionID string) error

	// Summarize compresses memories into higher-level summaries (level 1+).
	// maxLevel controls the highest summary level (1 or 2).
	// Returns the summary Memory IDs, or nil if not supported.
	Summarize(ctx context.Context, agentID, sessionID string, maxLevel int) ([]schemas.Memory, error)

	// Decay applies forgetting decay to memories for agent+session.
	// Returns nil if the algorithm does not support decay.
	Decay(ctx context.Context, agentID, sessionID string) error
}

// BaselineMemoryManager implements MemoryManager by calling CogDB's internal
// algorithm-dispatch endpoints. It is the default MemoryManager for AgentSession.
type BaselineMemoryManager struct {
	httpClient  *http.Client
	cogdbURL    *url.URL
	agentID     string
	tenantID    string
	workspaceID string
}

// NewBaselineMemoryManager creates a BaselineMemoryManager that calls the given
// CogDB endpoint.
func NewBaselineMemoryManager(client *http.Client, cogdbURL, agentID, tenantID, workspaceID string) *BaselineMemoryManager {
	u, _ := url.Parse(cogdbURL) // malformed URL is a programming error; guard at session creation
	return &BaselineMemoryManager{
		httpClient:  client,
		cogdbURL:    u,
		agentID:     agentID,
		tenantID:    tenantID,
		workspaceID: workspaceID,
	}
}

// Name implements MemoryManager.
func (b *BaselineMemoryManager) Name() string { return "baseline" }

// Close is a no-op for BaselineMemoryManager. It satisfies io.Closer so callers
// can uniformly close any MemoryManager.
func (b *BaselineMemoryManager) Close() error { return nil }

// Recall implements MemoryManager.
func (b *BaselineMemoryManager) Recall(ctx context.Context, query, scope string, topK int) (*schemas.MemoryView, error) {
	if topK <= 0 {
		topK = 10
	}
	reqBody := struct {
		Query       string `json:"query"`
		Scope       string `json:"scope"`
		TopK        int    `json:"top_k"`
		AgentID     string `json:"agent_id"`
		SessionID   string `json:"session_id"`
		TenantID    string `json:"tenant_id"`
		WorkspaceID string `json:"workspace_id"`
	}{
		Query:       query,
		Scope:       scope,
		TopK:        topK,
		AgentID:     b.agentID,
		SessionID:   "",
		TenantID:    b.tenantID,
		WorkspaceID: b.workspaceID,
	}

	u := *b.cogdbURL
	u.Path = "/v1/internal/memory/recall"

	var view schemas.MemoryView
	if err := b.doPost(ctx, u.String(), reqBody, &view); err != nil {
		return nil, fmt.Errorf("BaselineMemoryManager.Recall: %w", err)
	}
	return &view, nil
}

// Ingest implements MemoryManager.
func (b *BaselineMemoryManager) Ingest(ctx context.Context, memoryIDs []string) error {
	reqBody := struct {
		MemoryIDs []string `json:"memory_ids"`
		AgentID   string   `json:"agent_id"`
		SessionID string   `json:"session_id"`
	}{
		MemoryIDs: memoryIDs,
		AgentID:   b.agentID,
		SessionID: "",
	}
	u := *b.cogdbURL
	u.Path = "/v1/internal/memory/ingest"
	return b.doPost(ctx, u.String(), reqBody, nil)
}

// Compress implements MemoryManager.
func (b *BaselineMemoryManager) Compress(ctx context.Context, agentID, sessionID string) error {
	reqBody := struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
	}{
		AgentID:   agentID,
		SessionID: sessionID,
	}
	u := *b.cogdbURL
	u.Path = "/v1/internal/memory/compress"
	return b.doPost(ctx, u.String(), reqBody, nil)
}

// Summarize implements MemoryManager.
func (b *BaselineMemoryManager) Summarize(ctx context.Context, agentID, sessionID string, maxLevel int) ([]schemas.Memory, error) {
	reqBody := struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		MaxLevel  int    `json:"max_level"`
	}{
		AgentID:   agentID,
		SessionID: sessionID,
		MaxLevel:  maxLevel,
	}
	u := *b.cogdbURL
	u.Path = "/v1/internal/memory/summarize"

	var out schemas.AlgorithmDispatchOutput
	if err := b.doPost(ctx, u.String(), reqBody, &out); err != nil {
		return nil, fmt.Errorf("BaselineMemoryManager.Summarize: %w", err)
	}

	// Fetch full Memory objects for the produced IDs.
	mems := make([]schemas.Memory, 0, len(out.ProducedIDs))
	for _, id := range out.ProducedIDs {
		mems = append(mems, schemas.Memory{MemoryID: id})
	}
	return mems, nil
}

// Decay implements MemoryManager.
func (b *BaselineMemoryManager) Decay(ctx context.Context, agentID, sessionID string) error {
	reqBody := struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
	}{
		AgentID:   agentID,
		SessionID: sessionID,
	}
	u := *b.cogdbURL
	u.Path = "/v1/internal/memory/decay"
	return b.doPost(ctx, u.String(), reqBody, nil)
}

// doPost POSTs body to the given URL and decodes the JSON response into dest.
// dest may be nil.
func (b *BaselineMemoryManager) doPost(ctx context.Context, url string, body, dest any) error {
	bts, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bts))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bts, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bts))
	}
	if dest != nil {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}
