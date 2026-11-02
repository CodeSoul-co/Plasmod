package schemas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// MemoryFactIdentity uses a persisted equivalence decision when available. The
// fallback also lets existing facts participate without rewriting their sources.
func MemoryFactIdentity(m Memory) string {
	if m.FactIdentity != "" {
		return m.FactIdentity
	}
	data, _ := json.Marshal([]string{m.TenantID, m.WorkspaceID, m.AgentID, m.MemoryType, strings.ToLower(strings.TrimSpace(m.Content)), m.FactValidFrom, m.FactValidTo})
	sum := sha256.Sum256(data)
	return "fact_" + hex.EncodeToString(sum[:])
}

// SameMemoryFact never joins different scopes, types or explicit validity periods.
func SameMemoryFact(a, b Memory) bool {
	return a.TenantID == b.TenantID && a.WorkspaceID == b.WorkspaceID && a.AgentID == b.AgentID && a.MemoryType == b.MemoryType && a.FactValidFrom == b.FactValidFrom && a.FactValidTo == b.FactValidTo && MemoryFactIdentity(a) == MemoryFactIdentity(b)
}
