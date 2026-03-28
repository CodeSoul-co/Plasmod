package memorybank

import (
	"encoding/json"
	"strings"
	"time"

	"andb/src/internal/schemas"
)

// ProfileState captures stable user traits and preferences derived from memory
// interactions. It is serialised as JSON and stored in MemoryAlgorithmState.PortraitState.
type ProfileState struct {
	StableTraits        map[string]string  `json:"stable_traits,omitempty"`
	Preferences        map[string]float64 `json:"preferences,omitempty"`         // key: preference, value: strength (0-1)
	CommunicationStyle string             `json:"communication_style,omitempty"`
	// UncertainInferences stores low-confidence model inferences that should NOT
	// automatically enter high-priority prompts. They require explicit user confirmation.
	UncertainInferences map[string]float64 `json:"uncertain_inferences,omitempty"`
	UpdatedAt          string             `json:"updated_at"`
}

// ExtractProfile extracts a ProfileState from the given memories.
// Memories with MemoryTypeProfile are used as primary sources.
// Episodic memories are used as hints. Uncertain inferences (single source,
// low confidence) are stored separately and flagged as uncertain.
func ExtractProfile(memories []schemas.Memory) *ProfileState {
	ps := &ProfileState{
		StableTraits:        make(map[string]string),
		Preferences:        make(map[string]float64),
		UncertainInferences: make(map[string]float64),
		UpdatedAt:          time.Now().UTC().Format(time.RFC3339),
	}

	var profileSrcs, episodicSrcs, preferenceSrcs []schemas.Memory
	for _, m := range memories {
		switch string(m.MemoryType) {
		case string(schemas.MemoryTypeProfile):
			profileSrcs = append(profileSrcs, m)
		case string(schemas.MemoryTypePreferenceConstraint):
			preferenceSrcs = append(preferenceSrcs, m)
		case string(schemas.MemoryTypeEpisodic), string(schemas.MemoryTypeReflective):
			episodicSrcs = append(episodicSrcs, m)
		}
	}

	// Extract from profile-type memories
	for _, m := range profileSrcs {
		if m.Confidence >= 0.8 && len(m.SourceEventIDs) > 1 {
			// High-confidence, multi-source profile memories → stable traits
			parseProfileContent(m.Content, ps.StableTraits)
			if m.Summary != "" {
				parseProfileContent(m.Summary, ps.StableTraits)
			}
			ps.CommunicationStyle = detectCommunicationStyle(m.Content)
		}
	}

	// Extract from preference memories
	for _, m := range preferenceSrcs {
		if m.Confidence >= 0.7 {
			key, strength := parsePreference(m.Content)
			if key != "" {
				// Average with existing if already present
				if existing, ok := ps.Preferences[key]; ok {
					ps.Preferences[key] = (existing + strength) / 2.0
				} else {
					ps.Preferences[key] = strength
				}
			}
		}
	}

	// Extract uncertain inferences from episodic memories (single source = uncertain)
	for _, m := range episodicSrcs {
		if m.Confidence < 0.7 || len(m.SourceEventIDs) < 2 {
			// Single-source or low-confidence → uncertain inference
			key, strength := parsePreference(m.Content)
			if key != "" {
				ps.UncertainInferences[key] = strength
			}
		}
	}

	return ps
}

// MergeProfile merges new signals into an existing ProfileState.
// Preference strength is averaged with existing value.
// Stable traits are overwritten only when new evidence confidence exceeds existing.
func MergeProfile(existing, new *ProfileState) *ProfileState {
	if existing == nil {
		return new
	}
	if new == nil {
		return existing
	}

	merged := &ProfileState{
		StableTraits:        make(map[string]string),
		Preferences:        make(map[string]float64),
		UncertainInferences: make(map[string]float64),
		UpdatedAt:          new.UpdatedAt,
	}

	// Merge stable traits
	for k, v := range existing.StableTraits {
		merged.StableTraits[k] = v
	}
	for k, v := range new.StableTraits {
		merged.StableTraits[k] = v // overwrite with new
	}

	// Merge preferences (average)
	for k, v := range existing.Preferences {
		merged.Preferences[k] = v
	}
	for k, v := range new.Preferences {
		if existingVal, ok := merged.Preferences[k]; ok {
			merged.Preferences[k] = (existingVal + v) / 2.0
		} else {
			merged.Preferences[k] = v
		}
	}

	// Merge uncertain inferences
	for k, v := range existing.UncertainInferences {
		merged.UncertainInferences[k] = v
	}
	for k, v := range new.UncertainInferences {
		merged.UncertainInferences[k] = v
	}

	// Communication style: use new if present
	if new.CommunicationStyle != "" {
		merged.CommunicationStyle = new.CommunicationStyle
	} else {
		merged.CommunicationStyle = existing.CommunicationStyle
	}

	return merged
}

// Serialize returns the JSON-serialised form of ps.
func (ps *ProfileState) Serialize() string {
	if ps == nil {
		return ""
	}
	b, _ := json.Marshal(ps)
	return string(b)
}

// DeserializeProfile parses a JSON string into a ProfileState.
func DeserializeProfile(data string) (*ProfileState, error) {
	if data == "" {
		return nil, nil
	}
	var ps ProfileState
	if err := json.Unmarshal([]byte(data), &ps); err != nil {
		return nil, err
	}
	return &ps, nil
}

// parseProfileContent extracts key:value or "trait = value" pairs from content.
func parseProfileContent(content string, traits map[string]string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Try "key: value" or "key = value" format
		for _, sep := range []string{": ", " = "} {
			if idx := strings.Index(line, sep); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+len(sep):])
				if key != "" && val != "" {
					traits[key] = val
					break
				}
			}
		}
	}
}

// parsePreference extracts a preference key and strength from content.
// Looks for patterns like "prefers X" (strength 0.8) or "avoids X" (strength 0.3).
func parsePreference(content string) (key string, strength float64) {
	content = strings.ToLower(content)
	switch {
	case strings.Contains(content, "prefers") || strings.Contains(content, "likes") || strings.Contains(content, "wants"):
		return strings.TrimSpace(content), 0.8
	case strings.Contains(content, "avoids") || strings.Contains(content, "dislikes") || strings.Contains(content, "hates"):
		return strings.TrimSpace(content), 0.3
	case strings.Contains(content, "neutral"):
		return strings.TrimSpace(content), 0.5
	default:
		return strings.TrimSpace(content), 0.6
	}
}

// detectCommunicationStyle infers communication style from content keywords.
func detectCommunicationStyle(content string) string {
	content = strings.ToLower(content)
	switch {
	case strings.Contains(content, "concise"):
		return "concise"
	case strings.Contains(content, "detailed"):
		return "detailed"
	case strings.Contains(content, "technical"):
		return "technical"
	case strings.Contains(content, "casual"):
		return "casual"
	case strings.Contains(content, "formal"):
		return "formal"
	default:
		return "neutral"
	}
}
