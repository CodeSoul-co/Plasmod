package schemas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const DynamicEventSchemaV04 = "plasmod.dynamic_event.v0.4"

type EventIdentity struct {
	TraceID       string `json:"trace_id,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	TenantID      string `json:"tenant_id,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	Source        string `json:"source,omitempty"`
	Dataset       string `json:"dataset,omitempty"`
	ImportBatchID string `json:"import_batch_id,omitempty"`
	IngestMode    string `json:"ingest_mode,omitempty"`
	FileName      string `json:"file_name,omitempty"`
	ReplayOrder   int64  `json:"replay_order,omitempty"`
}

type EventActor struct {
	SessionID       string `json:"session_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	RoleProfile     string `json:"role_profile,omitempty"`
	TeamID          string `json:"team_id,omitempty"`
	ParentAgentID   string `json:"parent_agent_id,omitempty"`
	AgentGeneration *int   `json:"agent_generation,omitempty"`
	AgentType       string `json:"agent_type,omitempty"`
}

type EventTime struct {
	EventTime   int64 `json:"event_time,omitempty"`
	LogicalTS   int64 `json:"logical_ts,omitempty"`
	WalLSN      int64 `json:"wal_lsn,omitempty"`
	IngestTime  int64 `json:"ingest_time,omitempty"`
	VisibleTime int64 `json:"visible_time,omitempty"`
}

type EventDescriptor struct {
	EventType    string   `json:"event_type,omitempty"`
	EventSubtype string   `json:"event_subtype,omitempty"`
	Action       string   `json:"action,omitempty"`
	Importance   *float64 `json:"importance,omitempty"`
	Confidence   *float64 `json:"confidence,omitempty"`
}

type EventObject struct {
	ObjectID       string `json:"object_id,omitempty"`
	ObjectType     string `json:"object_type,omitempty"`
	ObjectSubtype  string `json:"object_subtype,omitempty"`
	Version        any    `json:"version,omitempty"`
	LifecycleState string `json:"lifecycle_state,omitempty"`
	StateType      string `json:"state_type,omitempty"`
	StateKey       string `json:"state_key,omitempty"`
	ArtifactName   string `json:"artifact_name,omitempty"`
	ArtifactURI    string `json:"artifact_uri,omitempty"`
	URI            string `json:"uri,omitempty"`
	MimeType       string `json:"mime_type,omitempty"`
}

type EventCausality struct {
	ParentEventID   string     `json:"parent_event_id,omitempty"`
	CausalRefs      []string   `json:"causal_refs,omitempty"`
	ProvenanceRefs  []string   `json:"provenance_refs,omitempty"`
	CallEventID     string     `json:"call_event_id,omitempty"`
	SourceObjectID  string     `json:"source_object_id,omitempty"`
	TargetObjectID  string     `json:"target_object_id,omitempty"`
	SourceObjectIDs []string   `json:"source_object_ids,omitempty"`
	TargetObjectIDs []string   `json:"target_object_ids,omitempty"`
	EdgeKind        string     `json:"edge_kind,omitempty"`
	EdgeWeight      *float64   `json:"edge_weight,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	Hooks           EventHooks `json:"hooks,omitempty"`
}

type EventAccess struct {
	Consistency     string     `json:"consistency,omitempty"`
	Visibility      string     `json:"visibility,omitempty"`
	VisibleToAgents []string   `json:"visible_to_agents,omitempty"`
	VisibleToRoles  []string   `json:"visible_to_roles,omitempty"`
	TTLMS           *int64     `json:"ttl_ms,omitempty"`
	FreshnessSLAMS  *int64     `json:"freshness_sla_ms,omitempty"`
	PolicyTags      []string   `json:"policy_tags,omitempty"`
	ShareContractID string     `json:"share_contract_id,omitempty"`
	Hooks           EventHooks `json:"hooks,omitempty"`
}

