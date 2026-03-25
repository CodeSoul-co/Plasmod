package integration_test

import (
	"net/http"
	"testing"
)

// TestTopologyConnectivity validates the runtime worker-node topology and
// that all nodes report a ready state.
func TestTopologyConnectivity(t *testing.T) {
	status, topo := doJSON(t, http.MethodGet, "/v1/admin/topology", nil)
	if status != http.StatusOK {
		t.Fatalf("status: got %d, want 200", status)
	}
	assertKeys(t, topo, "nodes", "segments", "indexes")

	t.Run("nodes field is a non-empty list", func(t *testing.T) {
		nodes, ok := topo["nodes"].([]any)
		if !ok {
			t.Fatalf("nodes: expected []any, got %T", topo["nodes"])
		}
		if len(nodes) == 0 {
			t.Error("nodes list is empty — expected at least one worker node")
		}
		t.Logf("node count: %d", len(nodes))
	})

	t.Run("all nodes report ready state", func(t *testing.T) {
		nodes, _ := topo["nodes"].([]any)
		for i, n := range nodes {
			m, ok := n.(map[string]any)
			if !ok {
				t.Errorf("node[%d]: expected object, got %T", i, n)
				continue
			}
			if m["state"] != "ready" {
				t.Errorf("node[%d] id=%v type=%v state=%v: expected ready", i, m["id"], m["type"], m["state"])
			}
		}
	})

	t.Run("each node has id, type, state, capabilities", func(t *testing.T) {
		nodes, _ := topo["nodes"].([]any)
		for i, n := range nodes {
			m, ok := n.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"id", "type", "state", "capabilities"} {
				if _, exists := m[key]; !exists {
					t.Errorf("node[%d]: missing key %q", i, key)
				}
			}
		}
	})

	t.Run("segments field is a list", func(t *testing.T) {
		if _, ok := topo["segments"].([]any); !ok {
			// segments may be nil/null in a fresh server — accept both []any and nil
			if topo["segments"] != nil {
				t.Errorf("segments: expected array or null, got %T", topo["segments"])
			}
		}
	})

	t.Run("topology includes required worker types", func(t *testing.T) {
		nodes, _ := topo["nodes"].([]any)
		required := []string{"subgraph_executor_worker"}
		typeSet := make(map[string]bool)
		for _, n := range nodes {
			if m, ok := n.(map[string]any); ok {
				if typ, ok := m["type"].(string); ok {
					typeSet[typ] = true
				}
			}
		}
		for _, want := range required {
			if !typeSet[want] {
				t.Errorf("topology missing required worker type %q", want)
			}
		}
	})

	t.Run("topology has expected node count", func(t *testing.T) {
		nodes, _ := topo["nodes"].([]any)
		const wantNodes = 19 // 18 standard workers + 1 AlgorithmDispatchWorker
		if len(nodes) != wantNodes {
			t.Errorf("node count: got %d, want %d", len(nodes), wantNodes)
		}
	})

	t.Run("GET /v1/admin/topology rejects non-GET methods", func(t *testing.T) {
		resp := doRaw(t, http.MethodPost, "/v1/admin/topology", "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status: got %d, want 405", resp.StatusCode)
		}
	})
}
