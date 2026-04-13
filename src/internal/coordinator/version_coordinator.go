package coordinator

import (
	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/schemas"
	"plasmod/src/internal/storage"
)

// VersionCoordinator advances logical time and manages object visibility.
// It implements the version/time model from spec section 13:
// event_time, ingest_time, visible_time, logical_version.
type VersionCoordinator struct {
	clock    *eventbackbone.HybridClock
	versions storage.SnapshotVersionStore
}

func NewVersionCoordinator(clock *eventbackbone.HybridClock, versions storage.SnapshotVersionStore) *VersionCoordinator {
	return &VersionCoordinator{clock: clock, versions: versions}
}

// NextLogicalTS allocates the next monotonic logical timestamp.
func (c *VersionCoordinator) NextLogicalTS() int64 {
	return c.clock.Next()
}

// LatestVersion returns the most recent version record for an object.
func (c *VersionCoordinator) LatestVersion(objectID string) (schemas.ObjectVersion, bool) {
	return c.versions.LatestVersion(objectID)
}

// History returns all version records for an object (time-travel support).
func (c *VersionCoordinator) History(objectID string) []schemas.ObjectVersion {
	return c.versions.GetVersions(objectID)
}

// Publish marks a version record as visible by setting its valid_from logical
// timestamp.  This models the visible_time advancement from spec section 13.
func (c *VersionCoordinator) Publish(objectID, objectType, mutationEventID string) schemas.ObjectVersion {
	var next int64 = 1
	if latest, ok := c.versions.LatestVersion(objectID); ok {
		next = latest.Version + 1
	}
	v := schemas.ObjectVersion{
		ObjectID:        objectID,
		ObjectType:      objectType,
		Version:         next,
		MutationEventID: mutationEventID,
	}
	c.versions.PutVersion(v)
	return v
}
