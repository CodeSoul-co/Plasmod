package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"andb/src/internal/schemas"
)

// S3ColdStore implements ColdObjectStore backed by a MinIO/S3-compatible bucket.
//
// Object layout inside the bucket (all under cfg.Prefix):
//
//	{prefix}/cold/memories/{memory_id}.json
//	{prefix}/cold/agents/{agent_id}.json
//
// Writes use PutBytes (no round-trip verify) for low-latency archival.
// Reads use GetBytes; a 404 is treated as "not found" (returns false).
// EnsureBucket is called at most once per store lifetime via ensureOnce.
type S3ColdStore struct {
	cfg        S3Config
	ensureOnce sync.Once
}

type s3ColdScored struct {
	id    string
	ts    int64
	score float64
}

type s3ColdSearchConfig struct {
	maxPages       int
	maxCandidates  int
	concurrency    int
	batchSize      int
	bufferFactor   int
	earlyStopScore float64
	noImprovePages int
}

// NewS3ColdStore returns an S3-backed ColdObjectStore using the supplied config.
func NewS3ColdStore(cfg S3Config) *S3ColdStore {
	return &S3ColdStore{cfg: cfg}
}

// doEnsureBucket creates the S3 bucket if it does not exist. It is called
// automatically before the first write and runs at most once per store lifetime.
func (s *S3ColdStore) doEnsureBucket() {
	s.ensureOnce.Do(func() {
		if err := EnsureBucket(context.Background(), nil, s.cfg); err != nil {
			log.Printf("s3cold: ensure bucket: %v", err)
		}
	})
}

func (s *S3ColdStore) memoryEmbeddingKey(id string) string {
	return fmt.Sprintf("%s/cold/embeddings/%s.npy", s.cfg.Prefix, id)
}

func (s *S3ColdStore) memoryKey(id string) string {
	return fmt.Sprintf("%s/cold/memories/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) agentKey(id string) string {
	return fmt.Sprintf("%s/cold/agents/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) stateKey(id string) string {
	return fmt.Sprintf("%s/cold/states/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) artifactKey(id string) string {
	return fmt.Sprintf("%s/cold/artifacts/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) edgeKey(id string) string {
	return fmt.Sprintf("%s/cold/edges/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) edgeBySrcKey(srcID, edgeID string) string {
	return fmt.Sprintf("%s/cold/edges_by_src/%s/%s.ref", s.cfg.Prefix, srcID, edgeID)
}

func (s *S3ColdStore) edgeByDstKey(dstID, edgeID string) string {
	return fmt.Sprintf("%s/cold/edges_by_dst/%s/%s.ref", s.cfg.Prefix, dstID, edgeID)
}

func (s *S3ColdStore) edgeBySrcPrefix(srcID string) string {
	return fmt.Sprintf("%s/cold/edges_by_src/%s/", s.cfg.Prefix, srcID)
}

func (s *S3ColdStore) edgeByDstPrefix(dstID string) string {
	return fmt.Sprintf("%s/cold/edges_by_dst/%s/", s.cfg.Prefix, dstID)
}

func (s *S3ColdStore) PutMemory(m schemas.Memory) {
	s.doEnsureBucket()
	data, err := json.Marshal(m)
	if err != nil {
		log.Printf("s3cold: marshal memory %s: %v", m.MemoryID, err)
		return
	}
	if err := PutBytes(context.Background(), nil, s.cfg, s.memoryKey(m.MemoryID), data, "application/json"); err != nil {
		log.Printf("s3cold: put memory %s: %v", m.MemoryID, err)
	}
}

func (s *S3ColdStore) GetMemory(id string) (schemas.Memory, bool) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.memoryKey(id))
	if err != nil {
		log.Printf("s3cold: get memory %s: %v", id, err)
		return schemas.Memory{}, false
	}
	if data == nil {
		log.Printf("s3cold: miss memory key=%s", s.memoryKey(id))
		return schemas.Memory{}, false
	}
	var m schemas.Memory
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("s3cold: unmarshal memory %s: %v", id, err)
		return schemas.Memory{}, false
	}
	return m, true
}

func (s *S3ColdStore) DeleteMemory(id string) error {
	return DeleteObject(context.Background(), nil, s.cfg, s.memoryKey(id))
}

func (s *S3ColdStore) PutMemoryEmbedding(memoryID string, vec []float32) error {
	s.doEnsureBucket()

	data, err := float32SliceToBytes(vec)
	if err != nil {
		return err
	}

	return PutBytes(
		context.Background(),
		nil,
		s.cfg,
		s.memoryEmbeddingKey(memoryID),
		data,
		"application/octet-stream",
	)
}

func (s *S3ColdStore) GetMemoryEmbedding(memoryID string) ([]float32, bool, error) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.memoryEmbeddingKey(memoryID))
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return nil, false, nil
	}

	vec, err := bytesToFloat32Slice(data)
	if err != nil {
		return nil, false, err
	}
	return vec, true, nil
}

