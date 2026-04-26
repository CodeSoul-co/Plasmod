package worker

import (
	"context"
	"os"
	"strings"

	"plasmod/src/internal/schemas"
)

// MemoryBackendLocalOnly is the only supported mode.
// Memory governance is handled by the algorithm dispatcher (e.g. MemoryBank),
// which drives storage lifecycle via SuggestedLifecycleState.
const MemoryBackendLocalOnly = "local_only"

type memoryBackendRouter struct {
	mode string
}

func newMemoryBackendRouterFromEnv() *memoryBackendRouter {
	mode := strings.TrimSpace(os.Getenv("PLASMOD_MEMORY_BACKEND_MODE"))
	if mode == "" {
		mode = MemoryBackendLocalOnly
	}
	if mode != MemoryBackendLocalOnly {
		mode = MemoryBackendLocalOnly
	}
	return &memoryBackendRouter{mode: mode}
}

func (m *memoryBackendRouter) Mode() string {
	if m == nil || m.mode == "" {
		return MemoryBackendLocalOnly
	}
	return m.mode
}

func (m *memoryBackendRouter) SetMode(mode string) bool {
	mode = strings.TrimSpace(mode)
	if mode == MemoryBackendLocalOnly {
		m.mode = mode
		return true
	}
	return false
}

func (m *memoryBackendRouter) ShouldShadowWrite() bool {
	return false
}

func (m *memoryBackendRouter) ShouldHybridRecall() bool {
	return false
}

func (m *memoryBackendRouter) WriteShadow(ctx context.Context, mem schemas.Memory, ev schemas.Event) error {
	return nil
}

func (m *memoryBackendRouter) RecallZep(
	ctx context.Context,
	query string,
	topK int,
	agentID, sessionID, tenantID, workspaceID string,
) ([]string, error) {
	return nil, nil
}

func (m *memoryBackendRouter) SoftDelete(ctx context.Context, memoryID string, reason string) error {
	return nil
}

func (m *memoryBackendRouter) HardDelete(ctx context.Context, memoryID string, reason string) error {
	return nil
}

func (m *memoryBackendRouter) Health(ctx context.Context) map[string]any {
	return map[string]any{
		"mode":   MemoryBackendLocalOnly,
		"status": "ok",
	}
}
