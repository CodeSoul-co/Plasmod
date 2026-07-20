package materialization

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"plasmod/src/internal/dataplane"
	"plasmod/src/internal/schemas"
)

// MaterializationResult is the full output of a single event materialization.
// It bundles:
//   - IngestRecord  — the retrieval-plane projection (goes to TieredDataPlane)
//   - Memory        — the canonical Memory object derived from the event
//   - Version       — the ObjectVersion record for the new Memory
//   - Edges         — any typed edges inferred from the event (causal, session, agent)
//   - State / StateVersion — optional session checkpoint derived from the ingest (week-2 path)
//   - Artifact / ArtifactVersion — optional attachment record when payload carries a URI
//
// The Runtime writes Memory + Version + Edges to their respective stores so
// the canonical object layer stays consistent with what the retrieval plane sees.
type MaterializationResult struct {
	Record          dataplane.IngestRecord
	Memory          schemas.Memory
	Version         schemas.ObjectVersion
	Edges           []schemas.Edge
	State           *schemas.State
	StateVersion    *schemas.ObjectVersion
	Artifact        *schemas.Artifact
	ArtifactVersion *schemas.ObjectVersion
}

// Service converts events into canonical object mutations and retrieval-ready
// projection records.  In v1 the derivation logic is intentionally lightweight,
// but the module boundary is stable and the output shape is production-aligned.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

// MaterializeEvent is the primary ingest-time entry-point. It returns the Event
// derivatives needed for one canonical projection: Memory, checkpoint State,
// optional Artifact, relations, versions, and the retrieval record.
func (s *Service) MaterializeEvent(ev schemas.Event) MaterializationResult {
	ev = ev.NormalizeDynamicEventV04()
	text := extractText(ev)
	retrievalNamespace := resolveNamespace(ev)
	memoryScope := resolveMemoryScope(ev, retrievalNamespace)
	memoryID := schemas.CanonicalMemoryID(ev)
	now := time.Now().UTC().Format(time.RFC3339)
	access := schemas.CanonicalAccessFromEvent(ev)

	record := dataplane.IngestRecord{
		ObjectID:        memoryID,
		Text:            text,
		Namespace:       retrievalNamespace,
		Attributes:      buildAttributes(ev),
		EventUnixTS:     parseEventUnixTS(ev),
		Embedding:       ev.Retrieval.EmbeddingVector,
		SkipVectorIndex: skipVectorIndex(ev),
	}

	mem := schemas.Memory{
		MemoryID:       memoryID,
		MemoryType:     resolveMemoryType(ev),
		TenantID:       ev.Identity.TenantID,
		WorkspaceID:    ev.Identity.WorkspaceID,
		AgentID:        ev.Actor.AgentID,
		SessionID:      ev.Actor.SessionID,
		Scope:          memoryScope,
		Level:          0,
		Content:        text,
		Summary:        text,
		SourceEventIDs: []string{ev.Identity.EventID},
		Confidence:     resolveConfidence(ev),
		Importance:     resolveImportance(ev),
		FreshnessScore: 1.0,
		ValidFrom:      now,
		ProvenanceRef:  ev.Identity.EventID,
		Version:        ev.Time.LogicalTS,
		MutationLSN:    ev.Time.WalLSN,
		MaterializedAt: now,
		Access:         access,
		IsActive:       true,
	}
	if ev.Access.TTLMS != nil && *ev.Access.TTLMS > 0 {
		mem.TTL = (*ev.Access.TTLMS + 999) / 1000
	}
	if ev.Payload != nil {
		if d, ok := ev.Payload[schemas.PayloadKeyDataset]; ok {
			if s, ok := d.(string); ok {
				mem.DatasetName = strings.TrimSpace(s)
			}
		}
		if f, ok := ev.Payload[schemas.PayloadKeyFileName]; ok {
			if s, ok := f.(string); ok {
				mem.SourceFileName = strings.TrimSpace(s)
			}
		}
		if b, ok := ev.Payload[schemas.PayloadKeyImportBatchID]; ok {
			if s, ok := b.(string); ok {
				mem.ImportBatchID = strings.TrimSpace(s)
			}
		}
	}
	if len(ev.Access.PolicyTags) > 0 {
		mem.PolicyTags = append([]string(nil), ev.Access.PolicyTags...)
	}
	if ev.Object.LifecycleState != "" {
		mem.LifecycleState = ev.Object.LifecycleState
	}
	if ev.Retrieval.EmbeddingRef != "" {
		mem.EmbeddingRef = ev.Retrieval.EmbeddingRef
	}

	version := schemas.ObjectVersion{
		ObjectID:        memoryID,
		ObjectType:      string(schemas.ObjectTypeMemory),
		Version:         ev.Time.LogicalTS,
		MutationEventID: ev.Identity.EventID,
		ValidFrom:       now,
		SnapshotTag:     fmt.Sprintf("ingest:%s", ev.EventInfo.EventType),
		MutationLSN:     ev.Time.WalLSN,
		Access:          access,
	}
	version.Snapshot = snapshotMap(mem)

	st, stVer := deriveStateAndVersion(ev, memoryID, now)
	art, artVer := deriveArtifactAndVersion(ev, now)
	edges := deriveEdges(ev, memoryID, st, art)

	return MaterializationResult{
		Record:          record,
		Memory:          mem,
		Version:         version,
		Edges:           edges,
		State:           st,
		StateVersion:    stVer,
		Artifact:        art,
		ArtifactVersion: artVer,
	}
}

