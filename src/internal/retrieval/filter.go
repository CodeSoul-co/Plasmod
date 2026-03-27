package retrieval

import (
	"time"

	"andb/src/internal/schemas"
)

// SafetyFilter applies the 7 governance rules that mirror the Python
// safety filter before any scoring or ranking.
//
// Rule reference (matches integration_tests/test_retrieval_b.py):
//
//  1. quarantine  — PolicyTags contains "quarantine" → exclude (when ExcludeQuarantined=true)
//  2. ttl         — TTL field is non-zero and expired (TTL < now.Unix()) → exclude
//  3. visible     — ValidFrom is set and in the future (ValidFrom > now) → exclude
//  4. is_active   — IsActive=false → exclude
//  5. as_of_ts    — ValidFrom > req.AsOfTS → exclude (time-travel: only records visible by AsOfTS)
//  6. min_version — Version < req.MinVersion → exclude (0 = no constraint)
//  7. unverified  — LifecycleState == "unverified" or PolicyTags contains "unverified" → exclude (when ExcludeUnverified=true)
type SafetyFilter struct{}

// Apply returns true when the memory PASSES all rules (i.e. should be included).
func (f SafetyFilter) Apply(m schemas.Memory, req RetrievalRequest) bool {
	now := time.Now().UTC()

	// Rule 1: quarantine
	if req.ExcludeQuarantined && hasTag(m.PolicyTags, "quarantine") {
		return false
	}

	// Rule 2: TTL expiry
	if m.TTL > 0 && m.TTL < now.Unix() {
		return false
	}

	// Rule 3: visible_time (ValidFrom) must not be in the future
	if m.ValidFrom != "" {
		if t, err := parseTimestamp(m.ValidFrom); err == nil && t.After(now) {
			return false
		}
	}

	// Rule 4: is_active
	if !m.IsActive {
		return false
	}

	// Rule 5: as_of_ts time-travel — only include records whose ValidFrom ≤ AsOfTS
	if !req.AsOfTS.IsZero() && m.ValidFrom != "" {
		if t, err := parseTimestamp(m.ValidFrom); err == nil && t.After(req.AsOfTS) {
			return false
		}
	}

	// Rule 6: min_version
	if req.MinVersion > 0 && m.Version < req.MinVersion {
		return false
	}

	// Rule 7: exclude unverified
	if req.ExcludeUnverified {
		if m.LifecycleState == "unverified" || hasTag(m.PolicyTags, "unverified") {
			return false
		}
	}

	return true
}

// hasTag returns true when tags contains target (case-sensitive).
func hasTag(tags []string, target string) bool {
	for _, t := range tags {
		if t == target {
			return true
		}
	}
	return false
}

// parseTimestamp parses RFC3339 or Unix-second strings.
func parseTimestamp(s string) (time.Time, error) {
	// Try RFC3339 first (most common in our schemas)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Fall back to RFC3339Nano
	return time.Parse(time.RFC3339Nano, s)
}
