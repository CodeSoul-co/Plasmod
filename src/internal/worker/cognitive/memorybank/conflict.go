package memorybank

import (
	"fmt"
	"strings"
	"time"

	"andb/src/internal/schemas"
)

// ConflictType categorises the kind of memory conflict detected.
type ConflictType int

const (
	ConflictTypeNone        ConflictType = iota
	ConflictTypeValue                  // same attribute has contradictory values
	ConflictTypePreference             // opposite preference polarity expressed
	ConflictTypeFactual                // high-confidence evidence contradicts memory
	ConflictTypeEntity                 // same entity has conflicting properties
)

// ConflictRecord stores a detected conflict between two memories.
type ConflictRecord struct {
	LeftID, RightID string
	Type           ConflictType
	Severity       float64 // 0-1; ≥0.7 → confirmed, <0.7 → suspected
	DetectedAt     string
	Resolved       bool
}

// ConflictRegistry manages conflict records in-memory within the algorithm.
// Conflicts are keyed by memory ID; each memory can have multiple conflict records.
type ConflictRegistry struct {
	// records keyed by memoryID → list of conflict records involving this memory
	records map[string][]ConflictRecord
}

// NewConflictRegistry creates an empty conflict registry.
func NewConflictRegistry() *ConflictRegistry {
	return &ConflictRegistry{records: make(map[string][]ConflictRecord)}
}

// Register records a conflict for both involved memories.
func (r *ConflictRegistry) Register(c ConflictRecord) {
	r.records[c.LeftID] = append(r.records[c.LeftID], c)
	r.records[c.RightID] = append(r.records[c.RightID], c)
}

// Get returns all conflict records for a given memory.
func (r *ConflictRegistry) Get(memoryID string) []ConflictRecord {
	return r.records[memoryID]
}

// MaxSeverity returns the highest conflict severity for a memory (0 if none).
func (r *ConflictRegistry) MaxSeverity(memoryID string) float64 {
	max := 0.0
	for _, c := range r.records[memoryID] {
		if c.Severity > max {
			max = c.Severity
		}
	}
	return max
}

// IsConfirmed returns true if any conflict for this memory has severity ≥ 0.7.
func (r *ConflictRegistry) IsConfirmed(memoryID string) bool {
	return r.MaxSeverity(memoryID) >= 0.7
}

// IsSuspected returns true if any conflict has severity > 0 and < 0.7.
func (r *ConflictRegistry) IsSuspected(memoryID string) bool {
	for _, c := range r.records[memoryID] {
		if c.Severity > 0 && c.Severity < 0.7 {
			return true
		}
	}
	return false
}

// DetectConflicts checks newMem against all registered memories for the same agent.
// Returns a list of ConflictRecords detected. The registry is NOT updated here
// (caller should register detected conflicts via DetectAndRegister).
func DetectConflicts(
	registry *ConflictRegistry,
	existing []schemas.Memory,
	newMem schemas.Memory,
) []ConflictRecord {
	var out []ConflictRecord
	now := time.Now().UTC().Format(time.RFC3339)

	for _, existingMem := range existing {
		if existingMem.MemoryID == newMem.MemoryID {
			continue
		}
		// Only compare memories from the same agent
		if existingMem.AgentID != newMem.AgentID {
			continue
		}

		conflict := evaluatePair(existingMem, newMem, now)
		if conflict != nil && conflict.Severity > 0 {
			out = append(out, *conflict)
		}
	}
	return out
}