func (s *S3ColdStore) PutAgent(a schemas.Agent) {
	s.doEnsureBucket()
	data, err := json.Marshal(a)
	if err != nil {
		log.Printf("s3cold: marshal agent %s: %v", a.AgentID, err)
		return
	}
	if err := PutBytes(context.Background(), nil, s.cfg, s.agentKey(a.AgentID), data, "application/json"); err != nil {
		log.Printf("s3cold: put agent %s: %v", a.AgentID, err)
	}
}

func (s *S3ColdStore) GetAgent(id string) (schemas.Agent, bool) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.agentKey(id))
	if err != nil {
		log.Printf("s3cold: get agent %s: %v", id, err)
		return schemas.Agent{}, false
	}
	if data == nil {
		log.Printf("s3cold: miss agent key=%s", s.agentKey(id))
		return schemas.Agent{}, false
	}
	var a schemas.Agent
	if err := json.Unmarshal(data, &a); err != nil {
		log.Printf("s3cold: unmarshal agent %s: %v", id, err)
		return schemas.Agent{}, false
	}
	return a, true
}

func (s *S3ColdStore) PutState(st schemas.State) {
	s.doEnsureBucket()
	data, err := json.Marshal(st)
	if err != nil {
		log.Printf("s3cold: marshal state %s: %v", st.StateID, err)
		return
	}
	if err := PutBytes(context.Background(), nil, s.cfg, s.stateKey(st.StateID), data, "application/json"); err != nil {
		log.Printf("s3cold: put state %s: %v", st.StateID, err)
	}
}

func (s *S3ColdStore) GetState(id string) (schemas.State, bool) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.stateKey(id))
	if err != nil {
		log.Printf("s3cold: get state %s: %v", id, err)
		return schemas.State{}, false
	}
	if data == nil {
		log.Printf("s3cold: miss state key=%s", s.stateKey(id))
		return schemas.State{}, false
	}
	var st schemas.State
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("s3cold: unmarshal state %s: %v", id, err)
		return schemas.State{}, false
	}
	return st, true
}

func (s *S3ColdStore) PutArtifact(art schemas.Artifact) {
	s.doEnsureBucket()
	data, err := json.Marshal(art)
	if err != nil {
		log.Printf("s3cold: marshal artifact %s: %v", art.ArtifactID, err)
		return
	}
	if err := PutBytes(context.Background(), nil, s.cfg, s.artifactKey(art.ArtifactID), data, "application/json"); err != nil {
		log.Printf("s3cold: put artifact %s: %v", art.ArtifactID, err)
	}
}

