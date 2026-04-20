package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"plasmod/src/internal/schemas"
)

const (
	MemoryBackendLocalOnly    = "local_only"
	MemoryBackendShadowWrite  = "shadow_write"
	MemoryBackendHybridRecall = "hybrid_recall"
	MemoryBackendZepOnly      = "zep_only"
)

type memoryBackendRouter struct {
	mode     string
	zep      *zepMemoryBackend
	httpCli  *http.Client
	endpoint string
}

type zepMemoryBackend struct {
	baseURL    string
	apiKey     string
	collection string
	ingestPath string
	recallPath string
	healthPath string
	softDeletePath string
	hardDeletePath string
	client     *http.Client
}

func newMemoryBackendRouterFromEnv() *memoryBackendRouter {
	mode := strings.TrimSpace(os.Getenv("PLASMOD_MEMORY_BACKEND_MODE"))
	if mode == "" {
		mode = MemoryBackendLocalOnly
	}
	if mode != MemoryBackendShadowWrite && mode != MemoryBackendHybridRecall && mode != MemoryBackendZepOnly {
		mode = MemoryBackendLocalOnly
	}
	timeoutMS := 1200
	if raw := strings.TrimSpace(os.Getenv("PLASMOD_ZEP_TIMEOUT_MS")); raw != "" {
		if n, err := parsePositiveInt(raw); err == nil {
			timeoutMS = n
		}
	}
	client := &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PLASMOD_ZEP_BASE_URL")), "/")
	apiKey := strings.TrimSpace(os.Getenv("PLASMOD_ZEP_API_KEY"))
	collection := strings.TrimSpace(os.Getenv("PLASMOD_ZEP_COLLECTION"))
	ingestPath := resolveZepPath(
		strings.TrimSpace(os.Getenv("PLASMOD_ZEP_INGEST_PATH")),
		"/v1/memory/ingest",
	)
	recallPath := resolveZepPath(
		strings.TrimSpace(os.Getenv("PLASMOD_ZEP_RECALL_PATH")),
		"/v1/memory/recall",
	)
	healthPath := resolveZepPath(
		strings.TrimSpace(os.Getenv("PLASMOD_ZEP_HEALTH_PATH")),
		"/healthz",
	)
	softDeletePath := resolveZepPath(
		strings.TrimSpace(os.Getenv("PLASMOD_ZEP_SOFT_DELETE_PATH")),
		"/v1/memory/soft-delete",
	)
	hardDeletePath := resolveZepPath(
		strings.TrimSpace(os.Getenv("PLASMOD_ZEP_HARD_DELETE_PATH")),
		"/v1/memory/hard-delete",
	)
	var zep *zepMemoryBackend
	if baseURL != "" {
		zep = &zepMemoryBackend{
			baseURL:    baseURL,
			apiKey:     apiKey,
			collection: collection,
			ingestPath: ingestPath,
			recallPath: recallPath,
			healthPath: healthPath,
			softDeletePath: softDeletePath,
			hardDeletePath: hardDeletePath,
			client:     client,
		}
	}
	return &memoryBackendRouter{
		mode:    mode,
		zep:     zep,
		httpCli: client,
	}
}

func (m *memoryBackendRouter) Mode() string {
	if m == nil || m.mode == "" {
		return MemoryBackendLocalOnly
	}
	return m.mode
}

func (m *memoryBackendRouter) SetMode(mode string) bool {
	mode = strings.TrimSpace(mode)
	switch mode {
	case MemoryBackendLocalOnly, MemoryBackendShadowWrite, MemoryBackendHybridRecall, MemoryBackendZepOnly:
		m.mode = mode
		return true
	default:
		return false
	}
}

func (m *memoryBackendRouter) ShouldShadowWrite() bool {
	mode := m.Mode()
	return mode == MemoryBackendShadowWrite || mode == MemoryBackendHybridRecall || mode == MemoryBackendZepOnly
}

func (m *memoryBackendRouter) ShouldHybridRecall() bool {
	return m.Mode() == MemoryBackendHybridRecall
}

func (m *memoryBackendRouter) WriteShadow(ctx context.Context, mem schemas.Memory, ev schemas.Event) error {
	if m == nil || !m.ShouldShadowWrite() || m.zep == nil {
		return nil
	}
	return m.zep.WriteMemory(ctx, mem, ev)
}

func (m *memoryBackendRouter) RecallZep(
	ctx context.Context,
	query string,
	topK int,
	agentID, sessionID, tenantID, workspaceID string,
) ([]string, error) {
	if m == nil || m.zep == nil {
		return nil, nil
	}
	return m.zep.Recall(ctx, query, topK, agentID, sessionID, tenantID, workspaceID)
}

