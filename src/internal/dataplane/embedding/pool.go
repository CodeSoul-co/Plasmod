package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// defaultClientPool is a pool of reusable HTTP clients keyed by timeout.
// Clients are returned to the pool after use; the pool grows unbounded
// up to maxTimeoutValues distinct entries.
var defaultClientPool = &clientPool{
	clients:  map[time.Duration]*http.Client{},
	maxConns: 64, // per-host connection limit
}

type clientPool struct {
	mu      sync.Mutex
	clients map[time.Duration]*http.Client
	// maxConns is the max idle connections per host (shared across all clients)
	maxConns int
}

// Get returns a reusable HTTP client for the given per-request timeout.
// The returned client should be returned via Put after use.
func (p *clientPool) Get(timeout time.Duration) *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[timeout]; ok {
		return c
	}
	c := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: p.maxConns,
			MaxConnsPerHost:     p.maxConns * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	p.clients[timeout] = c
	return c
}

// Put is a no-op in the current implementation. Clients are kept alive
// in the pool for reuse. Call Close() to drain the entire pool.
func (p *clientPool) Put(*http.Client) {}

// Close terminates all idle HTTP clients in the pool.
func (p *clientPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.clients {
		c.CloseIdleConnections()
	}
	p.clients = make(map[time.Duration]*http.Client)
}

// ─── HTTP request helper ───────────────────────────────────────────────────────

// doHTTPRequest performs a JSON POST and decodes the response into dest.
// It sets appropriate headers for the given auth scheme.
func doHTTPRequest(ctx context.Context, client *http.Client, url, apiKey, apiType string, body, dest any) error {
	bs, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bs))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	switch apiType {
	case "azure":
		// Azure OpenAI uses api-key header (or AAD token via AzureIdentity)
		req.Header.Set("api-key", apiKey)
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%w: HTTP %d — %s", ErrProviderUnavailable, resp.StatusCode, string(bodyBytes))
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response body: %w", err)
	}
	return nil
}