func (s *S3ColdStore) GetArtifact(id string) (schemas.Artifact, bool) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.artifactKey(id))
	if err != nil {
		log.Printf("s3cold: get artifact %s: %v", id, err)
		return schemas.Artifact{}, false
	}
	if data == nil {
		log.Printf("s3cold: miss artifact key=%s", s.artifactKey(id))
		return schemas.Artifact{}, false
	}
	var art schemas.Artifact
	if err := json.Unmarshal(data, &art); err != nil {
		log.Printf("s3cold: unmarshal artifact %s: %v", id, err)
		return schemas.Artifact{}, false
	}
	return art, true
}

func (s *S3ColdStore) PutEdge(e schemas.Edge) {
	s.doEnsureBucket()
	data, err := json.Marshal(e)
	if err != nil {
		log.Printf("s3cold: marshal edge %s: %v", e.EdgeID, err)
		return
	}
	if err := PutBytes(context.Background(), nil, s.cfg, s.edgeKey(e.EdgeID), data, "application/json"); err != nil {
		log.Printf("s3cold: put edge %s: %v", e.EdgeID, err)
	}

	// Secondary indices for incident-edge lookup by object ID.
	// These are small reference objects so HardDeleteMemory can delete cold edges
	// without scanning the entire cold edge set.
	ref := []byte("1")
	_ = PutBytes(context.Background(), nil, s.cfg, s.edgeBySrcKey(e.SrcObjectID, e.EdgeID), ref, "application/octet-stream")
	_ = PutBytes(context.Background(), nil, s.cfg, s.edgeByDstKey(e.DstObjectID, e.EdgeID), ref, "application/octet-stream")
}

func (s *S3ColdStore) GetEdge(id string) (schemas.Edge, bool) {
	data, err := GetBytes(context.Background(), nil, s.cfg, s.edgeKey(id))
	if err != nil {
		log.Printf("s3cold: get edge %s: %v", id, err)
		return schemas.Edge{}, false
	}
	if data == nil {
		log.Printf("s3cold: miss edge key=%s", s.edgeKey(id))
		return schemas.Edge{}, false
	}
	var e schemas.Edge
	if err := json.Unmarshal(data, &e); err != nil {
		log.Printf("s3cold: unmarshal edge %s: %v", id, err)
		return schemas.Edge{}, false
	}
	return e, true
}

func (s *S3ColdStore) DeleteEdge(id string) error {
	// Best-effort delete: remove main edge object plus its src/dst index refs.
	if e, ok := s.GetEdge(id); ok {
		_ = DeleteObject(context.Background(), nil, s.cfg, s.edgeBySrcKey(e.SrcObjectID, id))
		_ = DeleteObject(context.Background(), nil, s.cfg, s.edgeByDstKey(e.DstObjectID, id))
	}
	return DeleteObject(context.Background(), nil, s.cfg, s.edgeKey(id))
}