// evaluatePair compares two memories and returns a ConflictRecord if a conflict exists.
func evaluatePair(a, b schemas.Memory, now string) *ConflictRecord {
	// Rule 1: Value contradiction — same type, overlapping time, different content
	if a.MemoryType == b.MemoryType && a.MemoryType != "" {
		if !sameContent(a.Content, b.Content) && timeOverlap(a, b) {
			return &ConflictRecord{
				LeftID: a.MemoryID, RightID: b.MemoryID,
				Type: ConflictTypeValue,
				Severity:       0.8,
				DetectedAt: now,
			}
		}
	}

	// Rule 2: Preference reversal — preference constraint with opposite polarity
	if a.MemoryType == string(schemas.MemoryTypePreferenceConstraint) ||
		b.MemoryType == string(schemas.MemoryTypePreferenceConstraint) {
		if polarity(a.Content) != "neutral" && polarity(b.Content) != "neutral" {
			if polarity(a.Content) != polarity(b.Content) {
				return &ConflictRecord{
					LeftID: a.MemoryID, RightID: b.MemoryID,
					Type: ConflictTypePreference,
					Severity:       0.85,
					DetectedAt: now,
				}
			}
		}
	}

	// Rule 3: Factual disagreement — high-confidence factual contradicts new memory
	if a.MemoryType == string(schemas.MemoryTypeFactual) && a.Confidence >= 0.9 {
		if !sameContent(a.Content, b.Content) && !timeOverlap(a, b) {
			return &ConflictRecord{
				LeftID: a.MemoryID, RightID: b.MemoryID,
				Type: ConflictTypeFactual,
				Severity:       0.75,
				DetectedAt: now,
			}
		}
	}

	// Rule 4: Entity conflict — same source events, different confidence
	if len(a.SourceEventIDs) > 0 && len(b.SourceEventIDs) > 0 {
		if overlapSourceEvents(a.SourceEventIDs, b.SourceEventIDs) {
			delta := a.Confidence - b.Confidence
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.3 {
				return &ConflictRecord{
					LeftID: a.MemoryID, RightID: b.MemoryID,
					Type: ConflictTypeEntity,
					Severity:       0.6,
					DetectedAt: now,
				}
			}
		}
	}

	return nil
}

// sameContent returns true if two content strings are semantically the same.
// Uses simple heuristics: identical or one is a prefix of the other.
func sameContent(a, b string) bool {
	if a == b {
		return true
	}
	// Normalise whitespace
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	return a == b
}

// timeOverlap returns true if two memories have overlapping time validity.
// If both have ValidFrom/ValidTo set, they overlap if their intervals intersect.
func timeOverlap(a, b schemas.Memory) bool {
	// If no validity info, assume they may overlap (conservative)
	if a.ValidFrom == "" && b.ValidFrom == "" {
		return true
	}
	// If one has validity and other doesn't, assume overlap
	if a.ValidFrom == "" || b.ValidFrom == "" {
		return true
	}

	aFrom := parseTime(a.ValidFrom)
	bFrom := parseTime(b.ValidFrom)
	if aFrom.IsZero() || bFrom.IsZero() {
		return true
	}

	// Use ValidTo or now if not set
	aTo := parseTime(a.ValidTo)
	bTo := parseTime(b.ValidTo)
	if aTo.IsZero() {
		aTo = time.Now().UTC()
	}
	if bTo.IsZero() {
		bTo = time.Now().UTC()
	}

	// Two intervals [aFrom, aTo] and [bFrom, bTo] overlap if aFrom ≤ bTo && bFrom ≤ aTo
	return !aFrom.After(bTo) && !bFrom.After(aTo)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func overlapSourceEvents(a, b []string) bool {
	set := make(map[string]bool)
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if set[id] {
			return true
		}
	}
	return false
}

// PolicyTagsForConflict returns the policy tags to apply based on conflict severity.
func PolicyTagsForConflict(severity float64) []string {
	if severity >= 0.7 {
		return []string{"conflict_confirmed"}
	}
	if severity > 0 {
		return []string{"conflict_suspected"}
	}
	return nil
}

// String returns a human-readable representation of ConflictType.
func (ct ConflictType) String() string {
	switch ct {
	case ConflictTypeNone:
		return "none"
	case ConflictTypeValue:
		return "value_contradiction"
	case ConflictTypePreference:
		return "preference_reversal"
	case ConflictTypeFactual:
		return "factual_disagreement"
	case ConflictTypeEntity:
		return "entity_conflict"
	default:
		return fmt.Sprintf("unknown(%d)", ct)
	}
}
