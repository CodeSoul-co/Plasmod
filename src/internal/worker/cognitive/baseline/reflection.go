package baseline

import (
	"fmt"
	"time"

	"andb/src/internal/eventbackbone"
	"andb/src/internal/schemas"
	"andb/src/internal/storage"
	"andb/src/internal/worker/nodes"
)

// InMemoryReflectionPolicyWorker applies governance rules to canonical memory
// objects: TTL expiry, quarantine, confidence override, salience decay.
// This is the baseline algorithm's reflection/governance pipeline step.
// An optional AuditStore (set via WithAuditStore) also receives AuditRecords.
// tieredObjs (set via WithTieredObjects) is used to archive memories to cold
// storage when they are deactivated (quarantined or TTL-expired), keeping the
// hot/warm/cold tiers in sync.
type InMemoryReflectionPolicyWorker struct {
	id          string
	objStore    storage.ObjectStore
	polStore    storage.PolicyStore
	policyLog   eventbackbone.PolicyDecisionLogger
	auditStore  storage.AuditStore
	tieredObjs  *storage.TieredObjectStore
}

func CreateInMemoryReflectionPolicyWorker(
	id string,
	objStore storage.ObjectStore,
	polStore storage.PolicyStore,
	policyLog eventbackbone.PolicyDecisionLogger,
) *InMemoryReflectionPolicyWorker {
	return &InMemoryReflectionPolicyWorker{
		id:        id,
		objStore:  objStore,
		polStore:  polStore,
		policyLog: policyLog,
	}
}

// WithAuditStore wires an AuditStore so governance decisions are persisted as
// AuditRecords in addition to the PolicyDecisionLog.
func (w *InMemoryReflectionPolicyWorker) WithAuditStore(a storage.AuditStore) *InMemoryReflectionPolicyWorker {
	w.auditStore = a
	return w
}

// WithTieredObjects wires a TieredObjectStore so that deactivated memories
// (quarantined or TTL-expired) are archived to cold storage rather than
// lingering in the hot/warm tiers.
func (w *InMemoryReflectionPolicyWorker) WithTieredObjects(t *storage.TieredObjectStore) *InMemoryReflectionPolicyWorker {
	w.tieredObjs = t
	return w
}

func (w *InMemoryReflectionPolicyWorker) Run(input schemas.WorkerInput) (schemas.WorkerOutput, error) {
	in, ok := input.(schemas.ReflectionPolicyInput)
	if !ok {
		return schemas.ReflectionPolicyOutput{}, fmt.Errorf("reflection: unexpected input type %T", input)
	}
	if in.ObjectType != "memory" {
		return schemas.ReflectionPolicyOutput{}, w.Reflect(in.ObjectID, in.ObjectType)
	}
	before, exists := w.objStore.GetMemory(in.ObjectID)
	err := w.Reflect(in.ObjectID, in.ObjectType)
	if err != nil || !exists {
		return schemas.ReflectionPolicyOutput{}, err
	}
	after, _ := w.objStore.GetMemory(in.ObjectID)
	var rules []string
	if before.IsActive && !after.IsActive {
		rules = append(rules, "quarantined_or_ttl_expired")
	}
	if before.Confidence != after.Confidence {
		rules = append(rules, "confidence_overridden")
	}
	if before.Importance != after.Importance {
		rules = append(rules, "salience_decayed")
	}
	return schemas.ReflectionPolicyOutput{Modified: len(rules) > 0, AppliedRules: rules}, nil
}

func (w *InMemoryReflectionPolicyWorker) Info() nodes.NodeInfo {
	return nodes.NodeInfo{
		ID:           w.id,
		Type:         nodes.NodeTypeReflectionPolicy,
		State:        nodes.NodeStateReady,
		Capabilities: []string{"ttl_decay", "quarantine", "confidence_override", "salience_decay", "policy_audit"},
	}
}

func (w *InMemoryReflectionPolicyWorker) Reflect(objectID, objectType string) error {
	if objectType != "memory" {
		return nil
	}
	mem, ok := w.objStore.GetMemory(objectID)
	if !ok {
		return nil
	}
	policies := w.polStore.GetPolicies(objectID)
	if len(policies) == 0 {
		return nil
	}
	modified := false
	for _, p := range policies {
		if p.QuarantineFlag && mem.IsActive {
			mem.IsActive = false
			mem.LifecycleState = string(schemas.MemoryLifecycleQuarantined)
			modified = true
			w.policyLog.Append(objectID, objectType, p.PolicyID, "quarantined", p.PolicyReason)
			w.appendAudit(objectID, p.PolicyID, "quarantined", p.PolicyReason)
			continue
		}
		if p.TTL > 0 && mem.IsActive && mem.ValidFrom != "" {
			if created, err := time.Parse(time.RFC3339, mem.ValidFrom); err == nil {
				if time.Since(created) > time.Duration(p.TTL)*time.Second {
					mem.IsActive = false
					mem.LifecycleState = string(schemas.MemoryLifecycleDecayed)
					modified = true
					w.policyLog.Append(objectID, objectType, p.PolicyID, "ttl_expired", "lifetime exceeded")
					w.appendAudit(objectID, p.PolicyID, "ttl_expired", "lifetime exceeded")
				}
			}
		}
		if p.ConfidenceOverride > 0 && p.ConfidenceOverride != mem.Confidence {
			mem.Confidence = p.ConfidenceOverride
			modified = true
			w.policyLog.Append(objectID, objectType, p.PolicyID, "confidence_overridden", p.PolicyReason)
		}
		if p.SalienceWeight > 0 && p.SalienceWeight < 1.0 {
			mem.Importance *= p.SalienceWeight
			modified = true
			decayDesc := p.DecayFn
			if decayDesc == "" {
				decayDesc = "multiplicative"
			}
			w.policyLog.Append(objectID, objectType, p.PolicyID, "salience_decayed", decayDesc)
		}
	}
	if modified {
		w.objStore.PutMemory(mem)
		// Archive to cold tier if the memory was deactivated (quarantined or
		// TTL-expired), so the hot/warm tiers do not serve stale objects and the
		// cold store holds the authoritative historical record.
		if w.tieredObjs != nil && !mem.IsActive {
			w.tieredObjs.ArchiveMemory(mem.MemoryID)
		}
	}
	return nil
}

func (w *InMemoryReflectionPolicyWorker) appendAudit(memoryID, policyID, decision, reason string) {
	if w.auditStore == nil {
		return
	}
	w.auditStore.AppendAudit(schemas.AuditRecord{
		RecordID:         fmt.Sprintf("audit_reflect_%s_%d", memoryID, time.Now().UnixNano()),
		TargetMemoryID:   memoryID,
		OperationType:    string(schemas.AuditOpPolicyChange),
		ActorType:        "system",
		ActorID:          "reflection_policy_worker",
		PolicySnapshotID: policyID,
		Decision:         decision,
		ReasonCode:       reason,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
	})
}