type eventActorWire struct {
	SessionID       string `json:"session_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	RoleProfile     string `json:"role_profile,omitempty"`
	TeamID          string `json:"team_id,omitempty"`
	ParentAgentID   string `json:"parent_agent_id,omitempty"`
	AgentGeneration *int   `json:"agent_generation,omitempty"`
	AgentType       string `json:"agent_type,omitempty"`
	AgentKind       string `json:"agent_kind,omitempty"`
}

func (a *EventActor) UnmarshalJSON(data []byte) error {
	var wire eventActorWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*a = EventActor{
		SessionID:       wire.SessionID,
		AgentID:         wire.AgentID,
		RoleProfile:     wire.RoleProfile,
		TeamID:          wire.TeamID,
		ParentAgentID:   wire.ParentAgentID,
		AgentGeneration: wire.AgentGeneration,
		AgentType:       firstString(wire.AgentType, wire.AgentKind),
	}
	return nil
}

type eventTimeWire struct {
	EventTime     int64 `json:"event_time,omitempty"`
	EventTimeMS   int64 `json:"event_time_ms,omitempty"`
	TimestampMS   int64 `json:"timestamp_ms,omitempty"`
	LogicalTS     int64 `json:"logical_ts,omitempty"`
	WalLSN        int64 `json:"wal_lsn,omitempty"`
	IngestTime    int64 `json:"ingest_time,omitempty"`
	IngestTimeMS  int64 `json:"ingest_time_ms,omitempty"`
	VisibleTime   int64 `json:"visible_time,omitempty"`
	VisibleTimeMS int64 `json:"visible_time_ms,omitempty"`
}

func (t *EventTime) UnmarshalJSON(data []byte) error {
	var wire eventTimeWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*t = EventTime{
		EventTime:   firstInt64(wire.EventTime, wire.EventTimeMS, wire.TimestampMS),
		LogicalTS:   wire.LogicalTS,
		WalLSN:      wire.WalLSN,
		IngestTime:  firstInt64(wire.IngestTime, wire.IngestTimeMS),
		VisibleTime: firstInt64(wire.VisibleTime, wire.VisibleTimeMS),
	}
	return nil
}

type eventAccessWire struct {
	Consistency     string     `json:"consistency,omitempty"`
	Visibility      string     `json:"visibility,omitempty"`
	Sharing         string     `json:"sharing,omitempty"`
	Scope           string     `json:"scope,omitempty"`
	VisibleToAgents []string   `json:"visible_to_agents,omitempty"`
	VisibleToRoles  []string   `json:"visible_to_roles,omitempty"`
	TTLMS           *int64     `json:"ttl_ms,omitempty"`
	FreshnessSLAMS  *int64     `json:"freshness_sla_ms,omitempty"`
	PolicyTags      []string   `json:"policy_tags,omitempty"`
	ShareContractID string     `json:"share_contract_id,omitempty"`
	Hooks           EventHooks `json:"hooks,omitempty"`
}

func (a *EventAccess) UnmarshalJSON(data []byte) error {
	var wire eventAccessWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*a = EventAccess{
		Consistency:     wire.Consistency,
		Visibility:      firstString(wire.Visibility, wire.Sharing, wire.Scope),
		VisibleToAgents: append([]string(nil), wire.VisibleToAgents...),
		VisibleToRoles:  append([]string(nil), wire.VisibleToRoles...),
		TTLMS:           wire.TTLMS,
		FreshnessSLAMS:  wire.FreshnessSLAMS,
		PolicyTags:      append([]string(nil), wire.PolicyTags...),
		ShareContractID: wire.ShareContractID,
		Hooks:           wire.Hooks,
	}
	return nil
}

type EventMaterialization struct {
	Enabled          *bool      `json:"enabled,omitempty"`
	Targets          []string   `json:"targets,omitempty"`
	Mode             string     `json:"mode,omitempty"`
	PlannedObjectIDs []string   `json:"planned_object_ids,omitempty"`
	Status           string     `json:"status,omitempty"`
	MaterializedAtMS *int64     `json:"materialized_at_ms,omitempty"`
	Hooks            EventHooks `json:"hooks,omitempty"`
}

type EventRetrieval struct {
	IndexText          string             `json:"index_text,omitempty"`
	HasEmbedding       bool               `json:"has_embedding,omitempty"`
	EmbeddingDim       *int               `json:"embedding_dim,omitempty"`
	EmbeddingVector    []float32          `json:"embedding_vector,omitempty"`
	EmbeddingRef       string             `json:"embedding_ref,omitempty"`
	IndexFields        []string           `json:"index_fields,omitempty"`
	RetrievalNamespace string             `json:"retrieval_namespace,omitempty"`
	SparseTerms        map[string]float64 `json:"sparse_terms,omitempty"`
	Hooks              EventHooks         `json:"hooks,omitempty"`
}

func (r *EventRetrieval) UnmarshalJSON(data []byte) error {
	type retrievalWire struct {
		IndexText          any                `json:"index_text,omitempty"`
		HasEmbedding       bool               `json:"has_embedding,omitempty"`
		EmbeddingDim       *int               `json:"embedding_dim,omitempty"`
		EmbeddingVector    []float32          `json:"embedding_vector,omitempty"`
		EmbeddingVect      []float32          `json:"embedding_vect,omitempty"`
		EmbeddingRef       string             `json:"embedding_ref,omitempty"`
		IndexFields        []string           `json:"index_fields,omitempty"`
		RetrievalNamespace string             `json:"retrieval_namespace,omitempty"`
		RetrievalNamesp    string             `json:"retrieval_namesp,omitempty"`
		SparseTerms        map[string]float64 `json:"sparse_terms,omitempty"`
		Hooks              EventHooks         `json:"hooks,omitempty"`
	}
	var wire retrievalWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*r = EventRetrieval{
		IndexText:          stringifyRetrievalIndexText(wire.IndexText),
		HasEmbedding:       wire.HasEmbedding,
		EmbeddingDim:       wire.EmbeddingDim,
		EmbeddingVector:    firstFloat32Slice(wire.EmbeddingVector, wire.EmbeddingVect),
		EmbeddingRef:       wire.EmbeddingRef,
		IndexFields:        append([]string(nil), wire.IndexFields...),
		RetrievalNamespace: firstString(wire.RetrievalNamespace, wire.RetrievalNamesp),
		SparseTerms:        wire.SparseTerms,
		Hooks:              wire.Hooks,
	}
	return nil
}

func stringifyRetrievalIndexText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"text", "body", "content", "summary"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
		if raw, err := json.Marshal(v); err == nil {
			return string(raw)
		}
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(stringifyRetrievalIndexText(item)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
	return ""
}

func firstFloat32Slice(values ...[]float32) []float32 {
	for _, value := range values {
		if len(value) > 0 {
			return append([]float32(nil), value...)
		}
	}
	return nil
}

type EventData struct {
	PayloadSizeBytes int64  `json:"payload_size_bytes,omitempty"`
	RecordSizeBytes  *int64 `json:"record_size_bytes,omitempty"`
	PayloadHash      string `json:"payload_hash,omitempty"`
	Canonicalization string `json:"canonicalization,omitempty"`
	SchemaName       string `json:"schema_name,omitempty"`
	SchemaRef        string `json:"schema_ref,omitempty"`
}

type EventRuntime struct {
	TEventCreatedMS       int64    `json:"t_event_created_ms,omitempty"`
	TWriteStartMS         *int64   `json:"t_write_start_ms,omitempty"`
	TWriteAckMS           *int64   `json:"t_write_ack_ms,omitempty"`
	TMaterializedMS       *int64   `json:"t_materialized_ms,omitempty"`
	TFirstVisibleMS       *int64   `json:"t_first_visible_ms,omitempty"`
	TQueryFirstCheckedMS  *int64   `json:"t_query_first_checked_ms,omitempty"`
	WriteLatencyMS        *float64 `json:"write_latency_ms,omitempty"`
	WriteToVisibleMS      *float64 `json:"write_to_visible_ms,omitempty"`
	MaterializationLagMS  *float64 `json:"materialization_lag_ms,omitempty"`
	WriteStatus           string   `json:"write_status,omitempty"`
	MaterializationStatus string   `json:"materialization_status,omitempty"`
	VisibilityStatus      string   `json:"visibility_status,omitempty"`
}

type EventHooks struct {
	Materializers []string `json:"materializers,omitempty"`
	Indexers      []string `json:"indexers,omitempty"`
	QueryOps      []string `json:"query_ops,omitempty"`
	Policy        []string `json:"policy,omitempty"`
	Evidence      []string `json:"evidence,omitempty"`
	Chains        []string `json:"chains,omitempty"`
	Custom        []string `json:"custom,omitempty"`
}

type EventExtensions struct {
	Custom map[string]any `json:"custom,omitempty"`
	Labels []string       `json:"labels,omitempty"`
	Hooks  EventHooks     `json:"hooks,omitempty"`
}

type eventWire struct {
	SchemaVersion   string               `json:"schema_version"`
	Identity        EventIdentity        `json:"identity"`
	Actor           EventActor           `json:"actor"`
	Time            EventTime            `json:"time"`
	Event           EventDescriptor      `json:"event"`
	Object          EventObject          `json:"object"`
	Causality       EventCausality       `json:"causality"`
	Access          EventAccess          `json:"access"`
	Materialization EventMaterialization `json:"materialization"`
	Retrieval       EventRetrieval       `json:"retrieval"`
	Payload         map[string]any       `json:"payload"`
	Data            EventData            `json:"data"`
	Runtime         EventRuntime         `json:"runtime"`
	Extensions      EventExtensions      `json:"extensions"`
}

type eventLegacyWire struct {
	EventID         string         `json:"event_id"`
	TenantID        string         `json:"tenant_id"`
	WorkspaceID     string         `json:"workspace_id"`
	AgentID         string         `json:"agent_id"`
	SessionID       string         `json:"session_id"`
	EventType       string         `json:"event_type"`
	EventTime       string         `json:"event_time"`
	IngestTime      string         `json:"ingest_time"`
	VisibleTime     string         `json:"visible_time"`
	LogicalTS       int64          `json:"logical_ts"`
	ParentEventID   string         `json:"parent_event_id"`
	CausalRefs      []string       `json:"causal_refs"`
	Payload         map[string]any `json:"payload"`
	EmbeddingVector []float32      `json:"embedding_vector"`
	Source          string         `json:"source"`
	Importance      float64        `json:"importance"`
	Visibility      string         `json:"visibility"`
	Version         int64          `json:"version"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	normalized := e.NormalizeDynamicEventV04()
	return json.Marshal(normalized.toWire())
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var wire eventWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	out := Event{
		SchemaVersion:   wire.SchemaVersion,
		Identity:        wire.Identity,
		Actor:           wire.Actor,
		Time:            wire.Time,
		EventInfo:       wire.Event,
		Object:          wire.Object,
		Causality:       wire.Causality,
		Access:          wire.Access,
		Materialization: wire.Materialization,
		Retrieval:       wire.Retrieval,
		Payload:         wire.Payload,
		Data:            wire.Data,
		Runtime:         wire.Runtime,
		Extensions:      wire.Extensions,
	}
	var legacy eventLegacyWire
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	out.applyLegacyWire(legacy)
	out = out.NormalizeDynamicEventV04()
	*e = out
	return nil
}

