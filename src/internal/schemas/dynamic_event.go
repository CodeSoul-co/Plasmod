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
	ParentEventID   string   `json:"parent_event_id,omitempty"`
	CausalRefs      []string `json:"causal_refs,omitempty"`
	ProvenanceRefs  []string `json:"provenance_refs,omitempty"`
	CallEventID     string   `json:"call_event_id,omitempty"`
	SourceObjectID  string   `json:"source_object_id,omitempty"`
	TargetObjectID  string   `json:"target_object_id,omitempty"`
	SourceObjectIDs []string `json:"source_object_ids,omitempty"`
	TargetObjectIDs []string `json:"target_object_ids,omitempty"`
	EdgeKind        string   `json:"edge_kind,omitempty"`
	EdgeWeight      *float64 `json:"edge_weight,omitempty"`
	Reason          string   `json:"reason,omitempty"`
}

type EventAccess struct {
	Consistency     string   `json:"consistency,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	VisibleToAgents []string `json:"visible_to_agents,omitempty"`
	VisibleToRoles  []string `json:"visible_to_roles,omitempty"`
	TTLMS           *int64   `json:"ttl_ms,omitempty"`
	FreshnessSLAMS  *int64   `json:"freshness_sla_ms,omitempty"`
	PolicyTags      []string `json:"policy_tags,omitempty"`
	ShareContractID string   `json:"share_contract_id,omitempty"`
}

type EventMaterialization struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	Targets          []string `json:"targets,omitempty"`
	Mode             string   `json:"mode,omitempty"`
	PlannedObjectIDs []string `json:"planned_object_ids,omitempty"`
	Status           string   `json:"status,omitempty"`
	MaterializedAtMS *int64   `json:"materialized_at_ms,omitempty"`
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

type eventAlias Event

func (e Event) MarshalJSON() ([]byte, error) {
	normalized := e.NormalizeDynamicEventV04()
	return json.Marshal(normalized.toWire())
}

func (e *Event) UnmarshalJSON(data []byte) error {
	var legacy eventAlias
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	out := Event(legacy)
	out = out.NormalizeDynamicEventV04()
	*e = out
	return nil
}

func (e Event) NormalizeDynamicEventV04() Event {
	if e.SchemaVersion == "" && hasOnlyLegacyEventFields(e) {
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

func (e Event) EdgeKind() string {
	if e.Causality.EdgeKind != "" {
		return e.Causality.EdgeKind
	}
	if s := e.payloadNestedString("edge", "kind"); s != "" {
		return s
	}
	return e.payloadString(PayloadKeyRelationType)
}

func (e Event) RetrievalNamespaceOrDefault() string {
	if e.Retrieval.RetrievalNamespace != "" {
		return e.Retrieval.RetrievalNamespace
	}
	if e.WorkspaceID != "" {
		return e.WorkspaceID
	}
	if e.SessionID != "" {
		return e.SessionID
	}
	return "default"
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
	default:
		return v
	}
}
