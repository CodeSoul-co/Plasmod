package eventbackbone

import "sync"

// PolicyDecisionEntry records a governance decision that was applied to an
// object — e.g. quarantine set, TTL adjusted, ACL updated.  This log is
// consumed by ReflectionPolicy and ConflictMerge workers (spec section 16.4).
type PolicyDecisionEntry struct {
	LSN        int64  `json:"lsn"`
	ObjectID   string `json:"object_id"`
	ObjectType string `json:"object_type"`
	PolicyID   string `json:"policy_id"`
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	LogicalTS  int64  `json:"logical_ts"`
}

// PolicyDecisionLog is an append-only log of governance decisions.
// It is published on the "policy.decisions" channel so the Policy Coordinator
// and workers can react asynchronously.
type PolicyDecisionLog struct {
	mu      sync.RWMutex
	clock   *HybridClock
	bus     Bus
	entries []PolicyDecisionEntry
}

func NewPolicyDecisionLog(clock *HybridClock, bus Bus) *PolicyDecisionLog {
	return &PolicyDecisionLog{clock: clock, bus: bus}
}

// Append records a policy decision and broadcasts it on "policy.decisions".
func (l *PolicyDecisionLog) Append(objectID, objectType, policyID, decision, reason string) PolicyDecisionEntry {
	ts := l.clock.Next()
	entry := PolicyDecisionEntry{
		LSN:        ts,
		ObjectID:   objectID,
		ObjectType: objectType,
		PolicyID:   policyID,
		Decision:   decision,
		Reason:     reason,
		LogicalTS:  ts,
	}
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	l.mu.Unlock()
	l.bus.Publish(Message{Channel: "policy.decisions", Body: entry})
	return entry
}

// ForObject returns all decisions recorded for the given object ID.
func (l *PolicyDecisionLog) ForObject(objectID string) []PolicyDecisionEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := []PolicyDecisionEntry{}
	for _, e := range l.entries {
		if e.ObjectID == objectID {
			out = append(out, e)
		}
	}
	return out
}

// Since returns all entries with LSN >= fromLSN.
func (l *PolicyDecisionLog) Since(fromLSN int64) []PolicyDecisionEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := []PolicyDecisionEntry{}
	for _, e := range l.entries {
		if e.LSN >= fromLSN {
			out = append(out, e)
		}
	}
	return out
}

// Wipe clears all policy decision entries (admin full data wipe).
func (l *PolicyDecisionLog) Wipe() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = nil
}