func (e Event) NormalizeDynamicEventV04() Event {
	e = e.cloneDynamicFields()
	if hasOnlyLegacyEventFields(e) {
		e = e.promoteLegacyToDynamic()
	}
	if e.SchemaVersion == "" {
		e.SchemaVersion = DynamicEventSchemaV04
	}
	if e.Identity.EventID != "" {
		e.EventID = e.Identity.EventID
	}
	if e.Identity.TenantID != "" {
		e.TenantID = e.Identity.TenantID
	}
	if e.Identity.WorkspaceID != "" {
		e.WorkspaceID = e.Identity.WorkspaceID
	}
	if e.Identity.Source != "" {
		e.Source = e.Identity.Source
	}
	if e.Actor.AgentID != "" {
		e.AgentID = e.Actor.AgentID
	}
	if e.Actor.SessionID != "" {
		e.SessionID = e.Actor.SessionID
	}
	if e.EventInfo.EventType != "" {
		e.EventType = e.EventInfo.EventType
	}
	if e.EventInfo.Importance != nil {
		e.Importance = *e.EventInfo.Importance
	}
	if e.Time.EventTime > 0 {
		e.EventTime = millisToRFC3339(e.Time.EventTime)
	}
	if e.Time.IngestTime > 0 {
		e.IngestTime = millisToRFC3339(e.Time.IngestTime)
	}
	if e.Time.VisibleTime > 0 {
		e.VisibleTime = millisToRFC3339(e.Time.VisibleTime)
	}
	if e.Time.LogicalTS > 0 {
		e.LogicalTS = e.Time.LogicalTS
	}
	if e.Causality.ParentEventID != "" {
		e.ParentEventID = e.Causality.ParentEventID
	}
	if len(e.Causality.CausalRefs) > 0 {
		e.CausalRefs = append([]string(nil), e.Causality.CausalRefs...)
	}
	if e.Access.Visibility != "" {
		e.Visibility = e.Access.Visibility
	}
	if len(e.Retrieval.EmbeddingVector) > 0 {
		e.EmbeddingVector = append([]float32(nil), e.Retrieval.EmbeddingVector...)
	}
	e.ensurePayload()
	e.copyIdentityPayloadHints()
	e.copyObjectPayloadHints()
	e.ensureDataAccounting()
	return e
}

