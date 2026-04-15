package retrieval

import (
	"time"

	"plasmod/src/internal/schemas"
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

// ─── Additional Filters ───────────────────────────────────────────────────────

// NumericRangeFilter provides generic numeric field filtering.
// It supports filtering on any numeric metadata field (e.g., importance, confidence, version).
// Both Min and Max are optional (nil = no constraint on that bound).
type NumericRangeFilter struct {
	Field string   // Field name to filter on (e.g., "importance", "confidence", "version")
	Min   *float64 // Minimum value (inclusive); nil = no lower bound
	Max   *float64 // Maximum value (inclusive); nil = no upper bound
}

// Apply returns true when the memory's field value is within [Min, Max].
// If the field is not found or not numeric, the candidate is excluded.
func (f NumericRangeFilter) Apply(m schemas.Memory) bool {
	var value float64
	var found bool

	// Map common field names to Memory struct fields
	switch f.Field {
	case "importance":
		value, found = m.Importance, true
	case "confidence":
		value, found = m.Confidence, true
	case "version":
		value, found = float64(m.Version), true
	case "ttl":
		value, found = float64(m.TTL), true
	default:
		// Field not recognized
		return false
	}

	if !found {
		return false
	}

	// Check bounds
	if f.Min != nil && value < *f.Min {
		return false
	}
	if f.Max != nil && value > *f.Max {
		return false
	}

	return true
}

// TagFilter provides standardized tag-based filtering.
// It supports both required tags (all must be present) and excluded tags (none can be present).
type TagFilter struct {
	RequiredTags []string // All of these tags must be present
	ExcludedTags []string // None of these tags can be present
	Mode         string   // "all" (default) = all RequiredTags must match; "any" = at least one must match
}

// Apply returns true when the memory's PolicyTags satisfy the tag requirements.
func (f TagFilter) Apply(m schemas.Memory) bool {
	// Check excluded tags first (fast rejection)
	for _, excluded := range f.ExcludedTags {
		if hasTag(m.PolicyTags, excluded) {
			return false
		}
	}

	// If no required tags, pass
	if len(f.RequiredTags) == 0 {
		return true
	}

	// Check required tags based on mode
	mode := f.Mode
	if mode == "" {
		mode = "all" // default
	}

	if mode == "any" {
		// At least one required tag must be present
		for _, required := range f.RequiredTags {
			if hasTag(m.PolicyTags, required) {
				return true
			}
		}
		return false
	}

	// mode == "all": all required tags must be present
	for _, required := range f.RequiredTags {
		if !hasTag(m.PolicyTags, required) {
			return false
		}
	}
	return true
}
