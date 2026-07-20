package worker

import (
	"encoding/json"
	"time"

	"plasmod/src/internal/schemas"
)

// prepareStateMutation resolves one provisional materialized State against the
// current canonical object and version history. The caller holds
// stateProjectionMu until ApplyCanonicalProjection completes.
func (r *Runtime) prepareStateMutation(
	candidate *schemas.State,
	candidateVersion *schemas.ObjectVersion,
) (*schemas.State, []schemas.ObjectVersion) {
	if candidate == nil || r.storage == nil {
		return candidate, nil
	}
	state := *candidate
	versions := r.storage.Versions().GetVersions(state.StateID)
	for _, version := range versions {
		if version.MutationEventID != "" && version.MutationEventID == state.DerivedFromEventID {
			if current, ok := r.storage.Objects().GetState(state.StateID); ok &&
				current.DerivedFromEventID == state.DerivedFromEventID {
				return &current, nil
			}
			// The Event is already represented in history and a newer mutation is
			// current. Replaying the old Event must not roll state backward.
			return nil, nil
		}
	}

	now := state.CheckpointTS
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339)
		state.CheckpointTS = now
	}
	updates := make([]schemas.ObjectVersion, 0, 2)
	if current, ok := r.storage.Objects().GetState(state.StateID); ok {
		state.Version = current.Version + 1
		if latest, found := r.storage.Versions().LatestVersion(state.StateID); found {
			latest.ValidTo = now
			if len(latest.Snapshot) == 0 {
				latest.Snapshot = canonicalSnapshot(current)
			}
			updates = append(updates, latest)
		}
	} else {
		state.Version = 1
	}

	version := schemas.ObjectVersion{
		ObjectID:        state.StateID,
		ObjectType:      string(schemas.ObjectTypeAgentState),
		Version:         state.Version,
		MutationEventID: state.DerivedFromEventID,
		ValidFrom:       now,
		SnapshotTag:     "state_update",
		MutationLSN:     state.MutationLSN,
		Snapshot:        canonicalSnapshot(state),
		Access:          state.Access,
	}
	if candidateVersion != nil && candidateVersion.SnapshotTag != "" {
		version.SnapshotTag = candidateVersion.SnapshotTag
	}
	updates = append(updates, version)
	return &state, updates
}

func canonicalSnapshot(value any) map[string]any {
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
