package worker

import (
	"strings"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/semantic"
)

type canonicalAccessRecord struct {
	objectID     string
	ownerAgentID string
	access       schemas.CanonicalAccess
	mutationLSN  int64
	exists       bool
}

func (r *Runtime) filterObjectIDsByAccess(
	req schemas.QueryRequest,
	objectIDs []string,
	readWatermarkLSN int64,
) ([]string, []schemas.AccessDecision) {
	principal := r.accessPrincipal(req)
	contracts := r.storage.Contracts().ListContracts()
	if r.capabilities.GovernanceProfile == "no_share_contract" || r.capabilities.GovernanceProfile == "metadata_only" {
		contracts = nil
	}
	allowed := make([]string, 0, len(objectIDs))
	decisions := make([]schemas.AccessDecision, 0, len(objectIDs))
	seen := make(map[string]struct{}, len(objectIDs))
	for _, objectID := range objectIDs {
		objectID = strings.TrimSpace(objectID)
		if objectID == "" {
			continue
		}
		if _, ok := seen[objectID]; ok {
			continue
		}
		seen[objectID] = struct{}{}
		decision, ok := r.objectAccessDecision(req, principal, contracts, objectID, readWatermarkLSN)
		if !ok {
			continue
		}
		allowed = append(allowed, objectID)
		decisions = append(decisions, decision)
	}
	return allowed, decisions
}

func (r *Runtime) accessPrincipal(req schemas.QueryRequest) semantic.AccessPrincipal {
	agentID := strings.TrimSpace(req.RequesterAgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(req.AgentID)
	}
	roles := append([]string(nil), req.RequesterRoles...)
	if agentID != "" && len(roles) == 0 && r.storage != nil && r.storage.Objects() != nil {
		if agent, ok := r.storage.Objects().GetAgent(agentID); ok && strings.TrimSpace(agent.RoleProfile) != "" {
			roles = []string{strings.TrimSpace(agent.RoleProfile)}
		}
	}
	return semantic.AccessPrincipal{
		AgentID:     agentID,
		Roles:       roles,
		TenantID:    strings.TrimSpace(req.TenantID),
		WorkspaceID: strings.TrimSpace(req.WorkspaceID),
		TeamID:      strings.TrimSpace(req.TeamID),
		SessionID:   strings.TrimSpace(req.SessionID),
	}
}

func (r *Runtime) objectAccessDecision(
	req schemas.QueryRequest,
	principal semantic.AccessPrincipal,
	contracts []schemas.ShareContract,
	objectID string,
	readWatermarkLSN int64,
) (schemas.AccessDecision, bool) {
	record := r.canonicalAccessRecord(objectID)
	if !record.exists {
		if r.VectorOnlyMode || vectorOnlyModeEnabled() || r.capabilities.MaterializationProfile == "none" || strings.TrimSpace(req.WarmSegmentID) != "" {
			return schemas.AccessDecision{
				ObjectID:    objectID,
				PrincipalID: principal.AgentID,
				Reason:      "projection_only",
			}, true
		}
		return schemas.AccessDecision{}, false
	}
	if r.GovernanceDisabled || r.capabilities.GovernanceProfile == "no_access" {
		if record.mutationLSN > 0 && record.mutationLSN > readWatermarkLSN {
			return schemas.AccessDecision{}, false
		}
		return schemas.AccessDecision{
			ObjectID:    objectID,
			PrincipalID: principal.AgentID,
			Visibility:  record.access.Visibility,
			Reason:      "governance_disabled",
			MutationLSN: record.mutationLSN,
		}, true
	}
	if r.capabilities.GovernanceProfile == "metadata_only" {
		return metadataAccessDecision(record, principal, readWatermarkLSN)
	}
	decision, allowed := r.policy.EvaluateAccess(
		record.objectID,
		record.ownerAgentID,
		record.access,
		principal,
		contracts,
		record.mutationLSN,
		readWatermarkLSN,
	)
	if !allowed {
		return decision, false
	}
	if edge, ok := r.storage.Edges().GetEdge(objectID); ok {
		for _, endpointID := range []string{edge.SrcObjectID, edge.DstObjectID} {
			endpoint := r.canonicalAccessRecord(endpointID)
			if !endpoint.exists {
				continue
			}
			if _, endpointAllowed := r.policy.EvaluateAccess(
				endpoint.objectID,
				endpoint.ownerAgentID,
				endpoint.access,
				principal,
				contracts,
				endpoint.mutationLSN,
				readWatermarkLSN,
			); !endpointAllowed {
				return schemas.AccessDecision{}, false
			}
		}
	}
	return decision, true
}