func (e Event) cloneDynamicFields() Event {
	e.Object.Version = cloneJSONValue(e.Object.Version)
	e.Causality.CausalRefs = cloneStringSlice(e.Causality.CausalRefs)
	e.Causality.ProvenanceRefs = cloneStringSlice(e.Causality.ProvenanceRefs)
	e.Causality.SourceObjectIDs = cloneStringSlice(e.Causality.SourceObjectIDs)
	e.Causality.TargetObjectIDs = cloneStringSlice(e.Causality.TargetObjectIDs)
	e.Causality.Hooks = cloneEventHooks(e.Causality.Hooks)
	e.Access.VisibleToAgents = cloneStringSlice(e.Access.VisibleToAgents)
	e.Access.VisibleToRoles = cloneStringSlice(e.Access.VisibleToRoles)
	e.Access.PolicyTags = cloneStringSlice(e.Access.PolicyTags)
	e.Access.Hooks = cloneEventHooks(e.Access.Hooks)
	e.Materialization.Targets = cloneStringSlice(e.Materialization.Targets)
	e.Materialization.PlannedObjectIDs = cloneStringSlice(e.Materialization.PlannedObjectIDs)
	e.Materialization.Hooks = cloneEventHooks(e.Materialization.Hooks)
	e.Retrieval.EmbeddingVector = append([]float32(nil), e.Retrieval.EmbeddingVector...)
	e.Retrieval.IndexFields = cloneStringSlice(e.Retrieval.IndexFields)
	e.Retrieval.SparseTerms = cloneFloatMap(e.Retrieval.SparseTerms)
	e.Retrieval.Hooks = cloneEventHooks(e.Retrieval.Hooks)
	e.Extensions.Custom = cloneAnyMap(e.Extensions.Custom)
	e.Extensions.Labels = cloneStringSlice(e.Extensions.Labels)
	e.Extensions.Hooks = cloneEventHooks(e.Extensions.Hooks)
	e.Payload = cloneAnyMap(e.Payload)
	e.CausalRefs = cloneStringSlice(e.CausalRefs)
	e.EmbeddingVector = append([]float32(nil), e.EmbeddingVector...)
	return e
}

