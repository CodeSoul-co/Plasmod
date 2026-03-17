package semantic

import (
	"time"

	"andb/src/internal/schemas"
)

// PolicyEngine evaluates governance rules against objects and queries.
// It implements the Policy Layer from the memory-semantic three-layer model:
// salience weighting, TTL/decay enforcement, ACL gating, quarantine checks,
// and verified-state awareness.
type PolicyEngine struct{}

func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{}
}

// ApplyQueryFilters derives the active filter set for a query based on the
// constraints present in the request.  The returned strings are proof-trace
// tokens that appear in QueryResponse.AppliedFilters.
func (p *PolicyEngine) ApplyQueryFilters(req schemas.QueryRequest) []string {
	filters := []string{"scope", "visibility", "acl"}
	if req.TimeWindow.From != "" || req.TimeWindow.To != "" {
		filters = append(filters, "time_window_bound")
	}
	if len(req.RelationConstraints) > 0 {
		filters = append(filters, "relation_constraints")
	}
	filters = append(filters, "ttl_active", "quarantine_excluded", "salience_threshold")
	return filters
}

// IsTTLExpired returns true when the memory's TTL has elapsed relative to now.
// TTL == 0 means no expiry.
func (p *PolicyEngine) IsTTLExpired(mem schemas.Memory) bool {
	if mem.TTL <= 0 {
		return false
	}
	base, err := time.Parse(time.RFC3339, mem.ValidFrom)
	if err != nil {
		return false
	}
	return time.Since(base) > time.Duration(mem.TTL)*time.Second
}

// IsQuarantined returns true when the active policy record for the object has
// its quarantine flag set.
func (p *PolicyEngine) IsQuarantined(policies []schemas.PolicyRecord) bool {
	for _, pol := range policies {
		if pol.QuarantineFlag {
			return true
		}
	}
	return false
}

// EffectiveSalience returns the salience weight from the most recent policy
// record, or the memory's own importance when no policy record exists.
func (p *PolicyEngine) EffectiveSalience(mem schemas.Memory, policies []schemas.PolicyRecord) float64 {
	var latest schemas.PolicyRecord
	found := false
	for _, pol := range policies {
		if !found || pol.PolicyEventID > latest.PolicyEventID {
			latest = pol
			found = true
		}
	}
	if found && latest.SalienceWeight > 0 {
		return latest.SalienceWeight
	}
	return mem.Importance
}

// EffectiveConfidence returns the confidence value to use for ranking,
// honouring any policy override.
func (p *PolicyEngine) EffectiveConfidence(mem schemas.Memory, policies []schemas.PolicyRecord) float64 {
	for _, pol := range policies {
		if pol.ConfidenceOverride > 0 {
			return pol.ConfidenceOverride
		}
	}
	return mem.Confidence
}

// IsACLAllowed checks whether the requesting agent is permitted to read the
// object given the active share contract.  An empty ReadACL means open access.
func (p *PolicyEngine) IsACLAllowed(agentID string, contract schemas.ShareContract) bool {
	if contract.ReadACL == "" || contract.ReadACL == "*" {
		return true
	}
	return contract.ReadACL == agentID
}

// IsVerified returns true when the most recent policy record confirms the
// object has been verified and not retracted.
func (p *PolicyEngine) IsVerified(policies []schemas.PolicyRecord) bool {
	for _, pol := range policies {
		if pol.VerifiedState == string(schemas.VerifiedStateVerified) {
			return true
		}
		if pol.VerifiedState == string(schemas.VerifiedStateRetracted) {
			return false
		}
	}
	return false
}
