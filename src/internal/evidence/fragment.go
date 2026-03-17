package evidence

import "time"

// EvidenceFragment is a pre-computed partial proof chain stored at ingest time
// for a specific canonical object.  At query time the Assembler merges cached
// fragments rather than re-deriving chains from scratch, implementing the
// "DB-side pre-computation" design goal.
type EvidenceFragment struct {
	ObjectID      string    `json:"object_id"`
	ObjectType    string    `json:"object_type"`
	Namespace     string    `json:"namespace"`
	TextTokens    []string  `json:"text_tokens"`
	RelatedIDs    []string  `json:"related_ids"`
	EdgeTypes     []string  `json:"edge_types"`
	PolicyFilters []string  `json:"policy_filters"`
	SalienceScore float64   `json:"salience_score"`
	Level         int       `json:"level"`
	ComputedAt    time.Time `json:"computed_at"`
	// LogicalTS is the ingest-time watermark at which this fragment was built.
	LogicalTS int64 `json:"logical_ts"`
}

// ProofStep is a single step in the assembled proof trace returned to callers.
type ProofStep struct {
	StepType    string `json:"step_type"`
	ObjectID    string `json:"object_id"`
	Description string `json:"description"`
}