func (e Event) toWire() eventWire {
	return eventWire{
		SchemaVersion:   firstString(e.SchemaVersion, DynamicEventSchemaV04),
		Identity:        e.Identity,
		Actor:           e.Actor,
		Time:            e.Time,
		Event:           e.EventInfo,
		Object:          e.Object,
		Causality:       e.Causality,
		Access:          e.Access,
		Materialization: e.Materialization,
		Retrieval:       e.Retrieval,
		Payload:         e.Payload,
		Data:            e.Data,
		Runtime:         e.Runtime,
		Extensions:      e.Extensions,
	}
}

func (e *Event) applyLegacyWire(legacy eventLegacyWire) {
	if e.Identity.EventID == "" {
		e.Identity.EventID = legacy.EventID
	}
	if e.Identity.TenantID == "" {
		e.Identity.TenantID = legacy.TenantID
	}
	if e.Identity.WorkspaceID == "" {
		e.Identity.WorkspaceID = legacy.WorkspaceID
	}
	if e.Identity.Source == "" {
		e.Identity.Source = legacy.Source
	}
	if e.Actor.AgentID == "" {
		e.Actor.AgentID = legacy.AgentID
	}
	if e.Actor.SessionID == "" {
		e.Actor.SessionID = legacy.SessionID
	}
	if e.EventInfo.EventType == "" {
		e.EventInfo.EventType = legacy.EventType
	}
	if e.EventInfo.Importance == nil && legacy.Importance != 0 {
		e.EventInfo.Importance = &legacy.Importance
	}
	if e.Time.LogicalTS == 0 {
		e.Time.LogicalTS = legacy.LogicalTS
	}
	if e.Time.EventTime == 0 {
		e.Time.EventTime = rfc3339ToMillis(legacy.EventTime)
	}
	if e.Time.IngestTime == 0 {
		e.Time.IngestTime = rfc3339ToMillis(legacy.IngestTime)
	}
	if e.Time.VisibleTime == 0 {
		e.Time.VisibleTime = rfc3339ToMillis(legacy.VisibleTime)
	}
	if e.Causality.ParentEventID == "" {
		e.Causality.ParentEventID = legacy.ParentEventID
	}
	if len(e.Causality.CausalRefs) == 0 {
		e.Causality.CausalRefs = append([]string(nil), legacy.CausalRefs...)
	}
	if e.Access.Visibility == "" {
		e.Access.Visibility = legacy.Visibility
	}
	if len(e.Retrieval.EmbeddingVector) == 0 {
		e.Retrieval.EmbeddingVector = append([]float32(nil), legacy.EmbeddingVector...)
	}
	if e.Payload == nil {
		e.Payload = legacy.Payload
	}
	e.Version = legacy.Version
}

