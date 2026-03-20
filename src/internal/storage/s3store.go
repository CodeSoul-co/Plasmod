package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"andb/src/internal/s3util"
	"andb/src/internal/schemas"
)

// S3ColdStore implements ColdObjectStore backed by a MinIO/S3-compatible bucket.
//
// Object layout inside the bucket (all under cfg.Prefix):
//
//	{prefix}/cold/memories/{memory_id}.json
//	{prefix}/cold/agents/{agent_id}.json
//
// Writes use s3util.PutBytes (no round-trip verify) for low-latency archival.
// Reads use s3util.GetBytes; a 404 is treated as "not found" (returns false).
type S3ColdStore struct {
	cfg s3util.S3Config
}

// NewS3ColdStore returns an S3-backed ColdObjectStore using the supplied config.
func NewS3ColdStore(cfg s3util.S3Config) *S3ColdStore {
	return &S3ColdStore{cfg: cfg}
}

func (s *S3ColdStore) memoryKey(id string) string {
	return fmt.Sprintf("%s/cold/memories/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) agentKey(id string) string {
	return fmt.Sprintf("%s/cold/agents/%s.json", s.cfg.Prefix, id)
}

func (s *S3ColdStore) PutMemory(m schemas.Memory) {
	data, err := json.Marshal(m)
	if err != nil {
		log.Printf("s3cold: marshal memory %s: %v", m.MemoryID, err)
		return
	}
	if err := s3util.PutBytes(context.Background(), nil, s.cfg, s.memoryKey(m.MemoryID), data, "application/json"); err != nil {
		log.Printf("s3cold: put memory %s: %v", m.MemoryID, err)
	}
}

func (s *S3ColdStore) GetMemory(id string) (schemas.Memory, bool) {
	data, err := s3util.GetBytes(context.Background(), nil, s.cfg, s.memoryKey(id))
	if err != nil {
		log.Printf("s3cold: get memory %s: %v", id, err)
		return schemas.Memory{}, false
	}
	if data == nil {
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
	data, err := json.Marshal(a)
	if err != nil {
		log.Printf("s3cold: marshal agent %s: %v", a.AgentID, err)
		return
	}
	if err := s3util.PutBytes(context.Background(), nil, s.cfg, s.agentKey(a.AgentID), data, "application/json"); err != nil {
		log.Printf("s3cold: put agent %s: %v", a.AgentID, err)
	}
}

func (s *S3ColdStore) GetAgent(id string) (schemas.Agent, bool) {
	data, err := s3util.GetBytes(context.Background(), nil, s.cfg, s.agentKey(id))
	if err != nil {
		log.Printf("s3cold: get agent %s: %v", id, err)
		return schemas.Agent{}, false
	}
	if data == nil {
		return schemas.Agent{}, false
	}
	var a schemas.Agent
	if err := json.Unmarshal(data, &a); err != nil {
		log.Printf("s3cold: unmarshal agent %s: %v", id, err)
		return schemas.Agent{}, false
	}
	return a, true
}
