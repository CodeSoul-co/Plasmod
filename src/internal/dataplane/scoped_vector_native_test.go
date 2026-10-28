//go:build retrieval

package dataplane

import (
	"fmt"
	"testing"
)

func TestNativeScopedVector(t *testing.T) {
	for _, count := range []int{1, 2, 8, 9, 24} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			plane, err := NewSegmentDataPlaneWithEmbedder(NewTfidfEmbedder(384))
			if err != nil {
				t.Fatal(err)
			}
			vector := make([]float32, 384)
			vector[0] = 1
			for i := 0; i < count; i++ {
				id := fmt.Sprint(i)
				if err := plane.Ingest(IngestRecord{ObjectID: id, Text: "stored words", Attributes: map[string]string{"agent_id": id}, Embedding: vector}); err != nil {
					t.Fatal(err)
				}
			}
			if err := plane.Flush(); err != nil {
				t.Fatal(err)
			}
			if out := plane.Search(SearchInput{QueryText: "unrelated", QueryEmbedding: vector, ScopeFilters: map[string]string{"agent_id": "missing"}, TopK: 1}); len(out.ObjectIDs) != 0 {
				t.Fatalf("empty scope returned candidates: %+v", out)
			}
			for _, owner := range []string{"0", fmt.Sprint(count - 1)} {
				out := plane.Search(SearchInput{QueryText: "unrelated", QueryEmbedding: vector, ScopeFilters: map[string]string{"agent_id": owner}, TopK: 1})
				if len(out.ObjectIDs) != 1 || out.ObjectIDs[0] != owner || out.Tier != "vector" {
					t.Fatalf("owner=%s result=%+v", owner, out)
				}
			}
		})
	}
}
