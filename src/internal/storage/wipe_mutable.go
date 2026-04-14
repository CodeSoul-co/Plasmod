package storage

// WipeMutableRuntimeState clears all in-memory / hybrid memory-backed stores in place.
// When using Badger, the caller must run Badger.DropAll before this so on-disk KV is empty.
func WipeMutableRuntimeState(rs RuntimeStorage) {
	switch s := rs.(type) {
	case *MemoryRuntimeStorage:
		s.wipeAllData()
	case *compositeRuntimeStorage:
		s.wipeAllData()
	}
}
