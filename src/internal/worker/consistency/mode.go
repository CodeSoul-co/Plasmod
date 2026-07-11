package consistency

import (
	"fmt"
	"strings"
	"time"

	"plasmod/src/internal/schemas"
)

// Mode is the canonical consistency behavior applied by the runtime.
type Mode string

const (
	StrictVisible      Mode = "strict_visible"
	BoundedStaleness   Mode = "bounded_staleness"
	EventualVisibility Mode = "eventual_visibility"
)

// ParseMode accepts public aliases and returns the canonical runtime mode.
func ParseMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strict", string(StrictVisible):
		return StrictVisible, nil
	case "bounded", string(BoundedStaleness):
		return BoundedStaleness, nil
	case "eventual", string(EventualVisibility):
		return EventualVisibility, nil
	default:
		return "", fmt.Errorf("unsupported consistency mode %q", raw)
	}
}

// ResolveWrite resolves an event override before the runtime default and writes
// the canonical decision back into the event for durable WAL recovery.
func ResolveWrite(
	defaultMode Mode,
	ev schemas.Event,
	defaultLag time.Duration,
) (Mode, time.Duration, schemas.Event, error) {
	mode, err := ParseMode(string(defaultMode))
	if err != nil {
		return "", 0, ev, err
	}
	if explicit := strings.TrimSpace(ev.Access.Consistency); explicit != "" {
		mode, err = ParseMode(explicit)
		if err != nil {
			return "", 0, ev, err
		}
	}

	ev.Access.Consistency = string(mode)
	if mode != BoundedStaleness {
		return mode, 0, ev, nil
	}

	lag := defaultLag
	if ev.Access.FreshnessSLAMS != nil {
		if *ev.Access.FreshnessSLAMS <= 0 {
			return "", 0, ev, fmt.Errorf("freshness_sla_ms must be positive")
		}
		lag = time.Duration(*ev.Access.FreshnessSLAMS) * time.Millisecond
	}
	if lag < time.Millisecond {
		return "", 0, ev, fmt.Errorf("bounded staleness lag must be at least 1ms")
	}

	lagMS := lag.Milliseconds()
	ev.Access.FreshnessSLAMS = &lagMS
	return mode, lag, ev, nil
}

// ResolveRead resolves a query override before the runtime default.
func ResolveRead(defaultMode Mode, req schemas.QueryRequest) (Mode, error) {
	if explicit := strings.TrimSpace(req.AccessConsistency); explicit != "" {
		return ParseMode(explicit)
	}
	return ParseMode(string(defaultMode))
}
