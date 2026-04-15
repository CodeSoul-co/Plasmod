package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"plasmod/retrievalplane"
	"plasmod/src/internal/schemas"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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
	cfg          S3Config
	algoCfg      schemas.AlgorithmConfig
	ensureOnce   sync.Once
	listCacheMu  sync.RWMutex
	listCache    map[string]s3ListCacheEntry
	listCacheTTL int64
}

type s3ListCacheEntry struct {
	keys      []string
	expiresAt int64
}

type s3ColdScored struct {
	id    string
	ts    int64
	score float64
}

type s3ColdCandidate struct {
	id           string
	ts           int64
	lexicalScore float64
	recencyScore float64
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
	return NewS3ColdStoreWithAlgorithmConfig(cfg, schemas.DefaultAlgorithmConfig())
}

func NewS3ColdStoreWithAlgorithmConfig(cfg S3Config, algoCfg schemas.AlgorithmConfig) *S3ColdStore {
	return &S3ColdStore{
		cfg:          cfg,
		algoCfg:      algoCfg,
		listCache:    map[string]s3ListCacheEntry{},
		listCacheTTL: 5,
	}
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

func (s *S3ColdStore) embeddingPrefix() string {
	return fmt.Sprintf("%s/cold/embeddings/", s.cfg.Prefix)
}

func (s *S3ColdStore) memoryKey(id string) string {
	return fmt.Sprintf("%s/cold/memories/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) memoryPrefix() string {
	return fmt.Sprintf("%s/cold/memories/", s.cfg.Prefix)
}

func (s *S3ColdStore) agentPrefix() string {
	return fmt.Sprintf("%s/cold/agents/", s.cfg.Prefix)
}

func (s *S3ColdStore) agentKey(id string) string {
	return fmt.Sprintf("%s/cold/agents/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) statePrefix() string {
	return fmt.Sprintf("%s/cold/states/", s.cfg.Prefix)
}

func (s *S3ColdStore) stateKey(id string) string {
	return fmt.Sprintf("%s/cold/states/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) artifactPrefix() string {
	return fmt.Sprintf("%s/cold/artifacts/", s.cfg.Prefix)
}

func (s *S3ColdStore) artifactKey(id string) string {
	return fmt.Sprintf("%s/cold/artifacts/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) edgePrefix() string {
	return fmt.Sprintf("%s/cold/edges/", s.cfg.Prefix)
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

func getS3ObjectJSON[T any](cfg S3Config, objectKey, kind, id string) (T, bool) {
	data, err := GetBytes(context.Background(), nil, cfg, objectKey)
	if err != nil {
		log.Printf("s3cold: get %s %s: %v", kind, id, err)
		var zero T
		return zero, false
	}
	if data == nil {
		var zero T
		return zero, false
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		log.Printf("s3cold: unmarshal %s %s: %v", kind, id, err)
		var zero T
		return zero, false
	}
	return out, true

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
		return
	}
	s.invalidateListCache(s.memoryPrefix())
}

func (s *S3ColdStore) GetMemory(id string) (schemas.Memory, bool) {
	return getS3ObjectJSON[schemas.Memory](s.cfg, s.memoryKey(id), "memory", id)
}

func (s *S3ColdStore) DeleteMemory(id string) error {
	err := DeleteObject(context.Background(), nil, s.cfg, s.memoryKey(id))
	if err == nil {
		s.invalidateListCache(s.memoryPrefix())
	}
	return err
}

func (s *S3ColdStore) PutMemoryEmbedding(memoryID string, vec []float32) error {
	s.doEnsureBucket()

	data, err := float32SliceToBytes(vec)
	if err != nil {
		return err
	}

	if err := PutBytes(
		context.Background(),
		nil,
		s.cfg,
		s.memoryEmbeddingKey(memoryID),
		data,
		"application/octet-stream",
	); err != nil {
		return err
	}
	s.invalidateListCache(s.embeddingPrefix())
	return nil
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
		return
	}
	s.invalidateListCache(s.agentPrefix())
}

func (s *S3ColdStore) GetAgent(id string) (schemas.Agent, bool) {
	return getS3ObjectJSON[schemas.Agent](s.cfg, s.agentKey(id), "agent", id)
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
		return
	}
	s.invalidateListCache(s.statePrefix())
}

func (s *S3ColdStore) GetState(id string) (schemas.State, bool) {
	return getS3ObjectJSON[schemas.State](s.cfg, s.stateKey(id), "state", id)
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
		return
	}
	s.invalidateListCache(s.artifactPrefix())
}

func (s *S3ColdStore) GetArtifact(id string) (schemas.Artifact, bool) {
	return getS3ObjectJSON[schemas.Artifact](s.cfg, s.artifactKey(id), "artifact", id)
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
		return
	}

	// Secondary indices for incident-edge lookup by object ID.
	// These are small reference objects so HardDeleteMemory can delete cold edges
	// without scanning the entire cold edge set.
	ref := []byte("1")
	_ = PutBytes(context.Background(), nil, s.cfg, s.edgeBySrcKey(e.SrcObjectID, e.EdgeID), ref, "application/octet-stream")
	_ = PutBytes(context.Background(), nil, s.cfg, s.edgeByDstKey(e.DstObjectID, e.EdgeID), ref, "application/octet-stream")
	s.invalidateListCache(s.edgePrefix())

}

func (s *S3ColdStore) GetEdge(id string) (schemas.Edge, bool) {
	return getS3ObjectJSON[schemas.Edge](s.cfg, s.edgeKey(id), "edge", id)
}

func (s *S3ColdStore) DeleteEdge(id string) error {
	err := DeleteObject(context.Background(), nil, s.cfg, s.edgeKey(id))
	if err == nil {
		s.invalidateListCache(s.edgePrefix())
	}
	return err

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
	prefix := s.memoryPrefix()
	cfg := s.loadS3ColdSearchConfigFromEnv()

	keys, err := s.cachedListObjects(ctx, prefix)

	if err != nil || len(keys) == 0 {
		return nil
	}
	keys, pagesScanned, listTruncated := clampColdKeys(keys, cfg)

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
	err := DeleteObject(context.Background(), nil, s.cfg, s.memoryEmbeddingKey(memoryID))
	if err == nil {
		s.invalidateListCache(s.embeddingPrefix())
	}
	return err
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

func (s *S3ColdStore) invalidateListCache(prefix string) {
	s.listCacheMu.Lock()
	defer s.listCacheMu.Unlock()
	delete(s.listCache, prefix)
}

func (s *S3ColdStore) cachedListObjects(ctx context.Context, prefix string) ([]string, error) {
	now := time.Now().Unix()

	s.listCacheMu.RLock()
	if entry, ok := s.listCache[prefix]; ok && entry.expiresAt > now {
		keys := make([]string, len(entry.keys))
		copy(keys, entry.keys)
		s.listCacheMu.RUnlock()
		return keys, nil
	}
	s.listCacheMu.RUnlock()

	keys, err := ListObjects(ctx, nil, s.cfg, prefix)
	if err != nil {
		return nil, err
	}

	copyKeys := make([]string, len(keys))
	copy(copyKeys, keys)

	s.listCacheMu.Lock()
	s.listCache[prefix] = s3ListCacheEntry{
		keys:      copyKeys,
		expiresAt: now + s.listCacheTTL,
	}
	s.listCacheMu.Unlock()
	return keys, nil
}

func (s *S3ColdStore) loadS3ColdSearchConfigFromEnv() s3ColdSearchConfig {
	algoCfg := s.algoCfg
	if algoCfg == (schemas.AlgorithmConfig{}) {
		algoCfg = schemas.DefaultAlgorithmConfig()
	}

	cfg := s3ColdSearchConfig{
		maxPages:       20,
		maxCandidates:  algoCfg.ColdMaxCandidates,
		concurrency:    8,
		batchSize:      algoCfg.ColdBatchSize,
		bufferFactor:   algoCfg.ColdBufferFactor,
		earlyStopScore: algoCfg.ColdEarlyStopScore,
		noImprovePages: algoCfg.ColdNoImprovePages,
	}
	if cfg.maxCandidates <= 0 {
		cfg.maxCandidates = 1000
	}
	if cfg.batchSize <= 0 {
		cfg.batchSize = 128
	}
	if cfg.bufferFactor <= 0 {
		cfg.bufferFactor = 3
	}
	if cfg.earlyStopScore <= 0 || cfg.earlyStopScore > 1 {
		cfg.earlyStopScore = 0.95
	}
	if cfg.noImprovePages <= 0 {
		cfg.noImprovePages = 2
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
	if v := parseFirstEnvIntWithDefault([]string{"S3_COLD_CONCURRENCY", "S3_COLDSEARCH_CONCURRENCY"}, cfg.concurrency); v > 0 {
		cfg.concurrency = v
	}
	if v := parseFirstEnvIntWithDefault([]string{"S3_COLD_BATCH_SIZE", "S3_COLDSEARCH_BATCH_SIZE"}, cfg.batchSize); v > 0 {
		cfg.batchSize = v
	}
	if v := parseFirstEnvIntWithDefault([]string{"S3_COLD_BUFFER_FACTOR", "S3_COLDSEARCH_BUFFER_FACTOR"}, cfg.bufferFactor); v > 0 {
		cfg.bufferFactor = v
	}
	if v := parseFirstEnvFloatWithDefault([]string{"S3_COLD_EARLY_STOP_SCORE", "S3_COLDSEARCH_EARLY_STOP_SCORE"}, cfg.earlyStopScore); v > 0 && v <= 1 {
		cfg.earlyStopScore = v
	}
	if v := parseFirstEnvIntWithDefault([]string{"S3_COLD_NO_IMPROVE_PAGES", "S3_COLDSEARCH_NO_IMPROVE_PAGES"}, cfg.noImprovePages); v > 0 {
		cfg.noImprovePages = v
	}
	return cfg
}

func parseFirstEnvIntWithDefault(keys []string, fallback int) int {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err == nil {
			return n
		}
	}
	return fallback
}

func parseFirstEnvFloatWithDefault(keys []string, fallback float64) float64 {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err == nil {
			return n
		}
	}
	return fallback
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

func recencyScore(version int64, latest int64) float64 {
	if latest <= 0 || version <= 0 {
		return 0
	}
	if version >= latest {
		return 1
	}
	return float64(version) / float64(latest)
}

func combineColdScores(dense, lexical, recency float64, weights schemas.ColdSearchWeights) float64 {
	return weights.Dense*dense + weights.Lexical*lexical + weights.Recency*recency
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
	sorted := append([]s3ColdScored(nil), results...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].score != sorted[j].score {
			return sorted[i].score > sorted[j].score
		}
		return sorted[i].ts > sorted[j].ts
	})
	if len(sorted) <= n {
		return sorted
	}
	return sorted[:n]
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

func selectTopCandidates(results []s3ColdCandidate, n int) []s3ColdCandidate {
	if len(results) == 0 || n <= 0 {
		return nil
	}
	sort.Slice(results, func(i, j int) bool {
		left := results[i].lexicalScore + results[i].recencyScore*0.1
		right := results[j].lexicalScore + results[j].recencyScore*0.1
		if left != right {
			return left > right
		}
		return results[i].ts > results[j].ts
	})
	if len(results) <= n {
		return results
	}
	return results[:n]
}

func cutoffCandidateScore(results []s3ColdCandidate, topK int) float64 {
	if topK <= 0 || len(results) < topK {
		return -1
	}
	return results[topK-1].lexicalScore + results[topK-1].recencyScore*0.1
}

func shouldEarlyStopCandidates(
	topResults []s3ColdCandidate,
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
		if r.lexicalScore >= highScoreThreshold {
			high++
		}
	}
	return high >= topK && noImprovePages >= noImprovePagesThreshold
}

