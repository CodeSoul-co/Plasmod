package storage

import "github.com/dgraph-io/badger/v4"

// ConfigSnapshot is returned by GET /v1/admin/storage and BuildRuntimeFromEnv.
type ConfigSnapshot struct {
	Mode           string            `json:"mode"`
	DataDir        string            `json:"data_dir"`
	BadgerEnabled  bool              `json:"badger_enabled"`
	Stores         map[string]string `json:"stores"`
	WALPersistence bool              `json:"wal_persistence"`
}

// RuntimeBundle is a RuntimeStorage plus metadata and optional Badger cleanup.
type RuntimeBundle struct {
	RuntimeStorage
	Config *ConfigSnapshot
	// Badger is set when any sub-store uses on-disk Badger (same DB for all disk-backed stores).
	// Used by admin wipe to DropAll; optional (nil for pure memory mode).
	Badger *badger.DB
	// Close releases Badger handles when any sub-store used on-disk KV.
	Close func() error
}
