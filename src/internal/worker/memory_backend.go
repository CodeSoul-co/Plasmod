package worker

import (
	"context"
	"os"
	"strings"

	"plasmod/src/internal/schemas"
)

// MemoryBackendLocalOnly is the only supported mode.
//
// IMPORTANT ARCHITECTURE NOTE:
// Plasmod keeps a single source of truth for memory storage internally.
// Governance strategies (for example MemoryBank and Zep-style logic) are
// expected to run as algorithm plugins through AlgorithmDispatch, not as an
// external storage backend.
//
// This router is retained only as a compatibility shim for existing admin/API
// fields that still expose "backend mode". Its behavior is intentionally
// local-only and non-routing.
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

// WriteShadow is a no-op in local-only architecture. Governance algorithms
// should influence lifecycle/ranking via AlgorithmDispatch instead.
func (m *memoryBackendRouter) WriteShadow(ctx context.Context, mem schemas.Memory, ev schemas.Event) error {
	return nil
}

// RecallZep is a legacy compatibility stub. Recall should come from local
// storage + algorithm re-ranking in DispatchRecall.
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
