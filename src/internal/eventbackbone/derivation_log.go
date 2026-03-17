package eventbackbone

import "sync"

// DerivationEntry records a single step in a derivation chain — e.g. how a
// Memory was extracted from an Event, or how a summary was produced from raw
// records.  This log is the basis for proof-trace assembly (spec section 11.2).
type DerivationEntry struct {
	LSN          int64  `json:"lsn"`
	SourceID     string `json:"source_id"`
	SourceType   string `json:"source_type"`
	DerivedID    string `json:"derived_id"`
	DerivedType  string `json:"derived_type"`
	Operation    string `json:"operation"`
	LogicalTS    int64  `json:"logical_ts"`
}

// DerivationLog is an append-only log of derivation steps consumed by the
// ProofTraceWorker and the evidence Assembler.
type DerivationLog struct {
	mu      sync.RWMutex
	clock   *HybridClock
	bus     Bus
	entries []DerivationEntry
}

func NewDerivationLog(clock *HybridClock, bus Bus) *DerivationLog {
	return &DerivationLog{clock: clock, bus: bus}
}

// Append records a derivation step and broadcasts it on "derivation.events".
func (d *DerivationLog) Append(sourceID, sourceType, derivedID, derivedType, operation string) DerivationEntry {
	ts := d.clock.Next()
	entry := DerivationEntry{
		LSN:         ts,
		SourceID:    sourceID,
		SourceType:  sourceType,
		DerivedID:   derivedID,
		DerivedType: derivedType,
		Operation:   operation,
		LogicalTS:   ts,
	}
	d.mu.Lock()
	d.entries = append(d.entries, entry)
	d.mu.Unlock()
	d.bus.Publish(Message{Channel: "derivation.events", Body: entry})
	return entry
}

// Since returns all derivation entries with LSN >= fromLSN.
func (d *DerivationLog) Since(fromLSN int64) []DerivationEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := []DerivationEntry{}
	for _, e := range d.entries {
		if e.LSN >= fromLSN {
			out = append(out, e)
		}
	}
	return out
}

// ForDerived returns all derivation entries that produced the given object ID.
func (d *DerivationLog) ForDerived(derivedID string) []DerivationEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := []DerivationEntry{}
	for _, e := range d.entries {
		if e.DerivedID == derivedID {
			out = append(out, e)
		}
	}
	return out
}