func (e Event) promoteLegacyToDynamic() Event {
	e.SchemaVersion = DynamicEventSchemaV04
	e.Identity.EventID = e.EventID
	e.Identity.TenantID = e.TenantID
	e.Identity.WorkspaceID = e.WorkspaceID
	e.Identity.Source = e.Source
	e.Actor.AgentID = e.AgentID
	e.Actor.SessionID = e.SessionID
	e.EventInfo.EventType = e.EventType
	if e.Importance != 0 {
		e.EventInfo.Importance = &e.Importance
	}
	e.Time.LogicalTS = e.LogicalTS
	e.Time.EventTime = rfc3339ToMillis(e.EventTime)
	e.Time.IngestTime = rfc3339ToMillis(e.IngestTime)
	e.Time.VisibleTime = rfc3339ToMillis(e.VisibleTime)
	e.Causality.ParentEventID = e.ParentEventID
	e.Causality.CausalRefs = append([]string(nil), e.CausalRefs...)
	e.Access.Visibility = e.Visibility
	e.Retrieval.EmbeddingVector = append([]float32(nil), e.EmbeddingVector...)
	return e
}

func hasOnlyLegacyEventFields(e Event) bool {
	return e.Identity.EventID == "" &&
		e.Actor.AgentID == "" &&
		e.Time.LogicalTS == 0 &&
		e.EventInfo.EventType == "" &&
		e.Object.ObjectType == "" &&
		e.Causality.ParentEventID == "" &&
		len(e.Causality.CausalRefs) == 0 &&
		e.Access.Visibility == "" &&
		e.Retrieval.IndexText == "" &&
		e.Data.PayloadSizeBytes == 0 &&
		e.Runtime.TEventCreatedMS == 0 &&
		len(e.Extensions.Labels) == 0 &&
		len(e.Extensions.Custom) == 0
}

func firstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func rfc3339ToMillis(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	return ts.UnixMilli()
}

func (e *Event) NormalizeInPlace() {
	*e = e.NormalizeDynamicEventV04()
}

func (e *Event) ensurePayload() {
	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
}

func (e *Event) copyIdentityPayloadHints() {
	if e.Identity.Dataset != "" {
		e.Payload[PayloadKeyDataset] = e.Identity.Dataset
	}
	if e.Identity.ImportBatchID != "" {
		e.Payload[PayloadKeyImportBatchID] = e.Identity.ImportBatchID
	}
	if e.Identity.FileName != "" {
		e.Payload[PayloadKeyFileName] = e.Identity.FileName
	}
}

func (e *Event) copyObjectPayloadHints() {
	if e.Object.StateKey != "" {
		e.Payload[PayloadKeyStateKey] = e.Object.StateKey
	}
	if e.Object.ArtifactName != "" {
		e.Payload[PayloadKeyArtifactName] = e.Object.ArtifactName
	}
	if e.Object.ArtifactURI != "" {
		e.Payload[PayloadKeyArtifactURI] = e.Object.ArtifactURI
	}
	if e.Object.URI != "" {
		e.Payload[PayloadKeyURI] = e.Object.URI
	}
	if e.Object.MimeType != "" {
		e.Payload[PayloadKeyMimeType] = e.Object.MimeType
	}
}

func (e *Event) ensureDataAccounting() {
	if e.Data.Canonicalization == "" {
		e.Data.Canonicalization = "json_canonical_v1"
	}
	payloadBytes, err := json.Marshal(e.Payload)
	if err == nil {
		if e.Data.PayloadSizeBytes == 0 {
			e.Data.PayloadSizeBytes = int64(len(payloadBytes))
		}
		if e.Data.PayloadHash == "" {
			sum := sha256.Sum256(payloadBytes)
			e.Data.PayloadHash = hex.EncodeToString(sum[:])
		}
	}
}

func (e Event) Text() string {
	if e.Retrieval.IndexText != "" {
		return e.Retrieval.IndexText
	}
	if s := e.payloadString(PayloadKeyText); s != "" {
		return s
	}
	if s := e.payloadString("content"); s != "" {
		return s
	}
	return ""
}

func (e Event) StateKey() string {
	if e.Object.StateKey != "" {
		return e.Object.StateKey
	}
	if s := e.payloadNestedString("state", "key"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyStateKey)
}

func (e Event) StateValueString() string {
	if s := e.payloadNestedString("state", "value"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyStateValue)
}

func (e Event) ArtifactURI() string {
	if e.Object.ArtifactURI != "" {
		return e.Object.ArtifactURI
	}
	if e.Object.URI != "" {
		return e.Object.URI
	}
	if s := e.payloadNestedString("artifact", "uri"); s != "" {
		return s
	}
	if s := e.payloadString(PayloadKeyArtifactURI); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyURI)
}

func (e Event) ArtifactName() string {
	if e.Object.ArtifactName != "" {
		return e.Object.ArtifactName
	}
	if s := e.payloadNestedString("artifact", "name"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyArtifactName)
}

func (e Event) ArtifactMimeType() string {
	if e.Object.MimeType != "" {
		return e.Object.MimeType
	}
	if s := e.payloadNestedString("artifact", "mime_type"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyMimeType)
}

func (e Event) IsArtifactLike() bool {
	e = e.NormalizeDynamicEventV04()
	if strings.EqualFold(strings.TrimSpace(e.EventInfo.EventType), string(ObjectTypeArtifact)) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(NormalizeObjectTypeName(e.Object.ObjectType)), string(ObjectTypeArtifact)) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(e.Object.ObjectSubtype), string(ObjectTypeArtifact)) {
		return true
	}
	if e.ArtifactURI() != "" || e.ArtifactName() != "" || e.ArtifactBodyString() != "" {
		return true
	}
	return false
}

