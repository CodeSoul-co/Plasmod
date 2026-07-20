package semantic

import (
	"testing"

	"plasmod/src/internal/schemas"
)

func TestPolicyEngineEvaluateAccess(t *testing.T) {
	engine := NewPolicyEngine()
	base := schemas.CanonicalAccess{
		TenantID:     "tenant",
		WorkspaceID:  "workspace",
		TeamID:       "team",
		OwnerAgentID: "owner",
		SessionID:    "session",
		Visibility:   string(schemas.VisibilityPrivate),
	}
	principal := AccessPrincipal{
		AgentID: "reader", TenantID: "tenant", WorkspaceID: "workspace",
		TeamID: "team", SessionID: "session",
	}

	tests := []struct {
		name       string
		access     schemas.CanonicalAccess
		principal  AccessPrincipal
		contracts  []schemas.ShareContract
		mutation   int64
		watermark  int64
		wantAllow  bool
		wantReason string
	}{
		{name: "owner", access: base, principal: AccessPrincipal{AgentID: "owner", TenantID: "tenant"}, watermark: 10, wantAllow: true, wantReason: "owner"},
		{name: "explicit tenant mismatch is denied", access: base, principal: AccessPrincipal{AgentID: "owner", TenantID: "other"}, watermark: 10, wantReason: "tenant_mismatch"},
		{name: "legacy request may omit tenant when session matches", access: withVisibility(base, string(schemas.VisibilitySession)), principal: AccessPrincipal{SessionID: "session"}, watermark: 10, wantAllow: true, wantReason: "session"},
		{name: "future mutation is denied", access: base, principal: AccessPrincipal{AgentID: "owner", TenantID: "tenant"}, mutation: 11, watermark: 10, wantReason: "mutation_not_visible"},
		{name: "explicit agent", access: withAgentGrant(base, "reader"), principal: principal, watermark: 10, wantAllow: true, wantReason: "explicit_agent_grant"},
		{name: "explicit role", access: withRoleGrant(base, "critic"), principal: withRoles(principal, "critic"), watermark: 10, wantAllow: true, wantReason: "explicit_role_grant"},
		{name: "workspace scope", access: withVisibility(base, string(schemas.VisibilityWorkspace)), principal: principal, watermark: 10, wantAllow: true, wantReason: "workspace"},
		{
			name: "explicit share contract", access: withContract(base, "contract-1"), principal: principal,
			contracts: []schemas.ShareContract{{ContractID: "contract-1", TenantID: "tenant", WorkspaceID: "workspace", Scope: "workspace", ReadAgents: []string{"reader"}}},
			watermark: 10, wantAllow: true, wantReason: "share_contract",
		},
		{
			name: "unreferenced contract cannot grant", access: base, principal: principal,
			contracts: []schemas.ShareContract{{ContractID: "contract-1", ReadAgents: []string{"reader"}}},
			watermark: 10, wantReason: "private",
		},
		{
			name: "contract scope mismatch", access: withContract(base, "contract-1"), principal: principal,
			contracts: []schemas.ShareContract{{ContractID: "contract-1", TenantID: "tenant", WorkspaceID: "other", ReadAgents: []string{"reader"}}},
			watermark: 10, wantReason: "share_contract_denied",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, allowed := engine.EvaluateAccess("object", "owner", test.access, test.principal, test.contracts, test.mutation, test.watermark)
			if allowed != test.wantAllow || decision.Reason != test.wantReason {
				t.Fatalf("allowed=%t reason=%q, want allowed=%t reason=%q", allowed, decision.Reason, test.wantAllow, test.wantReason)
			}
		})
	}
}

func withAgentGrant(access schemas.CanonicalAccess, agentID string) schemas.CanonicalAccess {
	access.VisibleToAgents = []string{agentID}
	return access
}

func withRoleGrant(access schemas.CanonicalAccess, role string) schemas.CanonicalAccess {
	access.VisibleToRoles = []string{role}
	return access
}

func withRoles(principal AccessPrincipal, roles ...string) AccessPrincipal {
	principal.Roles = roles
	return principal
}

func withVisibility(access schemas.CanonicalAccess, visibility string) schemas.CanonicalAccess {
	access.Visibility = visibility
	return access
}

func withContract(access schemas.CanonicalAccess, contractID string) schemas.CanonicalAccess {
	access.ShareContractID = contractID
	return access
}
