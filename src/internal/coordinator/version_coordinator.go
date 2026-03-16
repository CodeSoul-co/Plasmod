package coordinator

import "andb/src/internal/eventbackbone"

type VersionCoordinator struct {
	clock *eventbackbone.HybridClock
}

func NewVersionCoordinator(clock *eventbackbone.HybridClock) *VersionCoordinator {
	return &VersionCoordinator{clock: clock}
}