func (s *S3ColdStore) getMemoryEmbeddingByID(memoryID string) ([]float32, bool) {
	vec, ok, err := s.GetMemoryEmbedding(memoryID)
	if err != nil || !ok || len(vec) == 0 {
		return nil, false
	}
	return vec, true
}

func (s *S3ColdStore) collectColdCandidates(query string, topK int, cfg s3ColdSearchConfig) []s3ColdCandidate {
	ctx := context.Background()
	prefix := s.memoryPrefix()
	queryEmpty := strings.TrimSpace(query) == ""

	keys, err := s.cachedListObjects(ctx, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	if cfg.maxCandidates > 0 && len(keys) > cfg.maxCandidates {
		keys = keys[:cfg.maxCandidates]
	}

	targetCandidates := topK * cfg.bufferFactor
	if targetCandidates < topK {
		targetCandidates = topK

	}
	maxCandidates := targetCandidates * 2
	if maxCandidates < topK {
		maxCandidates = topK
	}
	if queryEmpty {
		// Dense-only cold search has no lexical prior. Keep the full configured
		// candidate window so recency prefiltering does not discard relevant
		// archived embeddings before vector reranking.
		maxCandidates = len(keys)
		if cfg.maxCandidates > 0 && cfg.maxCandidates < maxCandidates {
			maxCandidates = cfg.maxCandidates
		}
		targetCandidates = maxCandidates
	}

	results := make([]s3ColdCandidate, 0, min(maxCandidates, len(keys)))
	noImprovePages := 0
	prevCutoff := -1.0
	latestVersion := int64(0)

	for start := 0; start < len(keys); start += cfg.batchSize {
		end := start + cfg.batchSize
		if end > len(keys) {
			end = len(keys)
		}
		page := keys[start:end]

		pageResults := make([]s3ColdCandidate, 0, len(page))
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
				lexical := 1.0
				if strings.TrimSpace(query) != "" {
					lexical = scoreColdMemory(query, m)
				}
				if lexical <= 0 {
					return
				}

				mu.Lock()
				if m.Version > latestVersion {
					latestVersion = m.Version
				}
				pageResults = append(pageResults, s3ColdCandidate{
					id:           m.MemoryID,
					ts:           m.Version,
					lexicalScore: lexical,
				})
				mu.Unlock()
			}(key)
		}
		wg.Wait()

		if latestVersion > 0 {
			for i := range pageResults {
				pageResults[i].recencyScore = recencyScore(pageResults[i].ts, latestVersion)
			}
		}
		if len(pageResults) > 0 {
			results = append(results, pageResults...)
			results = selectTopCandidates(results, maxCandidates)
		}

		topNow := selectTopCandidates(results, topK)
		currCutoff := cutoffCandidateScore(topNow, topK)
		if currCutoff > prevCutoff {
			noImprovePages = 0
			prevCutoff = currCutoff
		} else {
			noImprovePages++
		}

		if !queryEmpty && shouldEarlyStopCandidates(topNow, len(results), topK, targetCandidates, cfg.earlyStopScore, noImprovePages, cfg.noImprovePages) {
			break
		}
	}

	if latestVersion > 0 {
		for i := range results {
			results[i].recencyScore = recencyScore(results[i].ts, latestVersion)
		}
	}
	return selectTopCandidates(results, maxCandidates)
}