func (s *S3ColdStore) ListEdgeIDsByObjectID(objectID string) ([]string, error) {
	ctx := context.Background()
	out := make(map[string]bool)

	srcKeys, err := ListObjects(ctx, nil, s.cfg, s.edgeBySrcPrefix(objectID))
	if err != nil {
		return nil, err
	}
	for _, k := range srcKeys {
		if id, ok := parseEdgeRefKey(k); ok {
			out[id] = true
		}
	}

	dstKeys, err := ListObjects(ctx, nil, s.cfg, s.edgeByDstPrefix(objectID))
	if err != nil {
		return nil, err
	}
	for _, k := range dstKeys {
		if id, ok := parseEdgeRefKey(k); ok {
			out[id] = true
		}
	}

	ids := make([]string, 0, len(out))
	for id := range out {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// ListEdges is not supported for the S3 cold store — scanning all cold edge
// objects would require a list-objects API call with an unbounded result set.
// Callers should use GetEdge for point lookups.  Returns empty slice always.
func (s *S3ColdStore) ListEdges() []schemas.Edge {
	return []schemas.Edge{}
}

func parseEdgeRefKey(objectKey string) (string, bool) {
	// Expect: .../<edge_id>.ref
	objectKey = strings.TrimRight(objectKey, "/")
	i := strings.LastIndex(objectKey, "/")
	if i < 0 || i+1 >= len(objectKey) {
		return "", false
	}
	base := objectKey[i+1:]
	if !strings.HasSuffix(base, ".ref") {
		return "", false
	}
	return strings.TrimSuffix(base, ".ref"), true
}

// ColdSearch searches cold-tier memories stored in S3 using prefix-based listing.
// Since S3 does not support arbitrary query predicates, it lists all cold memory
// keys under the prefix, fetches each JSON object, and scores them lexically.
// For production with large cold archives, this should be replaced with a
// metadata index (e.g. DynamoDB or SQLite) keyed by text tokens.
func (s *S3ColdStore) ColdSearch(query string, topK int) []string {
	if topK <= 0 {
		return nil
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("%s/cold/memories/", s.cfg.Prefix)
	cfg := loadS3ColdSearchConfigFromEnv()

	keys, pagesScanned, listTruncated, err := ListObjectsLimited(ctx, nil, s.cfg, prefix, cfg.maxPages, cfg.maxCandidates)
	if err != nil || len(keys) == 0 {
		return nil
	}

	targetCandidates := topK * cfg.bufferFactor
	if targetCandidates < topK {
		targetCandidates = topK
	}
	maxCandidates := targetCandidates * 2
	if maxCandidates < topK {
		maxCandidates = topK
	}

	results := make([]s3ColdScored, 0, min(maxCandidates, len(keys)))
	noImprovePages := 0
	prevCutoff := -1.0

	for start := 0; start < len(keys); start += cfg.batchSize {
		end := start + cfg.batchSize
		if end > len(keys) {
			end = len(keys)
		}
		page := keys[start:end]

		pageResults := make([]s3ColdScored, 0, len(page))
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, cfg.concurrency)

		for _, key := range page {
			wg.Add(1)
			sem <- struct{}{}
			go func(objectKey string) {
				defer wg.Done()
				defer func() { <-sem }()

				data, err := GetBytes(ctx, nil, s.cfg, objectKey)
				if err != nil || data == nil {
					return
				}
				var m schemas.Memory
				if err := json.Unmarshal(data, &m); err != nil {
					return
				}
				score := scoreColdMemory(query, m)
				if score <= 0 {
					return
				}
				mu.Lock()
				pageResults = append(pageResults, s3ColdScored{
					id:    m.MemoryID,
					ts:    m.Version,
					score: score,
				})
				mu.Unlock()
			}(key)
		}
		wg.Wait()

		if len(pageResults) > 0 {
			results = append(results, pageResults...)
			results = selectTopScored(results, maxCandidates)
		}

		topNow := selectTopScored(results, topK)
		currCutoff := cutoffScore(topNow, topK)
		if currCutoff > prevCutoff {
			noImprovePages = 0
			prevCutoff = currCutoff
		} else {
			noImprovePages++
		}

		if shouldEarlyStop(topNow, len(results), topK, targetCandidates, cfg.earlyStopScore, noImprovePages, cfg.noImprovePages) {
			break
		}
	}

	results = selectTopScored(results, topK)
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.id)
	}
	log.Printf(
		"s3cold: cold lexical search query=%q top_k=%d pages=%d listed=%d list_truncated=%t returned=%d cfg{max_pages=%d,max_candidates=%d,batch=%d,concurrency=%d}",
		query, topK, pagesScanned, len(keys), listTruncated, len(out), cfg.maxPages, cfg.maxCandidates, cfg.batchSize, cfg.concurrency,
	)
	return out
}

func (s *S3ColdStore) DeleteMemoryEmbedding(memoryID string) error {
	return DeleteObject(context.Background(), nil, s.cfg, s.memoryEmbeddingKey(memoryID))
}

