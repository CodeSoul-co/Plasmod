package dataplane

import (
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// DefaultEmbeddingDim is the default vector dimensionality used by TfidfEmbedder.
// LLM-based embedding generators may use different dimensions (e.g. 768, 1536).
const DefaultEmbeddingDim = 256

// EmbeddingGenerator is the pluggable interface for converting text to float32 vectors.
//
// Default implementation: TfidfEmbedder (pure Go, no LLM dependency).
// Future plugins: OpenAI, ZhipuAI, or any HTTP/gRPC embedding service by
// registering an implementation that satisfies this interface.
type EmbeddingGenerator interface {
	// Generate returns a dense float32 vector for the given text.
	// The returned slice length must equal Dim().
	Generate(text string) ([]float32, error)
	// Dim returns the dimensionality of vectors produced by this generator.
	Dim() int
	// Reset clears internal statistics. Called after Flush so that
	// incremental counters (e.g. document frequency) can be restarted.
	Reset()
}

// TfidfEmbedder is a pure-Go, no-LLM embedding generator using word-hashed TF-IDF.
// It is suitable for demos and offline environments where no external embedding
// service is available.
//
// Algorithm (word-hashing TF-IDF, inspired by Microsoft's DSSM):
//   1. Tokenize: lowercase, split on non-alphanumeric, discard stopwords.
//   2. Word-hashing: map each word to one of `dim` buckets via FNV-1a hash + modulo.
//      Multiple words may collide in the same bucket (explicit trade-off for fixed dim).
//   3. TF-IDF: weight = (term_count_in_doc / doc_len) * log((N + 1) / (df + 1)) + 1.
//   4. L2-normalize the resulting dense vector.
type TfidfEmbedder struct {
	dim       int
	docFreq   []int       // [dim] number of docs containing each bucket (IDF numerator)
	totalDocs int          // total docs observed since last Reset

	mu sync.Mutex // protects docFreq and totalDocs during Ingest
}

// NewTfidfEmbedder creates a TfidfEmbedder with the given vector dimension.
// dim must be a positive power-of-2 for best hash distribution.
func NewTfidfEmbedder(dim int) *TfidfEmbedder {
	if dim <= 0 {
		dim = DefaultEmbeddingDim
	}
	return &TfidfEmbedder{
		dim:     dim,
		docFreq: make([]int, dim),
	}
}

// Dim returns the embedding dimensionality.
func (e *TfidfEmbedder) Dim() int { return e.dim }

// Reset clears document-frequency counters and total-doc count.
// Call this after flushing the index so fresh IDF values accumulate
// for the next batch.
func (e *TfidfEmbedder) Reset() {
	e.mu.Lock()
	e.totalDocs = 0
	e.docFreq = make([]int, e.dim)
	e.mu.Unlock()
}

// Generate returns a TF-IDF vector for the given text.
// Thread-safe: multiple goroutines may call Generate concurrently.
func (e *TfidfEmbedder) Generate(text string) ([]float32, error) {
	if text == "" {
		vec := make([]float32, e.dim)
		return vec, nil
	}

	tokens := e.tokenize(text)
	if len(tokens) == 0 {
		return make([]float32, e.dim), nil
	}

	// Count tokens per bucket (TF)
	bucketCounts := make(map[int]int, len(tokens))
	for _, tok := range tokens {
		bucket := e.hashBucket(tok)
		bucketCounts[bucket]++
	}

	docLen := float64(len(tokens))

	e.mu.Lock()
	n := e.totalDocs
	df := e.docFreq
	e.mu.Unlock()

	vec := make([]float32, e.dim)
	for bucket, count := range bucketCounts {
		tf := float64(count) / docLen
		idf := math.Log((float64(n)+1)/(float64(df[bucket])+1)) + 1
		vec[bucket] = float32(tf * idf)
	}

	// L2-normalize
	e.l2Normalize(vec)
	return vec, nil
}

// ObserveTokens updates the document-frequency counters for the given text.
// It must be called once per ingested document (before Build/RRF search).
// Safe for concurrent calls.
func (e *TfidfEmbedder) ObserveTokens(text string) {
	if text == "" {
		return
	}
	tokens := e.tokenize(text)
	if len(tokens) == 0 {
		return
	}

	// Accumulate which buckets appear in this document
	seen := make(map[int]struct{}, len(tokens))
	for _, tok := range tokens {
		seen[e.hashBucket(tok)] = struct{}{}
	}

	e.mu.Lock()
	e.totalDocs++
	for b := range seen {
		e.docFreq[b]++
	}
	e.mu.Unlock()
}

// tokenize splits text into mixed-language tokens.
// - ASCII letters/digits: grouped into word tokens (len > 1)
// - CJK Han runes: kept as single-rune tokens
// This keeps TF-IDF usable for Chinese text without external segmenters.
func (e *TfidfEmbedder) tokenize(text string) []string {
	lower := strings.ToLower(text)

	isASCIIAlphaNum := func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
	}

	out := make([]string, 0, len(lower))
	buf := make([]rune, 0, 16)
	flushASCII := func() {
		if len(buf) > 1 {
			out = append(out, string(buf))
		}
		buf = buf[:0]
	}

	for _, r := range lower {
		if isASCIIAlphaNum(r) {
			buf = append(buf, r)
			continue
		}
		flushASCII()
		if unicode.Is(unicode.Han, r) {
			out = append(out, string(r))
		}
	}
	flushASCII()
	return out
}

// hashBucket maps a word to a bucket index using FNV-1a.
func (e *TfidfEmbedder) hashBucket(word string) int {
	h := fnv.New32a()
	h.Write([]byte(word))
	return int(h.Sum32() % uint32(e.dim))
}

// l2Normalize divides each element by the L2 norm in-place.
func (e *TfidfEmbedder) l2Normalize(vec []float32) {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return
	}
	norm = math.Sqrt(norm)
	inv := float32(1.0 / norm)
	for i := range vec {
		vec[i] *= inv
	}
}

// FusedVectorAndScores holds the result of a vector search before RRF fusion.
type fusedResult struct {
	objectID string
	rrfScore float64
}

// rrfFuse merges two ranked lists using Reciprocal Rank Fusion.
// lexical / vecIDs: ordered slices of objectIDs from lexical and vector search (highest score first).
// vecScoresF: float scores from vector search (index-aligned with vecIDs), may be nil.
// topK: maximum results to return.
// rrfK: RRF constant (default 60); higher = more weight to lexical rank.
func rrfFuse(lexical, vecIDs []string, vecScoresF []float64, topK, rrfK int) []string {
	if topK <= 0 {
		topK = 10
	}
	// Build RRF score map
	rrf := make(map[string]float64)

	for rank, id := range lexical {
		rrf[id] += 1.0 / float64(rrfK+rank+1)
	}
	for rank, id := range vecIDs {
		rrf[id] += 1.0 / float64(rrfK+rank+1)
	}

	// Sort by RRF score descending
	type kv struct {
		id    string
		score float64
	}
	pairs := make([]kv, 0, len(rrf))
	for id, sc := range rrf {
		pairs = append(pairs, kv{id, sc})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	out := make([]string, 0, topK)
	for i := 0; i < len(pairs) && len(out) < topK; i++ {
		out = append(out, pairs[i].id)
	}
	return out
}
