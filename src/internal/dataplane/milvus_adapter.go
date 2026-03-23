package dataplane

import (
	"context"
	"fmt"
	"log"
	"time"

	"andb/src/internal/dataplane/milvus/client"
	"andb/src/internal/dataplane/milvus/entity"
)

// MilvusAdapter implements DataPlane by connecting to a standalone Milvus instance.
// This is the production-grade integration that uses real Knowhere indexes via Milvus.
//
// Prerequisites:
//   - Milvus server running (docker-compose or standalone)
//   - Collection created with appropriate schema
//
// Usage:
//
//	adapter, err := NewMilvusAdapter("localhost:19530", "cogdb_vectors")
//	if err != nil { ... }
//	defer adapter.Close()
type MilvusAdapter struct {
	client         client.Client
	collectionName string
	dim            int
	ctx            context.Context
}

// MilvusConfig holds configuration for connecting to Milvus.
type MilvusConfig struct {
	Address        string // e.g., "localhost:19530"
	CollectionName string // e.g., "cogdb_vectors"
	Dim            int    // vector dimension, e.g., 128
	Username       string // optional
	Password       string // optional
}

// NewMilvusAdapter creates a new adapter connected to Milvus.
func NewMilvusAdapter(cfg MilvusConfig) (*MilvusAdapter, error) {
	ctx := context.Background()

	// Connect to Milvus
	var c client.Client
	var err error

	if cfg.Username != "" && cfg.Password != "" {
		c, err = client.NewClient(ctx, client.Config{
			Address:  cfg.Address,
			Username: cfg.Username,
			Password: cfg.Password,
		})
	} else {
		c, err = client.NewClient(ctx, client.Config{
			Address: cfg.Address,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to Milvus at %s: %w", cfg.Address, err)
	}

	adapter := &MilvusAdapter{
		client:         c,
		collectionName: cfg.CollectionName,
		dim:            cfg.Dim,
		ctx:            ctx,
	}

	// Ensure collection exists
	if err := adapter.ensureCollection(); err != nil {
		c.Close()
		return nil, err
	}

	return adapter, nil
}

// ensureCollection creates the collection if it doesn't exist.
func (a *MilvusAdapter) ensureCollection() error {
	exists, err := a.client.HasCollection(a.ctx, a.collectionName)
	if err != nil {
		return fmt.Errorf("failed to check collection: %w", err)
	}

	if exists {
		// Load collection for search
		err = a.client.LoadCollection(a.ctx, a.collectionName, false)
		if err != nil {
			log.Printf("warning: failed to load collection: %v", err)
		}
		return nil
	}

	// Create collection schema
	schema := &entity.Schema{
		CollectionName: a.collectionName,
		Description:    "CogDB vector store for retrieval",
		Fields: []*entity.Field{
			{
				Name:       "object_id",
				DataType:   entity.FieldTypeVarChar,
				PrimaryKey: true,
				AutoID:     false,
				TypeParams: map[string]string{
					"max_length": "256",
				},
			},
			{
				Name:     "text",
				DataType: entity.FieldTypeVarChar,
				TypeParams: map[string]string{
					"max_length": "65535",
				},
			},
			{
				Name:     "namespace",
				DataType: entity.FieldTypeVarChar,
				TypeParams: map[string]string{
					"max_length": "256",
				},
			},
			{
				Name:     "event_ts",
				DataType: entity.FieldTypeInt64,
			},
			{
				Name:     "embedding",
				DataType: entity.FieldTypeFloatVector,
				TypeParams: map[string]string{
					"dim": fmt.Sprintf("%d", a.dim),
				},
			},
		},
	}

	err = a.client.CreateCollection(a.ctx, schema, entity.DefaultShardNumber)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Create HNSW index on embedding field
	idx, err := entity.NewIndexHNSW(entity.IP, 16, 256)
	if err != nil {
		return fmt.Errorf("failed to create HNSW index params: %w", err)
	}

	err = a.client.CreateIndex(a.ctx, a.collectionName, "embedding", idx, false)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	// Load collection for search
	err = a.client.LoadCollection(a.ctx, a.collectionName, false)
	if err != nil {
		return fmt.Errorf("failed to load collection: %w", err)
	}

	log.Printf("created Milvus collection %s with HNSW index (dim=%d)", a.collectionName, a.dim)
	return nil
}

// Close releases the Milvus client connection.
func (a *MilvusAdapter) Close() error {
	return a.client.Close()
}

// Ingest inserts a record into Milvus.
// Note: This requires the record to have a pre-computed embedding vector.
// In production, use an embedding service to convert text to vectors.
func (a *MilvusAdapter) Ingest(record IngestRecord) error {
	// For now, we need an embedding. In production, call embedding service here.
	// This is a placeholder that generates a random embedding for testing.
	embedding := make([]float32, a.dim)
	for i := range embedding {
		embedding[i] = float32(i) * 0.01 // placeholder
	}

	objectIDs := []string{record.ObjectID}
	texts := []string{record.Text}
	namespaces := []string{record.Namespace}
	eventTSs := []int64{record.EventUnixTS}
	embeddings := [][]float32{embedding}

	_, err := a.client.Insert(a.ctx, a.collectionName, "",
		entity.NewColumnVarChar("object_id", objectIDs),
		entity.NewColumnVarChar("text", texts),
		entity.NewColumnVarChar("namespace", namespaces),
		entity.NewColumnInt64("event_ts", eventTSs),
		entity.NewColumnFloatVector("embedding", a.dim, embeddings),
	)

	if err != nil {
		return fmt.Errorf("failed to insert into Milvus: %w", err)
	}

	return nil
}

// IngestWithEmbedding inserts a record with a pre-computed embedding vector.
func (a *MilvusAdapter) IngestWithEmbedding(record IngestRecord, embedding []float32) error {
	if len(embedding) != a.dim {
		return fmt.Errorf("embedding dimension mismatch: got %d, expected %d", len(embedding), a.dim)
	}

	objectIDs := []string{record.ObjectID}
	texts := []string{record.Text}
	namespaces := []string{record.Namespace}
	eventTSs := []int64{record.EventUnixTS}
	embeddings := [][]float32{embedding}

	_, err := a.client.Insert(a.ctx, a.collectionName, "",
		entity.NewColumnVarChar("object_id", objectIDs),
		entity.NewColumnVarChar("text", texts),
		entity.NewColumnVarChar("namespace", namespaces),
		entity.NewColumnInt64("event_ts", eventTSs),
		entity.NewColumnFloatVector("embedding", a.dim, embeddings),
	)

	if err != nil {
		return fmt.Errorf("failed to insert into Milvus: %w", err)
	}

	return nil
}

// Search queries Milvus for similar vectors.
func (a *MilvusAdapter) Search(input SearchInput) SearchOutput {
	start := time.Now()

	// For now, generate a random query vector. In production, embed the query text.
	queryVector := make([]float32, a.dim)
	for i := range queryVector {
		queryVector[i] = float32(i) * 0.01 // placeholder
	}

	topK := input.TopK
	if topK <= 0 {
		topK = 10
	}

	// Build search parameters
	sp, err := entity.NewIndexHNSWSearchParam(64) // ef parameter
	if err != nil {
		return SearchOutput{Tier: "error:search_param"}
	}

	// Build filter expression
	expr := ""
	if input.Namespace != "" {
		expr = fmt.Sprintf("namespace == \"%s\"", input.Namespace)
	}
	if input.TimeFromUnixTS > 0 {
		if expr != "" {
			expr += " && "
		}
		expr += fmt.Sprintf("event_ts >= %d", input.TimeFromUnixTS)
	}
	if input.TimeToUnixTS > 0 {
		if expr != "" {
			expr += " && "
		}
		expr += fmt.Sprintf("event_ts <= %d", input.TimeToUnixTS)
	}

	// Execute search
	vectors := []entity.Vector{entity.FloatVector(queryVector)}
	results, err := a.client.Search(
		a.ctx,
		a.collectionName,
		nil, // partitions
		expr,
		[]string{"object_id", "text", "namespace"},
		vectors,
		"embedding",
		entity.IP,
		topK,
		sp,
	)

	if err != nil {
		log.Printf("Milvus search error: %v", err)
		return SearchOutput{Tier: "error:search"}
	}

	// Extract object IDs from results
	objectIDs := make([]string, 0)
	for _, result := range results {
		for i := 0; i < result.ResultCount; i++ {
			if idCol, ok := result.Fields.GetColumn("object_id").(*entity.ColumnVarChar); ok {
				id, err := idCol.ValueByIdx(i)
				if err == nil {
					objectIDs = append(objectIDs, id)
				}
			}
		}
	}

	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	return SearchOutput{
		ObjectIDs:       objectIDs,
		ScannedSegments: []string{fmt.Sprintf("milvus:latency_ms=%.1f", latencyMs)},
		Tier:            "milvus",
	}
}

// SearchWithEmbedding queries Milvus with a pre-computed query embedding.
func (a *MilvusAdapter) SearchWithEmbedding(input SearchInput, queryEmbedding []float32) SearchOutput {
	start := time.Now()

	if len(queryEmbedding) != a.dim {
		return SearchOutput{Tier: "error:dim_mismatch"}
	}

	topK := input.TopK
	if topK <= 0 {
		topK = 10
	}

	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return SearchOutput{Tier: "error:search_param"}
	}

	expr := ""
	if input.Namespace != "" {
		expr = fmt.Sprintf("namespace == \"%s\"", input.Namespace)
	}
	if input.TimeFromUnixTS > 0 {
		if expr != "" {
			expr += " && "
		}
		expr += fmt.Sprintf("event_ts >= %d", input.TimeFromUnixTS)
	}
	if input.TimeToUnixTS > 0 {
		if expr != "" {
			expr += " && "
		}
		expr += fmt.Sprintf("event_ts <= %d", input.TimeToUnixTS)
	}

	vectors := []entity.Vector{entity.FloatVector(queryEmbedding)}
	results, err := a.client.Search(
		a.ctx,
		a.collectionName,
		nil,
		expr,
		[]string{"object_id", "text", "namespace"},
		vectors,
		"embedding",
		entity.IP,
		topK,
		sp,
	)

	if err != nil {
		log.Printf("Milvus search error: %v", err)
		return SearchOutput{Tier: "error:search"}
	}

	objectIDs := make([]string, 0)
	for _, result := range results {
		for i := 0; i < result.ResultCount; i++ {
			if idCol, ok := result.Fields.GetColumn("object_id").(*entity.ColumnVarChar); ok {
				id, err := idCol.ValueByIdx(i)
				if err == nil {
					objectIDs = append(objectIDs, id)
				}
			}
		}
	}

	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0

	return SearchOutput{
		ObjectIDs:       objectIDs,
		ScannedSegments: []string{fmt.Sprintf("milvus:latency_ms=%.1f", latencyMs)},
		Tier:            "milvus",
	}
}

// Flush forces Milvus to persist data.
func (a *MilvusAdapter) Flush() error {
	return a.client.Flush(a.ctx, a.collectionName, false)
}

// DropCollection removes the collection (use with caution).
func (a *MilvusAdapter) DropCollection() error {
	return a.client.DropCollection(a.ctx, a.collectionName)
}

// CollectionStats returns statistics about the collection.
func (a *MilvusAdapter) CollectionStats() (map[string]string, error) {
	stats, err := a.client.GetCollectionStatistics(a.ctx, a.collectionName)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
