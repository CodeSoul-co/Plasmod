package semantic

import (
	"strings"
	"time"

	"plasmod/src/internal/schemas"
)

// PolicyEngine evaluates governance rules against objects and queries.
// It implements the Policy Layer from the memory-semantic three-layer model:
// salience weighting, TTL/decay enforcement, ACL gating, quarantine checks,
// and verified-state awareness.
type PolicyEngine struct{}

// AccessPrincipal is the authenticated or request-bound identity used for
// canonical visibility decisions. AgentID falls back to QueryRequest.AgentID
// in Runtime for compatibility with existing clients.
type AccessPrincipal struct {
	AgentID     string
	Roles       []string
	TenantID    string
	WorkspaceID string
	TeamID      string
	SessionID   string
}

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
	if strings.TrimSpace(req.DatasetName) != "" || strings.TrimSpace(req.SourceFileName) != "" {
		filters = append(filters, "dataset_selector")
	}
	if strings.TrimSpace(req.ImportBatchID) != "" || req.LatestBatchOnly {
		filters = append(filters, "import_batch")
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

// EvaluateAccess applies the canonical scope contract and the current visible
// WAL frontier. It returns a positive decision only when both visibility and
// causality are safe for the requester to observe.
func (p *PolicyEngine) EvaluateAccess(
	objectID string,
	ownerAgentID string,
	access schemas.CanonicalAccess,
	principal AccessPrincipal,
	contracts []schemas.ShareContract,
	mutationLSN int64,
	readWatermarkLSN int64,
) (schemas.AccessDecision, bool) {
	decision := schemas.AccessDecision{
		ObjectID:        objectID,
		PrincipalID:     principal.AgentID,
		Visibility:      access.Visibility,
		ShareContractID: access.ShareContractID,
		MutationLSN:     mutationLSN,
	}
	allow := func(reason string) (schemas.AccessDecision, bool) {
		decision.Reason = reason
		return decision, true
	}
	deny := func(reason string) (schemas.AccessDecision, bool) {
		decision.Reason = reason
		return decision, false
	}

	if mutationLSN > 0 && mutationLSN > readWatermarkLSN {
		return deny("mutation_not_visible")
	}
	if access.TenantID != "" && principal.TenantID != "" && access.TenantID != principal.TenantID {
		return deny("tenant_mismatch")
	}
	if principal.AgentID != "" && principal.AgentID == firstNonEmptyAccess(ownerAgentID, access.OwnerAgentID) {
		return allow("owner")
	}
	if access.ShareContractID != "" {
		for _, contract := range contracts {
			if contract.ContractID != access.ShareContractID || !contractMatchesAccessScope(contract, access) {
				continue
			}
			if contractAllowsRead(contract, principal) {
				decision.ShareContractID = contract.ContractID
				return allow("share_contract")
			}
		}
		return deny("share_contract_denied")
	}
	if containsAccessValue(access.VisibleToAgents, principal.AgentID) {
		return allow("explicit_agent_grant")
	}
	if intersectsAccessValues(access.VisibleToRoles, principal.Roles) {
		return allow("explicit_role_grant")
	}

	visibility := strings.ToLower(strings.TrimSpace(access.Visibility))
	switch visibility {
	case string(schemas.VisibilityPublic), string(schemas.MemoryScopeGlobalShared), "global":
		return allow("public")
	case string(schemas.VisibilityTenant):
		if access.TenantID != "" && access.TenantID == principal.TenantID {
			return allow("tenant")
		}
	case string(schemas.VisibilityWorkspace), string(schemas.VisibilityShared), string(schemas.MemoryScopeWorkspaceShared):
		if access.WorkspaceID != "" && access.WorkspaceID == principal.WorkspaceID {
			return allow("workspace")
		}
	case string(schemas.VisibilityTeam), string(schemas.MemoryScopeTeamShared):
		if access.TeamID != "" && access.TeamID == principal.TeamID {
			return allow("team")
		}
	case string(schemas.VisibilitySession), string(schemas.MemoryScopeSessionLocal):
		if access.SessionID != "" && access.SessionID == principal.SessionID {
			return allow("session")
		}
	case string(schemas.VisibilityPrivate), string(schemas.MemoryScopePrivateAgent), string(schemas.MemoryScopePrivateUser):
		return deny("private")
	case string(schemas.MemoryScopeRestrictedShared):
		return deny("restricted_without_grant")
	case "":
		// Legacy canonical records may not yet contain CanonicalAccess. Preserve
		// their historical workspace/session/owner semantics without treating an
		// unscoped owned record as public.
		if ownerAgentID == "" {
			return allow("legacy_unowned")
		}
	}
	return deny("scope_not_visible")
}

func contractAllowsRead(contract schemas.ShareContract, principal AccessPrincipal) bool {
	return shareContractAllows("read", contract, principal)
}

// IsShareContractAllowed evaluates one typed ShareContract permission. It is
// used by collaboration mutations before they create a derived canonical
// object; query reads call the same evaluator through EvaluateAccess.
func (p *PolicyEngine) IsShareContractAllowed(operation string, contract schemas.ShareContract, principal AccessPrincipal) bool {
	return shareContractAllows(operation, contract, principal)
}

func shareContractAllows(operation string, contract schemas.ShareContract, principal AccessPrincipal) bool {
	if contract.TenantID != "" && contract.TenantID != principal.TenantID {
		return false
	}
	if contract.WorkspaceID != "" && contract.WorkspaceID != principal.WorkspaceID {
		return false
	}
	var agents, roles []string
	var legacyACL string
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "read":
		agents, roles, legacyACL = contract.ReadAgents, contract.ReadRoles, contract.ReadACL
	case "write":
		agents, roles, legacyACL = contract.WriteAgents, contract.WriteRoles, contract.WriteACL
	case "derive":
		agents, roles, legacyACL = contract.DeriveAgents, contract.DeriveRoles, contract.DeriveACL
	default:
		return false
	}
	if containsAccessValue(agents, principal.AgentID) || intersectsAccessValues(roles, principal.Roles) {
		return true
	}
	for _, token := range strings.FieldsFunc(legacyACL, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		token = strings.TrimSpace(token)
		switch {
		case token == "*":
			return true
		case token == principal.AgentID, token == "agent:"+principal.AgentID:
			return principal.AgentID != ""
		case strings.HasPrefix(token, "role:") && containsAccessValue(principal.Roles, strings.TrimPrefix(token, "role:")):
			return true
		}
	}
	return false
}

func contractMatchesAccessScope(contract schemas.ShareContract, access schemas.CanonicalAccess) bool {
	if contract.TenantID != "" && contract.TenantID != access.TenantID {
		return false
	}
	if contract.WorkspaceID != "" && contract.WorkspaceID != access.WorkspaceID {
		return false
	}
	if contract.Scope == "" {
		return true
	}
	return contract.Scope == access.WorkspaceID ||
		contract.Scope == access.TeamID ||
		contract.Scope == access.SessionID
}

func containsAccessValue(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target || strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

func intersectsAccessValues(left, right []string) bool {
	for _, value := range right {
		if containsAccessValue(left, value) {
			return true
		}
	}
	return false
}

func firstNonEmptyAccess(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
