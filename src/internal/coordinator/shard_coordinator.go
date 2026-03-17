package coordinator

import (
	"fmt"
	"hash/fnv"
)

// ShardCoordinator maps ingest requests to WAL channels and segment namespaces
// following the priority order: tenant_id → workspace_id → agent_id → session_id.
// This mirrors the Manu hash-ring approach described in spec section 15.
type ShardCoordinator struct {
	numShards int
}

func NewShardCoordinator(numShards int) *ShardCoordinator {
	if numShards <= 0 {
		numShards = 8
	}
	return &ShardCoordinator{numShards: numShards}
}

// ChannelFor returns the WAL channel name for the given routing key tuple.
// Shared scopes get a dedicated namespace channel.
func (c *ShardCoordinator) ChannelFor(tenantID, agentID, sessionID string) string {
	key := tenantID + "/" + agentID + "/" + sessionID
	shard := c.hash(key) % uint32(c.numShards)
	return fmt.Sprintf("wal.shard.%d", shard)
}

// NamespaceFor derives the segment namespace for a given object using the
// same priority ordering used for WAL channel assignment.
func (c *ShardCoordinator) NamespaceFor(workspaceID, agentID, sessionID string) string {
	if workspaceID != "" {
		return workspaceID
	}
	if agentID != "" {
		return agentID
	}
	if sessionID != "" {
		return sessionID
	}
	return "default"
}

// SegmentName produces a human-readable segment identifier following the
// convention from spec section 15.2: objectType.scope.timeBucket.agentID
func (c *ShardCoordinator) SegmentName(objectType, scope, timeBucket, agentID string) string {
	return fmt.Sprintf("%s.%s.%s.%s", objectType, scope, timeBucket, agentID)
}

func (c *ShardCoordinator) hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
