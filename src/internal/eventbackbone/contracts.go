package eventbackbone

import "plasmod/src/internal/schemas"

// WAL is the write-ahead log interface — the single source of truth for all
// state-changing requests in the system.
type WAL interface {
	// Append writes a new event to the log and returns the assigned entry.
	Append(event schemas.Event) (WALEntry, error)
	// Scan returns all WAL entries with LSN >= fromLSN, enabling replay and
	// bounded-staleness recovery.
	Scan(fromLSN int64) []WALEntry
	// LatestLSN returns the last assigned log sequence number.
	LatestLSN() int64
}

// ErrorAwareWAL is implemented by durable WALs whose scans can fail because
// of storage or decoding errors.
type ErrorAwareWAL interface {
	WAL
	ScanWithError(fromLSN int64) ([]WALEntry, error)
}

// ScanWAL preserves the simple in-memory WAL contract while propagating errors
// from durable implementations.
func ScanWAL(wal WAL, fromLSN int64) ([]WALEntry, error) {
	if scanner, ok := wal.(ErrorAwareWAL); ok {
		return scanner.ScanWithError(fromLSN)
	}
	return wal.Scan(fromLSN), nil
}

// Bus is the pub-sub message bus used to decouple producers from subscribers.
type Bus interface {
	Subscribe(channel string) <-chan Message
	Publish(msg Message)
}

// WatermarkReader exposes the current visible-time watermark.
type WatermarkReader interface {
	Current() int64
}

// DerivationLogger records derivation steps for proof-trace assembly.
type DerivationLogger interface {
	Append(sourceID, sourceType, derivedID, derivedType, operation string) DerivationEntry
	ForDerived(derivedID string) []DerivationEntry
	Since(fromLSN int64) []DerivationEntry
}

// PolicyDecisionLogger records governance decisions for audit and replay.
type PolicyDecisionLogger interface {
	Append(objectID, objectType, policyID, decision, reason string) PolicyDecisionEntry
	ForObject(objectID string) []PolicyDecisionEntry
	Since(fromLSN int64) []PolicyDecisionEntry
}
