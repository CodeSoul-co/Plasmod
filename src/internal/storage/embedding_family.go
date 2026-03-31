package storage

import (
	"os"
	"strings"
)

// ResolveEmbeddingFamily normalizes the embedding family identifier used in
// segment metadata. Priority:
// 1) explicit attr["embedding_family"] from ingest record
// 2) ANDB_EMBEDDING_FAMILY env override
// 3) ANDB_EMBEDDER + ANDB_EMBEDDER_MODEL composed value
// 4) fallback to "tfidf"
func ResolveEmbeddingFamily(attrs map[string]string) string {
	if attrs != nil {
		if v := strings.TrimSpace(attrs["embedding_family"]); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(os.Getenv("ANDB_EMBEDDING_FAMILY")); v != "" {
		return v
	}
	provider := strings.TrimSpace(os.Getenv("ANDB_EMBEDDER"))
	if provider == "" {
		provider = "tfidf"
	}
	model := strings.TrimSpace(os.Getenv("ANDB_EMBEDDER_MODEL"))
	if model == "" || provider == "tfidf" {
		return provider
	}
	return provider + ":" + model
}