func metadataAccessDecision(record canonicalAccessRecord, principal semantic.AccessPrincipal, readWatermarkLSN int64) (schemas.AccessDecision, bool) {
	decision := schemas.AccessDecision{
		ObjectID: record.objectID, PrincipalID: principal.AgentID,
		Visibility: record.access.Visibility, Reason: "metadata_filter",
		MutationLSN: record.mutationLSN,
	}
	if record.mutationLSN > 0 && record.mutationLSN > readWatermarkLSN {
		return decision, false
	}
	if record.access.TenantID != "" && principal.TenantID != "" && record.access.TenantID != principal.TenantID {
		return decision, false
	}
	if record.ownerAgentID != "" && record.ownerAgentID == principal.AgentID {
		return decision, true
	}
	if metadataContainsString(record.access.VisibleToAgents, principal.AgentID) || metadataIntersectsStrings(record.access.VisibleToRoles, principal.Roles) {
		return decision, true
	}
	switch strings.ToLower(strings.TrimSpace(record.access.Visibility)) {
	case "", string(schemas.VisibilityPrivate):
		return decision, false
	case string(schemas.VisibilitySession):
		return decision, record.access.SessionID != "" && record.access.SessionID == principal.SessionID
	case string(schemas.VisibilityTeam):
		return decision, record.access.TeamID != "" && record.access.TeamID == principal.TeamID
	case string(schemas.VisibilityWorkspace):
		return decision, record.access.WorkspaceID != "" && record.access.WorkspaceID == principal.WorkspaceID
	case string(schemas.VisibilityPublic), "shared":
		return decision, true
	default:
		return decision, false
	}
}

func metadataContainsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" && strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func metadataIntersectsStrings(left, right []string) bool {
	for _, value := range left {
		if metadataContainsString(right, value) {
			return true
		}
	}
	return false
}

func (r *Runtime) canonicalAccessRecord(objectID string) canonicalAccessRecord {
	if r == nil || r.storage == nil || r.storage.Objects() == nil {
		return canonicalAccessRecord{}
	}
	objects := r.storage.Objects()
	if memory, ok := r.peekCanonicalMemory(objectID); ok {
		return memoryAccessRecord(memory)
	}
	if state, ok := objects.GetState(objectID); ok {
		access := state.Access
		if access.OwnerAgentID == "" {
			access.OwnerAgentID = state.AgentID
		}
		if access.TenantID == "" {
			access.TenantID = state.TenantID
		}
		if access.WorkspaceID == "" {
			access.WorkspaceID = state.WorkspaceID
		}
		if access.SessionID == "" {
			access.SessionID = state.SessionID
		}
		if access.Visibility == "" {
			access.Visibility = string(schemas.VisibilitySession)
		}
		return canonicalAccessRecord{objectID, state.AgentID, access, state.MutationLSN, true}
	}
	if artifact, ok := objects.GetArtifact(objectID); ok {
		access := artifact.Access
		if access.OwnerAgentID == "" {
			access.OwnerAgentID = artifact.OwnerAgentID
		}
		if access.TenantID == "" {
			access.TenantID = artifact.TenantID
		}
		if access.WorkspaceID == "" {
			access.WorkspaceID = artifact.WorkspaceID
		}
		if access.SessionID == "" {
			access.SessionID = artifact.SessionID
		}
		if access.Visibility == "" {
			access.Visibility = string(schemas.VisibilitySession)
		}
		return canonicalAccessRecord{objectID, artifact.OwnerAgentID, access, artifact.MutationLSN, true}
	}
	if event, ok := objects.GetEvent(objectID); ok {
		event = event.NormalizeDynamicEventV04()
		return canonicalAccessRecord{
			objectID:     objectID,
			ownerAgentID: event.Actor.AgentID,
			access:       schemas.CanonicalAccessFromEvent(event),
			mutationLSN:  event.Time.WalLSN,
			exists:       true,
		}
	}
	if edge, ok := r.storage.Edges().GetEdge(objectID); ok {
		return canonicalAccessRecord{objectID, edge.Access.OwnerAgentID, edge.Access, edge.MutationLSN, true}
	}
	return canonicalAccessRecord{}
}