func (m *memoryBackendRouter) SoftDelete(ctx context.Context, memoryID string, reason string) error {
	if m == nil || m.zep == nil || strings.TrimSpace(memoryID) == "" {
		return nil
	}
	return m.zep.SoftDelete(ctx, memoryID, reason)
}

func (m *memoryBackendRouter) HardDelete(ctx context.Context, memoryID string, reason string) error {
	if m == nil || m.zep == nil || strings.TrimSpace(memoryID) == "" {
		return nil
	}
	return m.zep.HardDelete(ctx, memoryID, reason)
}

func (m *memoryBackendRouter) Health(ctx context.Context) map[string]any {
	out := map[string]any{
		"mode":         m.Mode(),
		"zep_enabled":  m != nil && m.zep != nil,
		"zep_base_url": "",
	}
	if m == nil || m.zep == nil {
		out["status"] = "local_only"
		return out
	}
	out["zep_base_url"] = m.zep.baseURL
	ok, detail := m.zep.Health(ctx)
	if ok {
		out["status"] = "ok"
	} else {
		out["status"] = "degraded"
	}
	out["zep_health"] = detail
	return out
}

func (z *zepMemoryBackend) WriteMemory(ctx context.Context, mem schemas.Memory, ev schemas.Event) error {
	body := map[string]any{
		"memory_id":    mem.MemoryID,
		"agent_id":     mem.AgentID,
		"session_id":   mem.SessionID,
		"workspace_id": mem.Scope,
		"tenant_id":    ev.TenantID,
		"content":      mem.Content,
		"memory_type":  mem.MemoryType,
		"importance":   mem.Importance,
		"valid_from":   mem.ValidFrom,
		"metadata": map[string]any{
			"source_event_id": ev.EventID,
			"collection":      z.collection,
		},
	}
	_, err := z.postJSON(ctx, z.ingestPath, body)
	return err
}

func (z *zepMemoryBackend) Recall(
	ctx context.Context,
	query string,
	topK int,
	agentID, sessionID, tenantID, workspaceID string,
) ([]string, error) {
	if topK <= 0 {
		topK = 10
	}
	body := map[string]any{
		"query":        query,
		"top_k":        topK,
		"agent_id":     agentID,
		"session_id":   sessionID,
		"tenant_id":    tenantID,
		"workspace_id": workspaceID,
		"collection":   z.collection,
	}
	raw, err := z.postJSON(ctx, z.recallPath, body)
	if err != nil {
		return nil, err
	}
	ids := parseRecallIDs(raw)
	return ids, nil
}

func (z *zepMemoryBackend) Health(ctx context.Context) (bool, map[string]any) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, z.baseURL+z.healthPath, nil)
	if err != nil {
		return false, map[string]any{"error": err.Error()}
	}
	if z.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+z.apiKey)
	}
	resp, err := z.client.Do(req)
	if err != nil {
		return false, map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300, map[string]any{"status_code": resp.StatusCode}
}

func (z *zepMemoryBackend) SoftDelete(ctx context.Context, memoryID string, reason string) error {
	_, err := z.postJSON(ctx, z.softDeletePath, map[string]any{
		"memory_id":  memoryID,
		"reason":     reason,
		"collection": z.collection,
	})
	return err
}

func (z *zepMemoryBackend) HardDelete(ctx context.Context, memoryID string, reason string) error {
	_, err := z.postJSON(ctx, z.hardDeletePath, map[string]any{
		"memory_id":  memoryID,
		"reason":     reason,
		"collection": z.collection,
	})
	return err
}

func (z *zepMemoryBackend) postJSON(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, z.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if z.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+z.apiKey)
	}
	resp, err := z.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("zep http status=%d", resp.StatusCode)
	}
	return out, nil
}

func parseRecallIDs(raw map[string]any) []string {
	keys := []string{"memory_ids", "visible_memory_refs", "objects", "ids"}
	for _, key := range keys {
		if v, ok := raw[key]; ok {
			if ids := toStringSlice(v); len(ids) > 0 {
				return ids
			}
		}
	}
	return nil
}

func toStringSlice(v any) []string {
	switch vv := v.(type) {
	case []string:
		return vv
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func parsePositiveInt(raw string) (int, error) {
	n := 0
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid int")
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return n, nil
}

func resolveZepPath(path string, fallback string) string {
	if strings.TrimSpace(path) == "" {
		return fallback
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}
