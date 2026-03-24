package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
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

func (s *S3ColdStore) memoryKey(id string) string {
	return fmt.Sprintf("%s/cold/memories/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) agentKey(id string) string {
	return fmt.Sprintf("%s/cold/agents/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) stateKey(id string) string {
	return fmt.Sprintf("%s/cold/states/%s.json", s.cfg.Prefix, id)
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
	ctx := context.Background()
	prefix := fmt.Sprintf("%s/cold/memories/", s.cfg.Prefix)

	keys, err := s3util.ListObjects(ctx, nil, s.cfg, prefix)
	if err != nil || len(keys) == 0 {
		return nil
	}

	type scored struct {
		key  string
		id   string
		ts   int64
		score float64
	}
	var results []scored
	lq := strings.ToLower(query)

	for _, key := range keys {
		data, err := s3util.GetBytes(ctx, nil, s.cfg, key)
		if err != nil || data == nil {
			continue
		}
		var m schemas.Memory
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		text := strings.ToLower(m.Content)
		summary := strings.ToLower(m.Summary)
		var score float64
		if strings.Contains(text, lq) || strings.Contains(summary, lq) {
			score = 1.0
		} else {
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
			if len(qTokens) > 0 {
				score = float64(match) / float64(len(qTokens))
			}
		}
		if score > 0 {
			results = append(results, scored{key: key, id: m.MemoryID, ts: m.Version, score: score})
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
	return out
}