func skipVectorIndex(ev schemas.Event) bool {
	if envEnabled("PLASMOD_SKIP_VECTOR_INDEX") {
		return true
	}
	if len(ev.Retrieval.EmbeddingVector) > 0 || len(ev.EmbeddingVector) > 0 {
		return false
	}
	return strings.TrimSpace(ev.Retrieval.IndexText) != "" && !ev.Retrieval.HasEmbedding
}

func envEnabled(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || strings.EqualFold(value, "on")
}

// ProjectEvent is kept for backward-compatibility.  New code should call
// MaterializeEvent and use MaterializationResult.Record.
func (s *Service) ProjectEvent(ev schemas.Event) dataplane.IngestRecord {
	return s.MaterializeEvent(ev).Record
}

func extractText(ev schemas.Event) string {
	return ev.Text()
}

func resolveConfidence(ev schemas.Event) float64 {
	if ev.EventInfo.Confidence != nil {
		return *ev.EventInfo.Confidence
	}
	return 1.0
}

func resolveImportance(ev schemas.Event) float64 {
	if ev.EventInfo.Importance != nil {
		return *ev.EventInfo.Importance
	}
	return 0
}

func resolveNamespace(ev schemas.Event) string {
	return ev.RetrievalNamespaceOrDefault()
}

func resolveMemoryScope(ev schemas.Event, fallback string) string {
	ev = ev.NormalizeDynamicEventV04()
	if ev.Identity.WorkspaceID != "" {
		return ev.Identity.WorkspaceID
	}
	return fallback
}

func resolveMemoryType(ev schemas.Event) string {
	if ev.Payload != nil {
		if raw, ok := ev.Payload["memory_type"].(string); ok && strings.TrimSpace(raw) != "" {
			return strings.TrimSpace(raw)
		}
	}
	switch ev.EventInfo.EventType {
	case string(schemas.EventTypeUserMessage), string(schemas.EventTypeAssistantMessage):
		return string(schemas.MemoryTypeEpisodic)
	case string(schemas.EventTypeCritiqueGenerated), string(schemas.EventTypeReflection):
		return string(schemas.MemoryTypeReflective)
	case string(schemas.EventTypePlanUpdated):
		return string(schemas.MemoryTypeProcedural)
	case string(schemas.EventTypeToolResultReturned):
		return string(schemas.MemoryTypeFactual)
	default:
		return string(schemas.MemoryTypeEpisodic)
	}
}

