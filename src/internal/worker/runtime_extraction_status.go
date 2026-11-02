package worker

import (
	"encoding/json"
	"errors"
	"time"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
	"plasmod/src/internal/worker/cognition"
)

// ExtractionStatus is separate from the cached valid facts and the global WAL
// checkpoint: a failed model response must never become a successful extraction.
type ExtractionStatus struct {
	EventID        string `json:"event_id"`
	SourceMemoryID string `json:"source_memory_id"`
	LSN            int64  `json:"lsn"`
	BaseProjection string `json:"base_projection"`
	Status         string `json:"status"`
	Stage          string `json:"stage"`
	Attempts       int    `json:"attempts"`
	FactCount      int    `json:"fact_count"`
	ErrorCode      string `json:"error_code,omitempty"`
	FactIndex      *int   `json:"fact_index,omitempty"`
	UpdatedAt      string `json:"updated_at"`
}

func extractionStatusID(tenant, workspace, eventID string) string {
	return "extraction_status:" + stableID(tenant, workspace, eventID)
}
func (r *Runtime) ExtractionStatus(tenant, workspace, eventID string) (ExtractionStatus, bool) {
	st, ok := r.storage.Objects().GetState(extractionStatusID(tenant, workspace, eventID))
	var out ExtractionStatus
	if !ok || json.Unmarshal([]byte(st.StateValue), &out) != nil {
		return out, false
	}
	return out, true
}
func (r *Runtime) saveExtractionStatus(source schemas.Memory, out *ExtractionStatus, err error) error {
	out.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	out.ErrorCode, out.FactIndex = "", nil
	if err != nil {
		out.Status = "failed"
		out.ErrorCode = out.Stage + "_failed"
		var validation *cognition.ValidationError
		if errors.As(err, &validation) {
			out.ErrorCode = validation.Code
			out.FactIndex = &validation.FactIndex
		}
	}
	data, marshalErr := json.Marshal(out)
	if marshalErr != nil {
		return marshalErr
	}
	st := schemas.State{StateID: extractionStatusID(source.TenantID, source.WorkspaceID, out.EventID), TenantID: source.TenantID, WorkspaceID: source.WorkspaceID, AgentID: source.AgentID, SessionID: source.SessionID, StateType: "memory_extraction_status", StateValue: string(data), DerivedFromEventID: out.EventID, Access: source.Access, Version: int64(out.Attempts)}
	return r.storage.ApplyCanonicalProjection(storage.CanonicalProjection{State: &st})
}
