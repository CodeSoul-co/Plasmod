package segmentstore

import "testing"

func TestIndex_InsertAndSearch(t *testing.T) {
	idx := NewIndex()

	idx.InsertObject("mem_1", "the quick brown fox", nil, "ws1", 0)
	idx.InsertObject("mem_2", "lazy dog jumps over", nil, "ws1", 0)
	idx.InsertObject("mem_3", "hello world agent", nil, "ws2", 0)

	req := SearchRequest{
		Query:          "quick fox",
		TopK:           5,
		Namespace:      "ws1",
		IncludeGrowing: true,
	}
	result := idx.Search(req)
	if len(result.Hits) == 0 {
		t.Fatal("Search: expected at least one hit for 'quick fox'")
	}
	if result.Hits[0].ObjectID != "mem_1" {
		t.Errorf("Search: top hit should be mem_1, got %q", result.Hits[0].ObjectID)
	}
}

func TestIndex_NamespaceFilter(t *testing.T) {
	idx := NewIndex()
	idx.InsertObject("obj_a", "alpha beta gamma", nil, "ws_a", 0)
	idx.InsertObject("obj_b", "alpha beta gamma", nil, "ws_b", 0)

	// Only search in ws_a namespace; obj_b should not appear.
	req := SearchRequest{Query: "alpha", TopK: 5, Namespace: "ws_a", IncludeGrowing: true}
	result := idx.Search(req)

	for _, h := range result.Hits {
		if h.ObjectID == "obj_b" {
			t.Errorf("Search: object from ws_b should not appear when filtering by ws_a")
		}
	}
}

func TestIndex_InsertObjectUpsertsWithinNamespace(t *testing.T) {
	idx := NewIndex()
	idx.InsertObject("obj", "obsolete text", nil, "ws", 10)
	idx.InsertObject("obj", "replacement text", nil, "ws", 20)

	var rows int
	for _, shard := range idx.shards {
		if shard.Namespace == "ws" {
			rows += shard.Meta().RowCount
		}
	}
	if got := rows; got != 1 {
		t.Fatalf("row count after upsert = %d, want 1", got)
	}
	if got := idx.Search(SearchRequest{
		Query: "obsolete", TopK: 5, Namespace: "ws", IncludeGrowing: true,
	}); len(got.Hits) != 0 {
		t.Fatalf("obsolete record remained searchable after upsert: %+v", got.Hits)
	}
	got := idx.Search(SearchRequest{
		Query: "replacement", TopK: 5, Namespace: "ws", IncludeGrowing: true,
	})
	if len(got.Hits) != 1 || got.Hits[0].ObjectID != "obj" {
		t.Fatalf("replacement record not searchable after upsert: %+v", got.Hits)
	}
}

func TestIndex_InsertObjectKeepsNamespacesIndependent(t *testing.T) {
	idx := NewIndex()
	idx.InsertObject("obj", "workspace one", nil, "ws-1", 10)
	idx.InsertObject("obj", "workspace two", nil, "ws-2", 20)

	for _, namespace := range []string{"ws-1", "ws-2"} {
		got := idx.Search(SearchRequest{
			Query: "workspace", TopK: 5, Namespace: namespace, IncludeGrowing: true,
		})
		if len(got.Hits) != 1 || got.Hits[0].ObjectID != "obj" {
			t.Fatalf("namespace %q lost its object: %+v", namespace, got.Hits)
		}
	}
}

func TestNewGrowingShard(t *testing.T) {
	s := NewGrowingShard("shard_1", "ns1")
	if s.ID != "shard_1" {
		t.Errorf("ID: want shard_1, got %q", s.ID)
	}
	if s.State != ShardStateGrowing {
		t.Errorf("State: want ShardStateGrowing, got %v", s.State)
	}
}
