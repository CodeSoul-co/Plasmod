package storage

import (
	"fmt"
	"sort"
	"strings"
)

// EmbeddingSpec identifies one vector space. Vectors can only be queried or
// indexed together when both fields match exactly.
type EmbeddingSpec struct {
	Family string `json:"family"`
	Dim    int    `json:"dim"`
}

func ResolveEmbeddingSpec(attrs map[string]string, family string, dim int) EmbeddingSpec {
	if family = strings.TrimSpace(family); family == "" {
		family = ResolveEmbeddingFamily(attrs)
	}
	return EmbeddingSpec{Family: family, Dim: dim}
}

func (s EmbeddingSpec) Valid() bool {
	return strings.TrimSpace(s.Family) != "" && s.Dim > 0
}

func (s EmbeddingSpec) String() string {
	return fmt.Sprintf("%s/%d", s.Family, s.Dim)
}

// EmbeddingCompatibilityReport describes persisted segments that cannot share
// the configured embedding space. Legacy records with no dimension are treated
// as incompatible deliberately: assuming they match is unsafe after a model
// change.
type EmbeddingCompatibilityReport struct {
	Target       EmbeddingSpec `json:"target"`
	Checked      int           `json:"checked"`
	Incompatible int           `json:"incompatible"`
	Legacy       int           `json:"legacy"`
	Examples     []string      `json:"examples,omitempty"`
}

func (r EmbeddingCompatibilityReport) Compatible() bool {
	return r.Incompatible == 0
}

func (r EmbeddingCompatibilityReport) Error() error {
	if r.Compatible() {
		return nil
	}
	return fmt.Errorf(
		"embedding index incompatible with runtime %s: %d/%d segment records differ (%d legacy without dimension); run with PLASMOD_EMBEDDING_REINDEX=1 to rebuild",
		r.Target, r.Incompatible, r.Checked, r.Legacy,
	)
}

func CheckEmbeddingCompatibility(records []SegmentRecord, target EmbeddingSpec) EmbeddingCompatibilityReport {
	report := EmbeddingCompatibilityReport{Target: target, Checked: len(records)}
	examples := make([]string, 0, 3)
	for _, record := range records {
		family := strings.TrimSpace(record.EmbeddingFamily)
		legacy := family == "" || record.EmbeddingDim <= 0
		if !legacy && family == target.Family && record.EmbeddingDim == target.Dim {
			continue
		}
		report.Incompatible++
		if legacy {
			report.Legacy++
		}
		if len(examples) < cap(examples) {
			examples = append(examples, record.SegmentID)
		}
	}
	sort.Strings(examples)
	report.Examples = examples
	return report
}
