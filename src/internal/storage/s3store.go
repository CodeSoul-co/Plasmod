package storage

import (
	"andb/retrievalplane"
	"andb/src/internal/schemas"
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
	algoCfg    schemas.AlgorithmConfig
	ensureOnce sync.Once
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
	maxKeys        int
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
		cfg:     cfg,
		algoCfg: algoCfg,
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
	return DeleteObject(context.Background(), nil, s.cfg, s.edgeKey(id))
}

// ListEdges is not supported for the S3 cold store — scanning all cold edge
// objects would require a list-objects API call with an unbounded result set.
// Callers should use GetEdge for point lookups.  Returns empty slice always.
func (s *S3ColdStore) ListEdges() []schemas.Edge {
	return []schemas.Edge{}
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
	cfg := s.loadS3ColdSearchConfigFromEnv()

	keys, err := ListObjects(ctx, nil, s.cfg, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	if cfg.maxKeys > 0 && len(keys) > cfg.maxKeys {
		keys = keys[:cfg.maxKeys]
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

func (s *S3ColdStore) loadS3ColdSearchConfigFromEnv() s3ColdSearchConfig {
	algoCfg := s.algoCfg
	if algoCfg == (schemas.AlgorithmConfig{}) {
		algoCfg = schemas.DefaultAlgorithmConfig()
	}

	cfg := s3ColdSearchConfig{
		maxKeys:        algoCfg.ColdMaxCandidates,
		concurrency:    8,
		batchSize:      algoCfg.ColdBatchSize,
		bufferFactor:   algoCfg.ColdBufferFactor,
		earlyStopScore: algoCfg.ColdEarlyStopScore,
		noImprovePages: algoCfg.ColdNoImprovePages,
	}
	if cfg.maxKeys <= 0 {
		cfg.maxKeys = 1000
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

	if v := parseFirstEnvIntWithDefault([]string{"S3_COLD_MAX_CANDIDATES", "S3_COLDSEARCH_MAX_KEYS"}, cfg.maxKeys); v > 0 {
		cfg.maxKeys = v
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
	prefix := fmt.Sprintf("%s/cold/memories/", s.cfg.Prefix)

	keys, err := ListObjects(ctx, nil, s.cfg, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	if cfg.maxKeys > 0 && len(keys) > cfg.maxKeys {
		keys = keys[:cfg.maxKeys]
	}

	targetCandidates := topK * cfg.bufferFactor
	if targetCandidates < topK {
		targetCandidates = topK
	}
	maxCandidates := targetCandidates * 2
	if maxCandidates < topK {
		maxCandidates = topK
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

		if shouldEarlyStopCandidates(topNow, len(results), topK, targetCandidates, cfg.earlyStopScore, noImprovePages, cfg.noImprovePages) {
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

	candidates := s.collectColdCandidates("", topK, cfg)
	if len(candidates) == 0 {
		return nil
	}

	results := make([]scored, 0, len(candidates))
	for _, cand := range candidates {
		vec, ok := s.getMemoryEmbeddingByID(cand.id)
		if !ok {
			continue
		}
		denseScore := dotProduct(queryVec, vec)
		if denseScore < algoCfg.DFSRelevanceThreshold {
			continue
		}
		results = append(results, scored{
			id: cand.id,
			score: combineColdScores(
				denseScore,
				cand.lexicalScore,
				cand.recencyScore,
				algoCfg.ColdSearchWeights,
			),
			ts: cand.ts,
		})
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
	return out
}

func (s *S3ColdStore) ColdHNSWSearch(queryVec []float32, topK int) []string {
	if topK <= 0 || len(queryVec) == 0 {
		return nil
	}

	ctx := context.Background()
	prefix := fmt.Sprintf("%s/cold/embeddings/", s.cfg.Prefix)
	cfg := s.loadS3ColdSearchConfigFromEnv()

	keys, err := ListObjects(ctx, nil, s.cfg, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}
	if cfg.maxKeys > 0 && len(keys) > cfg.maxKeys {
		keys = keys[:cfg.maxKeys]
	}

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