func (r *Runtime) peekCanonicalMemory(objectID string) (schemas.Memory, bool) {
	if r.tieredObjects != nil {
		return r.tieredObjects.PeekMemory(objectID)
	}
	return r.storage.Objects().GetMemory(objectID)
}

func memoryAccessRecord(memory schemas.Memory) canonicalAccessRecord {
	access := memory.Access
	if access.OwnerAgentID == "" {
		access.OwnerAgentID = memory.AgentID
	}
	if access.TenantID == "" {
		access.TenantID = memory.TenantID
	}
	if access.WorkspaceID == "" {
		access.WorkspaceID = memory.WorkspaceID
	}
	if access.SessionID == "" {
		access.SessionID = memory.SessionID
	}
	if access.Visibility == "" {
		applyLegacyMemoryScope(&access, memory.Scope)
	}
	ownerAgentID := access.OwnerAgentID
	if ownerAgentID == "" {
		ownerAgentID = memory.AgentID
	}
	return canonicalAccessRecord{memory.MemoryID, ownerAgentID, access, memory.MutationLSN, true}
}

func applyLegacyMemoryScope(access *schemas.CanonicalAccess, scope string) {
	scope = strings.TrimSpace(scope)
	switch scope {
	case string(schemas.MemoryScopePrivateUser), string(schemas.MemoryScopePrivateAgent):
		access.Visibility = string(schemas.VisibilityPrivate)
	case string(schemas.MemoryScopeSessionLocal):
		access.Visibility = string(schemas.VisibilitySession)
	case string(schemas.MemoryScopeTeamShared):
		access.Visibility = string(schemas.VisibilityTeam)
	case string(schemas.MemoryScopeGlobalShared):
		access.Visibility = string(schemas.VisibilityPublic)
	case string(schemas.MemoryScopeRestrictedShared):
		access.Visibility = string(schemas.MemoryScopeRestrictedShared)
	case string(schemas.MemoryScopeWorkspaceShared):
		access.Visibility = string(schemas.VisibilityWorkspace)
	default:
		if scope != "" {
			access.WorkspaceID = scope
			access.Visibility = string(schemas.VisibilityWorkspace)
		} else if access.SessionID != "" {
			access.Visibility = string(schemas.VisibilitySession)
		} else {
			access.Visibility = string(schemas.VisibilityPrivate)
		}
	}
}

func (r *Runtime) filterResponseEvidenceByAccess(
	req schemas.QueryRequest,
	readWatermarkLSN int64,
	resp *schemas.QueryResponse,
) {
	if resp == nil {
		return
	}
	principal := r.accessPrincipal(req)
	contracts := r.storage.Contracts().ListContracts()
	if r.capabilities.GovernanceProfile == "no_share_contract" || r.capabilities.GovernanceProfile == "metadata_only" {
		contracts = nil
	}
	allowedReference := func(objectID string) bool {
		if strings.TrimSpace(objectID) == "" {
			return true
		}
		record := r.canonicalAccessRecord(objectID)
		if !record.exists {
			return true
		}
		_, allowed := r.objectAccessDecision(req, principal, contracts, objectID, readWatermarkLSN)
		return allowed
	}

	nodes := resp.Nodes[:0]
	for _, node := range resp.Nodes {
		if allowedReference(node.ObjectID) {
			nodes = append(nodes, node)
		}
	}
	resp.Nodes = nodes

	edges := resp.Edges[:0]
	for _, edge := range resp.Edges {
		if allowedReference(edge.EdgeID) && allowedReference(edge.SrcObjectID) && allowedReference(edge.DstObjectID) {
			edges = append(edges, edge)
		}
	}
	resp.Edges = edges

	proof := resp.ProofTrace[:0]
	for _, step := range resp.ProofTrace {
		if allowedReference(step.SourceID) && allowedReference(step.TargetID) {
			proof = append(proof, step)
		}
	}
	for _, decision := range resp.AccessDecisions {
		proof = append(proof, schemas.ProofStep{
			StepType:    "access",
			Operation:   "access_allowed",
			TargetID:    decision.ObjectID,
			Description: "access:" + decision.Reason,
		})
	}
	resp.ProofTrace = proof

	provenance := resp.Provenance[:0]
	for _, objectID := range resp.Provenance {
		if allowedReference(objectID) {
			provenance = append(provenance, objectID)
		}
	}
	resp.Provenance = provenance
}