// deriveEdges infers typed edges from the event relationships.
// In v1 we emit:
//   - memory → session  ("belongs_to_session")
//   - memory → agent    ("owned_by_agent")
//   - memory → causal   ("derived_from")  for each causal ref
func deriveEdges(ev schemas.Event, memoryID string, st *schemas.State, art *schemas.Artifact) []schemas.Edge {
	now := time.Now().UTC().Format(time.RFC3339)
	edges := []schemas.Edge{}

	edges = append(edges, schemas.Edge{
		EdgeID:        schemas.IDPrefixEdge + memoryID + "_event",
		SrcObjectID:   memoryID,
		SrcType:       string(schemas.ObjectTypeMemory),
		EdgeType:      string(schemas.EdgeTypeCausedBy),
		DstObjectID:   ev.Identity.EventID,
		DstType:       string(schemas.ObjectTypeEvent),
		Weight:        schemas.DefaultEdgeWeight,
		ProvenanceRef: ev.Identity.EventID,
		CreatedTS:     now,
	})

	if ev.Actor.SessionID != "" {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", schemas.IDPrefixEdge, memoryID, schemas.EdgeTypeBelongsToSession, ev.Actor.SessionID),
			SrcObjectID:   memoryID,
			SrcType:       string(schemas.ObjectTypeMemory),
			EdgeType:      string(schemas.EdgeTypeBelongsToSession),
			DstObjectID:   ev.Actor.SessionID,
			DstType:       string(schemas.ObjectTypeSession),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
	}
	if ev.Actor.AgentID != "" {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", schemas.IDPrefixEdge, memoryID, schemas.EdgeTypeOwnedByAgent, ev.Actor.AgentID),
			SrcObjectID:   memoryID,
			SrcType:       string(schemas.ObjectTypeMemory),
			EdgeType:      string(schemas.EdgeTypeOwnedByAgent),
			DstObjectID:   ev.Actor.AgentID,
			DstType:       string(schemas.ObjectTypeAgent),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
	}
	for i, ref := range ev.Causality.CausalRefs {
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("%s%s_causal_%d", schemas.IDPrefixEdge, memoryID, i),
			SrcObjectID:   memoryID,
			SrcType:       string(schemas.ObjectTypeMemory),
			EdgeType:      string(schemas.EdgeTypeDerivedFrom),
			DstObjectID:   ref,
			DstType:       string(schemas.ObjectTypeEvent),
			Weight:        schemas.DefaultCausalWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
	}
	if ev.Causality.SourceObjectID != "" && ev.Causality.TargetObjectID != "" && ev.EdgeKind() != "" {
		weight := schemas.DefaultEdgeWeight
		if ev.Causality.EdgeWeight != nil {
			weight = *ev.Causality.EdgeWeight
		}
		edges = append(edges, schemas.Edge{
			EdgeID:        fmt.Sprintf("%s%s_%s_%s", schemas.IDPrefixEdge, ev.Causality.SourceObjectID, ev.EdgeKind(), ev.Causality.TargetObjectID),
			SrcObjectID:   ev.Causality.SourceObjectID,
			SrcType:       string(schemas.ObjectTypeMemory),
			EdgeType:      ev.EdgeKind(),
			DstObjectID:   ev.Causality.TargetObjectID,
			DstType:       string(schemas.ObjectTypeMemory),
			Weight:        weight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
			Properties: map[string]any{
				"reason":          ev.Causality.Reason,
				"provenance_refs": ev.Causality.ProvenanceRefs,
			},
		})
	}
	if st != nil {
		edges = append(edges, schemas.Edge{
			EdgeID:        schemas.IDPrefixEdge + st.StateID + "_event",
			SrcObjectID:   st.StateID,
			SrcType:       string(schemas.ObjectTypeAgentState),
			EdgeType:      string(schemas.EdgeTypeDerivedFrom),
			DstObjectID:   ev.Identity.EventID,
			DstType:       string(schemas.ObjectTypeEvent),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
		edges = append(edges, schemas.Edge{
			EdgeID:        schemas.IDPrefixEdge + st.StateID + "_memory",
			SrcObjectID:   st.StateID,
			SrcType:       string(schemas.ObjectTypeAgentState),
			EdgeType:      string(schemas.EdgeTypeProjectedFrom),
			DstObjectID:   memoryID,
			DstType:       string(schemas.ObjectTypeMemory),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
	}
	if art != nil {
		edges = append(edges, schemas.Edge{
			EdgeID:        schemas.IDPrefixEdge + art.ArtifactID + "_event",
			SrcObjectID:   art.ArtifactID,
			SrcType:       string(schemas.ObjectTypeArtifact),
			EdgeType:      string(schemas.EdgeTypeCreatedBy),
			DstObjectID:   ev.Identity.EventID,
			DstType:       string(schemas.ObjectTypeEvent),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
		edges = append(edges, schemas.Edge{
			EdgeID:        schemas.IDPrefixEdge + memoryID + "_artifact",
			SrcObjectID:   memoryID,
			SrcType:       string(schemas.ObjectTypeMemory),
			EdgeType:      string(schemas.EdgeTypeGroundedOnResource),
			DstObjectID:   art.ArtifactID,
			DstType:       string(schemas.ObjectTypeArtifact),
			Weight:        schemas.DefaultEdgeWeight,
			ProvenanceRef: ev.Identity.EventID,
			CreatedTS:     now,
		})
	}
	access := schemas.CanonicalAccessFromEvent(ev)
	for i := range edges {
		edges[i].MutationLSN = ev.Time.WalLSN
		edges[i].Access = access
	}
	return edges
}

func buildAttributes(ev schemas.Event) map[string]string {
	attrs := map[string]string{
		"tenant_id":     ev.Identity.TenantID,
		"workspace_id":  ev.Identity.WorkspaceID,
		"agent_id":      ev.Actor.AgentID,
		"session_id":    ev.Actor.SessionID,
		"event_type":    ev.EventInfo.EventType,
		"event_subtype": ev.EventInfo.EventSubtype,
		"action":        ev.EventInfo.Action,
		// Retrieval path constrains by object/memory type for /v1/internal/memory/recall.
		// Persist these attributes at ingest time so type filtering can match.
		"object_type":               string(schemas.ObjectTypeMemory),
		"memory_type":               resolveMemoryType(ev),
		"visibility":                ev.Access.Visibility,
		"access_consistency":        ev.Access.Consistency,
		"access_visibility":         ev.Access.Visibility,
		"retrieval_namespace":       ev.Retrieval.RetrievalNamespace,
		"materialization_status":    ev.Materialization.Status,
		"runtime_write_status":      ev.Runtime.WriteStatus,
		"runtime_visibility_status": ev.Runtime.VisibilityStatus,
	}
	if ev.Identity.Dataset != "" {
		attrs["dataset"] = ev.Identity.Dataset
	}
	if ev.Identity.ImportBatchID != "" {
		attrs["import_batch_id"] = ev.Identity.ImportBatchID
	}
	if ev.Identity.FileName != "" {
		attrs["file_name"] = ev.Identity.FileName
	}
	if ev.Data.PayloadSizeBytes > 0 {
		attrs["payload_size_bytes"] = fmt.Sprintf("%d", ev.Data.PayloadSizeBytes)
	}
	if ev.Data.PayloadHash != "" {
		attrs["payload_hash"] = ev.Data.PayloadHash
	}
	addAttributeList(attrs, "hook_materializers", ev.MaterializerHooks())
	addAttributeList(attrs, "hook_indexers", ev.IndexerHooks())
	addAttributeList(attrs, "hook_query_ops", ev.QueryOpHooks())
	addAttributeList(attrs, "hook_policy", ev.PolicyHooks())
	addAttributeList(attrs, "hook_evidence", ev.EvidenceHooks())
	addAttributeList(attrs, "hook_chains", ev.ChainHooks())
	addAttributeList(attrs, "hook_custom", ev.CustomHooks())
	addAttributeList(attrs, "visible_to_agents", ev.Access.VisibleToAgents)
	addAttributeList(attrs, "visible_to_roles", ev.Access.VisibleToRoles)
	addAttributeList(attrs, "policy_tags", ev.Access.PolicyTags)
	if ev.Access.ShareContractID != "" {
		attrs["share_contract_id"] = ev.Access.ShareContractID
	}
	return attrs
}

func addAttributeList(attrs map[string]string, key string, values []string) {
	if len(values) == 0 {
		return
	}
	attrs[key] = strings.Join(values, ",")
}

func parseEventUnixTS(ev schemas.Event) int64 {
	if ev.Time.EventTime > 0 {
		return ev.Time.EventTime / 1000
	}
	if ev.Time.IngestTime > 0 {
		return ev.Time.IngestTime / 1000
	}
	return time.Now().Unix()
}

// deriveStateAndVersion creates one stable state key per canonical scope. State
// updates use the key/value supplied by the Event; all other Events advance the
// session's last_memory_id checkpoint. Runtime assigns the final monotonic
// version after reading the current canonical State.
func deriveStateAndVersion(ev schemas.Event, memoryID, nowRFC3339 string) (*schemas.State, *schemas.ObjectVersion) {
	stateKey := strings.TrimSpace(ev.StateKey())
	stateValue := ev.StateValueString()
	stateType := ev.EventInfo.EventType
	if stateKey == "" {
		stateKey = "last_memory_id"
		stateValue = memoryID
		stateType = "ingest_checkpoint"
	}
	stateID := schemas.CanonicalStateID(
		ev.Identity.TenantID,
		ev.Identity.WorkspaceID,
		ev.Actor.AgentID,
		ev.Actor.SessionID,
		stateKey,
	)
	access := schemas.CanonicalAccessFromEvent(ev)
	st := &schemas.State{
		StateID:            stateID,
		TenantID:           ev.Identity.TenantID,
		WorkspaceID:        ev.Identity.WorkspaceID,
		AgentID:            ev.Actor.AgentID,
		SessionID:          ev.Actor.SessionID,
		StateType:          stateType,
		StateKey:           stateKey,
		StateValue:         stateValue,
		DerivedFromEventID: ev.Identity.EventID,
		CheckpointTS:       nowRFC3339,
		Version:            1,
		MutationLSN:        ev.Time.WalLSN,
		Access:             access,
	}
	ver := &schemas.ObjectVersion{
		ObjectID:        stateID,
		ObjectType:      string(schemas.ObjectTypeAgentState),
		Version:         1,
		MutationEventID: ev.Identity.EventID,
		ValidFrom:       nowRFC3339,
		SnapshotTag:     fmt.Sprintf("ingest:%s", ev.EventInfo.EventType),
		MutationLSN:     ev.Time.WalLSN,
		Snapshot:        snapshotMap(*st),
		Access:          access,
	}
	return st, ver
}

// deriveArtifactAndVersion returns an Artifact when the payload declares an
// external URI (week-2 minimal hook). Optional keys: "artifact_uri", nested
// "artifact" map with "uri", or event_type artifact_attached / tool_result_returned with uri.
func deriveArtifactAndVersion(ev schemas.Event, nowRFC3339 string) (*schemas.Artifact, *schemas.ObjectVersion) {
	uri := ev.ArtifactURI()
	isToolEvent := ev.EventInfo.EventType == string(schemas.EventTypeToolCall) ||
		ev.EventInfo.EventType == string(schemas.EventTypeToolResult)
	if uri == "" && !ev.IsArtifactLike() && !isToolEvent {
		return nil, nil
	}
	artID := ev.ArtifactIDOrDefault()
	mime := ev.ArtifactMimeType()
	if mime == "" && uri == "" {
		mime = "text/plain"
	}
	art := &schemas.Artifact{
		ArtifactID:        artID,
		TenantID:          ev.Identity.TenantID,
		WorkspaceID:       ev.Identity.WorkspaceID,
		SessionID:         ev.Actor.SessionID,
		OwnerAgentID:      ev.Actor.AgentID,
		ArtifactType:      firstNonEmpty(ev.Object.ObjectSubtype, ev.EventInfo.EventType, "artifact"),
		URI:               uri,
		MimeType:          mime,
		ProducedByEventID: ev.Identity.EventID,
		Version:           ev.Time.LogicalTS,
		MutationLSN:       ev.Time.WalLSN,
		MaterializedAt:    nowRFC3339,
		Access:            schemas.CanonicalAccessFromEvent(ev),
	}
	if name := ev.ArtifactName(); name != "" {
		if art.Metadata == nil {
			art.Metadata = map[string]any{}
		}
		art.Metadata["name"] = name
	}
	if body := ev.ArtifactBodyString(); body != "" {
		if art.Metadata == nil {
			art.Metadata = map[string]any{}
		}
		art.Metadata["body"] = body
		art.ContentRef = "inline"
	}
	ver := &schemas.ObjectVersion{
		ObjectID:        artID,
		ObjectType:      "artifact",
		Version:         ev.Time.LogicalTS,
		MutationEventID: ev.Identity.EventID,
		ValidFrom:       nowRFC3339,
		SnapshotTag:     fmt.Sprintf("ingest:%s", ev.EventInfo.EventType),
		MutationLSN:     ev.Time.WalLSN,
		Snapshot:        snapshotMap(*art),
		Access:          schemas.CanonicalAccessFromEvent(ev),
	}
	return art, ver
}

func snapshotMap(value any) map[string]any {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil
	}
	return snapshot
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