func float32SliceToBytes(vec []float32) ([]byte, error) {
	if len(vec) == 0 {
		return []byte{}, nil
	}
	var buf bytes.Buffer
	for _, v := range vec {
		if err := binary.Write(&buf, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func bytesToFloat32Slice(data []byte) ([]float32, error) {
	if len(data) == 0 {
		return []float32{}, nil
	}
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("invalid float32 byte length: %d", len(data))
	}

	vec := make([]float32, len(data)/4)
	reader := bytes.NewReader(data)
	for i := range vec {
		if err := binary.Read(reader, binary.LittleEndian, &vec[i]); err != nil {
			return nil, err
		}
	}
	return vec, nil
}

func memoryIDFromEmbeddingKey(key string) string {
	key = strings.TrimSuffix(key, ".npy")
	parts := strings.Split(key, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func loadS3ColdSearchConfigFromEnv() s3ColdSearchConfig {
	algoCfg := schemas.DefaultAlgorithmConfig()

	cfg := s3ColdSearchConfig{
		maxPages:       20,
		maxCandidates:  algoCfg.ColdMaxCandidates,
		concurrency:    8,
		batchSize:      algoCfg.ColdBatchSize,
		bufferFactor:   3,
		earlyStopScore: 0.95,
		noImprovePages: 2,
	}
	if cfg.maxCandidates <= 0 {
		cfg.maxCandidates = 1000
	}
	if cfg.batchSize <= 0 {
		cfg.batchSize = 128
	}

	if v := parseEnvIntWithDefault("S3_COLD_MAX_PAGES", cfg.maxPages); v > 0 {
		cfg.maxPages = v
	}
	if v := parseEnvIntWithDefault("S3_COLD_MAX_CANDIDATES", cfg.maxCandidates); v > 0 {
		cfg.maxCandidates = v
	}
	// Backward compatibility: old env name still works if new one is not set.
	if strings.TrimSpace(os.Getenv("S3_COLD_MAX_CANDIDATES")) == "" {
		if v := parseEnvIntWithDefault("S3_COLDSEARCH_MAX_KEYS", cfg.maxCandidates); v > 0 {
			cfg.maxCandidates = v
		}
	}
	if v := parseEnvIntWithDefault("S3_COLDSEARCH_CONCURRENCY", cfg.concurrency); v > 0 {
		cfg.concurrency = v
	}
	if v := parseEnvIntWithDefault("S3_COLDSEARCH_BATCH_SIZE", cfg.batchSize); v > 0 {
		cfg.batchSize = v
	}
	if v := parseEnvIntWithDefault("S3_COLDSEARCH_BUFFER_FACTOR", cfg.bufferFactor); v > 0 {
		cfg.bufferFactor = v
	}
	if v := parseEnvFloatWithDefault("S3_COLDSEARCH_EARLY_STOP_SCORE", cfg.earlyStopScore); v > 0 && v <= 1 {
		cfg.earlyStopScore = v
	}
	if v := parseEnvIntWithDefault("S3_COLDSEARCH_NO_IMPROVE_PAGES", cfg.noImprovePages); v > 0 {
		cfg.noImprovePages = v
	}
	return cfg
}

func parseEnvIntWithDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func parseEnvFloatWithDefault(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return n
}

func scoreColdMemory(query string, m schemas.Memory) float64 {
	lq := strings.ToLower(query)
	if strings.TrimSpace(lq) == "" {
		return 0
	}
	text := strings.ToLower(m.Content)
	summary := strings.ToLower(m.Summary)
	if strings.Contains(text, lq) || strings.Contains(summary, lq) {
		return 1.0
	}
	qTokens := strings.Fields(lq)
	textTokens := strings.Fields(text)
	match := 0
	for _, qt := range qTokens {
		for _, tt := range textTokens {
			if tt == qt {
				match++
				break
			}
		}
	}
	if len(qTokens) == 0 {
		return 0
	}
	return float64(match) / float64(len(qTokens))
}

func selectTopScored(results []s3ColdScored, n int) []s3ColdScored {
	if len(results) == 0 || n <= 0 {
		return nil
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].ts > results[j].ts
	})
	if len(results) <= n {
		return results
	}
	return results[:n]
}

func cutoffScore(results []s3ColdScored, topK int) float64 {
	if topK <= 0 || len(results) < topK {
		return -1
	}
	return results[topK-1].score
}

func shouldEarlyStop(
	topResults []s3ColdScored,
	totalCandidates int,
	topK int,
	targetCandidates int,
	highScoreThreshold float64,
	noImprovePages int,
	noImprovePagesThreshold int,
) bool {
	if topK <= 0 || len(topResults) < topK || totalCandidates < targetCandidates {
		return false
	}
	high := 0
	for _, r := range topResults {
		if r.score >= highScoreThreshold {
			high++
		}
	}
	return high >= topK && noImprovePages >= noImprovePagesThreshold
}

func (s *S3ColdStore) ColdVectorSearch(queryVec []float32, topK int) []string {
	if topK <= 0 || len(queryVec) == 0 {
		return nil
	}

	ctx := context.Background()
	prefix := fmt.Sprintf("%s/cold/embeddings/", s.cfg.Prefix)
	cfg := loadS3ColdSearchConfigFromEnv()

	keys, pagesScanned, listTruncated, err := ListObjectsLimited(ctx, nil, s.cfg, prefix, cfg.maxPages, cfg.maxCandidates)
	if err != nil || len(keys) == 0 {
		return nil
	}

	type scored struct {
		id    string
		score float64
		ts    int64
	}

	maxCandidates := cfg.maxCandidates
	if maxCandidates <= 0 {
		maxCandidates = topK * 4
	}
	if maxCandidates < topK {
		maxCandidates = topK
	}

	results := make([]scored, 0, min(maxCandidates, len(keys)))

	for start := 0; start < len(keys); start += cfg.batchSize {
		end := start + cfg.batchSize
		if end > len(keys) {
			end = len(keys)
		}
		page := keys[start:end]

		pageResults := make([]scored, 0, len(page))
		for _, key := range page {
			data, err := GetBytes(ctx, nil, s.cfg, key)
			if err != nil || data == nil {
				continue
			}

			vec, err := bytesToFloat32Slice(data)
			if err != nil {
				continue
			}

			score := dotProduct(queryVec, vec)
			if score <= 0 {
				continue
			}

			memoryID := memoryIDFromEmbeddingKey(key)
			if memoryID == "" {
				continue
			}

			pageResults = append(pageResults, scored{
				id:    memoryID,
				score: score,
				ts:    0,
			})
		}

		if len(pageResults) > 0 {
			results = append(results, pageResults...)

			sort.Slice(results, func(i, j int) bool {
				if results[i].score != results[j].score {
					return results[i].score > results[j].score
				}
				return results[i].ts > results[j].ts
			})

			if len(results) > maxCandidates {
				results = results[:maxCandidates]
			}
		}
	}

	for i := range results {
		if m, ok := s.GetMemory(results[i].id); ok {
			results[i].ts = m.Version
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].ts > results[j].ts
	})

	out := make([]string, 0, min(topK, len(results)))
	for i := range results {
		if i >= topK {
			break
		}
		out = append(out, results[i].id)
	}
	log.Printf(
		"s3cold: cold vector search top_k=%d pages=%d listed=%d list_truncated=%t returned=%d cfg{max_pages=%d,max_candidates=%d,batch=%d}",
		topK, pagesScanned, len(keys), listTruncated, len(out), cfg.maxPages, cfg.maxCandidates, cfg.batchSize,
	)
	return out
}

func (s *S3ColdStore) ColdHNSWSearch(queryVec []float32, topK int) []string {
	// HNSW index loading from S3 is not implemented yet.
	// Return nil so callers fall back to brute-force ColdVectorSearch.
	return nil
}