func (e Event) ArtifactIDOrDefault() string {
	e = e.NormalizeDynamicEventV04()
	if e.IsArtifactLike() && strings.TrimSpace(e.Object.ObjectID) != "" {
		return strings.TrimSpace(e.Object.ObjectID)
	}
	return IDPrefixArtifact + e.Identity.EventID
}

func (e Event) ArtifactBodyString() string {
	if s := e.payloadNestedString("artifact", "body"); s != "" {
		return s
	}
	return e.payloadString("artifact_body")
}

func (e Event) EdgeKind() string {
	if e.Causality.EdgeKind != "" {
		return e.Causality.EdgeKind
	}
	if s := e.payloadNestedString("edge", "kind"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyRelationType)
}

func (e Event) MaterializerHooks() []string {
	return mergeHookNames(e.Materialization.Hooks.Materializers, e.Extensions.Hooks.Materializers)
}

func (e Event) IndexerHooks() []string {
	return mergeHookNames(e.Retrieval.Hooks.Indexers, e.Extensions.Hooks.Indexers)
}

func (e Event) QueryOpHooks() []string {
	return mergeHookNames(e.Retrieval.Hooks.QueryOps, e.Extensions.Hooks.QueryOps)
}

func (e Event) PolicyHooks() []string {
	return mergeHookNames(e.Access.Hooks.Policy, e.Extensions.Hooks.Policy)
}

func (e Event) EvidenceHooks() []string {
	return mergeHookNames(e.Causality.Hooks.Evidence, e.Extensions.Hooks.Evidence)
}

func (e Event) ChainHooks() []string {
	return mergeHookNames(e.Extensions.Hooks.Chains)
}

func (e Event) CustomHooks() []string {
	return mergeHookNames(
		e.Access.Hooks.Custom,
		e.Materialization.Hooks.Custom,
		e.Retrieval.Hooks.Custom,
		e.Causality.Hooks.Custom,
		e.Extensions.Hooks.Custom,
	)
}

func (e Event) RetrievalNamespaceOrDefault() string {
	e = e.NormalizeDynamicEventV04()
	if e.Retrieval.RetrievalNamespace != "" {
		return e.Retrieval.RetrievalNamespace
	}
	if e.Identity.WorkspaceID != "" {
		return e.Identity.WorkspaceID
	}
	if e.Actor.SessionID != "" {
		return e.Actor.SessionID
	}
	return "default"
}

func mergeHookNames(groups ...[]string) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, value := range group {
			key := strings.TrimSpace(value)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}

func cloneEventHooks(in EventHooks) EventHooks {
	return EventHooks{
		Materializers: cloneStringSlice(in.Materializers),
		Indexers:      cloneStringSlice(in.Indexers),
		QueryOps:      cloneStringSlice(in.QueryOps),
		Policy:        cloneStringSlice(in.Policy),
		Evidence:      cloneStringSlice(in.Evidence),
		Chains:        cloneStringSlice(in.Chains),
		Custom:        cloneStringSlice(in.Custom),
	}
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]float64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneJSONValue(value)
	}
	return out
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	case map[string]float64:
		return cloneFloatMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneJSONValue(item)
		}
		return out
	case []string:
		return cloneStringSlice(typed)
	case []float64:
		return append([]float64(nil), typed...)
	case []float32:
		return append([]float32(nil), typed...)
	case []int:
		return append([]int(nil), typed...)
	case []int64:
		return append([]int64(nil), typed...)
	default:
		return value
	}
}

func (e Event) payloadString(key string) string {
	if e.Payload == nil {
		return ""
	}
	return anyToString(e.Payload[key])
}

func (e Event) payloadNestedString(section, key string) string {
	if e.Payload == nil {
		return ""
	}
	switch m := e.Payload[section].(type) {
	case map[string]any:
		return anyToString(m[key])
	case map[string]string:
		return m[key]
	default:
		return ""
	}
}

func anyToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case json.Number:
		return x.String()
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func millisToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func NormalizeObjectTypeName(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "agent_state", "state":
		return string(ObjectTypeAgentState)
	case "relation":
		return "edge"
	default:
		return v
	}
}
