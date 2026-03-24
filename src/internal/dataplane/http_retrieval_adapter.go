package dataplane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPRetrievalAdapter implements DataPlane by delegating to the Python
// retrieval service over HTTP.  This bridges Go and the C++ retrieval engine
// without requiring CGO or Milvus dependencies.
//
// The Python service must be running at the configured endpoint with:
//
//	python -m src.internal.retrieval.main --serve --port 8081
type HTTPRetrievalAdapter struct {
	endpoint   string
	httpClient *http.Client
}

// NewHTTPRetrievalAdapter creates an adapter pointing to the Python retrieval
// service.  endpoint should be the base URL, e.g. "http://127.0.0.1:8081".
func NewHTTPRetrievalAdapter(endpoint string) *HTTPRetrievalAdapter {
	return &HTTPRetrievalAdapter{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// retrievalRequest is the JSON payload sent to the Python service.
type retrievalRequest struct {
	QueryText    string   `json:"query_text"`
	TopK         int      `json:"top_k"`
	Namespace    string   `json:"namespace,omitempty"`
	ObjectTypes  []string `json:"object_types,omitempty"`
	MemoryTypes  []string `json:"memory_types,omitempty"`
	EnableDense  bool     `json:"enable_dense"`
	EnableSparse bool     `json:"enable_sparse"`
	ForGraph     bool     `json:"for_graph"`
}

// retrievalResponse is the JSON payload returned by the Python service.
type retrievalResponse struct {
	Candidates []struct {
		ObjectID   string  `json:"object_id"`
		FinalScore float64 `json:"final_score"`
		IsSeed     bool    `json:"is_seed"`
	} `json:"candidates"`
	TotalFound int     `json:"total_found"`
	DenseHits  int     `json:"dense_hits"`
	SparseHits int     `json:"sparse_hits"`
	LatencyMs  float64 `json:"latency_ms"`
}

// ingestRequest is the JSON payload for ingesting records.
type ingestRequest struct {
	ObjectID   string            `json:"object_id"`
	Text       string            `json:"text"`
	Namespace  string            `json:"namespace"`
	Attributes map[string]string `json:"attributes,omitempty"`
	EventTS    int64             `json:"event_ts"`
}

// Ingest sends a record to the Python retrieval service for indexing.
func (a *HTTPRetrievalAdapter) Ingest(record IngestRecord) error {
	req := ingestRequest{
		ObjectID:   record.ObjectID,
		Text:       record.Text,
		Namespace:  record.Namespace,
		Attributes: record.Attributes,
		EventTS:    record.EventUnixTS,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal ingest request: %w", err)
	}

	resp, err := a.httpClient.Post(
		a.endpoint+"/ingest",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("http post ingest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ingest failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Search queries the Python retrieval service and returns matching object IDs.
func (a *HTTPRetrievalAdapter) Search(input SearchInput) SearchOutput {
	topK := input.TopK
	if topK <= 0 {
		topK = 10
	}

	req := retrievalRequest{
		QueryText:    input.QueryText,
		TopK:         topK,
		Namespace:    input.Namespace,
		ObjectTypes:  input.ObjectTypes,
		MemoryTypes:  input.MemoryTypes,
		EnableDense:  true,
		EnableSparse: true,
		ForGraph:     false,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return SearchOutput{Tier: "error:marshal"}
	}

	resp, err := a.httpClient.Post(
		a.endpoint+"/retrieve",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return SearchOutput{Tier: "error:http"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SearchOutput{Tier: fmt.Sprintf("error:status_%d", resp.StatusCode)}
	}

	var result retrievalResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return SearchOutput{Tier: "error:decode"}
	}

	objectIDs := make([]string, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		objectIDs = append(objectIDs, c.ObjectID)
	}

	tier := "hot"
	if result.DenseHits > 0 && result.SparseHits > 0 {
		tier = "hot+warm"
	}

	return SearchOutput{
		ObjectIDs:       objectIDs,
		ScannedSegments: []string{fmt.Sprintf("python_retrieval:latency_ms=%.1f", result.LatencyMs)},
		Tier:            tier,
	}
}

// Flush is a no-op for the HTTP adapter; the Python service manages its own state.
func (a *HTTPRetrievalAdapter) Flush() error {
	return nil
}

// Healthz checks if the Python retrieval service is healthy.
func (a *HTTPRetrievalAdapter) Healthz() error {
	resp, err := a.httpClient.Get(a.endpoint + "/healthz")
	if err != nil {
		return fmt.Errorf("healthz request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned status %d", resp.StatusCode)
	}

	return nil
}

// VectorSearcher interface implementation for HybridDataPlane integration.

// vectorSearchRequest is the JSON payload for vector search with embedding.
type vectorSearchRequest struct {
	Embedding []float32 `json:"embedding"`
	TopK      int       `json:"top_k"`
	Filter    string    `json:"filter,omitempty"`
}

// vectorIngestRequest is the JSON payload for ingesting with embedding.
type vectorIngestRequest struct {
	ObjectID   string            `json:"object_id"`
	Embedding  []float32         `json:"embedding"`
	Text       string            `json:"text"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// SearchWithEmbeddingVS implements VectorSearcher.SearchWithEmbeddingVS.
func (a *HTTPRetrievalAdapter) SearchWithEmbeddingVS(embedding []float32, topK int, filter string) ([]string, []float32, error) {
	if topK <= 0 {
		topK = 10
	}

	req := vectorSearchRequest{
		Embedding: embedding,
		TopK:      topK,
		Filter:    filter,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal vector search request: %w", err)
	}

	resp, err := a.httpClient.Post(
		a.endpoint+"/vector_search",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("http post vector_search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("vector_search failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result retrievalResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("decode vector_search response: %w", err)
	}

	objectIDs := make([]string, 0, len(result.Candidates))
	scores := make([]float32, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		objectIDs = append(objectIDs, c.ObjectID)
		scores = append(scores, float32(c.FinalScore))
	}

	return objectIDs, scores, nil
}

// IngestWithEmbeddingVS implements VectorSearcher.IngestWithEmbeddingVS.
func (a *HTTPRetrievalAdapter) IngestWithEmbeddingVS(objectID string, embedding []float32, text string, attributes map[string]string) error {
	req := vectorIngestRequest{
		ObjectID:   objectID,
		Embedding:  embedding,
		Text:       text,
		Attributes: attributes,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal vector ingest request: %w", err)
	}

	resp, err := a.httpClient.Post(
		a.endpoint+"/vector_ingest",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("http post vector_ingest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vector_ingest failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	return nil
}