func (s *S3ColdStore) ColdVectorSearch(queryVec []float32, topK int) []string {
	if topK <= 0 || len(queryVec) == 0 {
		return nil
	}

	ctx := context.Background()
	cfg := s.loadS3ColdSearchConfigFromEnv()
	algoCfg := s.algoCfg
	if algoCfg == (schemas.AlgorithmConfig{}) {
		algoCfg = schemas.DefaultAlgorithmConfig()
	}

	type scored struct {
		id    string
		score float64
		ts    int64
	}

	prefix := s.embeddingPrefix()
	keys, err := s.cachedListObjects(ctx, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	keys, pagesScanned, listTruncated := clampColdKeys(keys, cfg)

	maxCandidates := cfg.maxCandidates
	if maxCandidates <= 0 || maxCandidates > len(keys) {
		maxCandidates = len(keys)
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
				vec, err := bytesToFloat32Slice(data)
				if err != nil {
					return
				}
				denseScore := dotProduct(queryVec, vec)
				if denseScore < algoCfg.DFSRelevanceThreshold {
					return
				}
				memoryID := memoryIDFromEmbeddingKey(objectKey)
				if memoryID == "" {
					return
				}

				mu.Lock()
				pageResults = append(pageResults, scored{
					id:    memoryID,
					score: combineColdScores(denseScore, 0, 0, algoCfg.ColdSearchWeights),
					ts:    0,
				})
				mu.Unlock()
			}(key)
		}
		wg.Wait()

		if len(pageResults) > 0 {
			results = append(results, pageResults...)
			sort.Slice(results, func(i, j int) bool {
				if results[i].score != results[j].score {
					return results[i].score > results[j].score
				}
				return results[i].id < results[j].id
			})
			if len(results) > maxCandidates {
				results = results[:maxCandidates]
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].id < results[j].id
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
	if topK <= 0 || len(queryVec) == 0 {
		return nil
	}

	ctx := context.Background()
	prefix := s.embeddingPrefix()
	cfg := s.loadS3ColdSearchConfigFromEnv()

	keys, err := s.cachedListObjects(ctx, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	keys, _, _ = clampColdKeys(keys, cfg)

	dim := len(queryVec)
	if dim <= 0 {
		return nil
	}

	ids := make([]string, 0, len(keys))
	vectors := make([]float32, 0, len(keys)*dim)

	for start := 0; start < len(keys); start += cfg.batchSize {
		end := start + cfg.batchSize
		if end > len(keys) {
			end = len(keys)
		}
		page := keys[start:end]

		for _, key := range page {
			data, err := GetBytes(ctx, nil, s.cfg, key)
			if err != nil || data == nil {
				continue
			}

			vec, err := bytesToFloat32Slice(data)
			if err != nil {
				continue
			}
			if len(vec) != dim {
				// 维度不一致直接跳过，避免 Build/Search 出错
				continue
			}

			memoryID := memoryIDFromEmbeddingKey(key)
			if memoryID == "" {
				continue
			}

			ids = append(ids, memoryID)
			vectors = append(vectors, vec...)
		}
	}

	if len(ids) == 0 || len(vectors) == 0 {
		return nil
	}

	algoCfg := s.algoCfg
	if algoCfg == (schemas.AlgorithmConfig{}) {
		algoCfg = schemas.DefaultAlgorithmConfig()
	}

	retriever, err := retrievalplane.NewRetrieverWithMetric(
		dim,
		algoCfg.HNSWM,
		algoCfg.HNSEfConstruction,
		algoCfg.RRFK,
		"IP",
	)
	if err != nil || retriever == nil {
		// retrieval 不可用时优雅回退，让上层继续走 ColdVectorSearch
		return nil
	}
	defer retriever.Close()

	if err := retriever.Build(vectors, len(ids)); err != nil {
		return nil
	}

	intIDs, _, err := retriever.Search(queryVec, topK, nil)
	if err != nil || len(intIDs) == 0 {
		return nil
	}

	out := make([]string, 0, len(intIDs))
	for _, idx := range intIDs {
		i := int(idx)
		if i < 0 || i >= len(ids) {
			continue
		}
		out = append(out, ids[i])
	}
	return out
}

func clampColdKeys(keys []string, cfg s3ColdSearchConfig) ([]string, int, bool) {
	if len(keys) == 0 {
		return keys, 0, false
	}
	limit := len(keys)
	if cfg.batchSize > 0 && cfg.maxPages > 0 {
		pageLimit := cfg.batchSize * cfg.maxPages
		if pageLimit > 0 && pageLimit < limit {
			limit = pageLimit
		}
	}
	if cfg.maxCandidates > 0 && cfg.maxCandidates < limit {
		limit = cfg.maxCandidates
	}
	truncated := limit < len(keys)
	keys = keys[:limit]
	pages := 1
	if cfg.batchSize > 0 {
		pages = (len(keys) + cfg.batchSize - 1) / cfg.batchSize
	}
	return keys, pages, truncated
}
