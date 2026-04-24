package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// Environment variable names (see STORAGE_BACKEND.md).
const (
	EnvStorage        = "PLASMOD_STORAGE"
	EnvDataDir        = "PLASMOD_DATA_DIR"
	EnvStoreSegments  = "PLASMOD_STORE_SEGMENTS"
	EnvStoreIndexes   = "PLASMOD_STORE_INDEXES"
	EnvStoreObjects   = "PLASMOD_STORE_OBJECTS"
	EnvStoreEdges     = "PLASMOD_STORE_EDGES"
	EnvStoreVersions  = "PLASMOD_STORE_VERSIONS"
	EnvStorePolicies  = "PLASMOD_STORE_POLICIES"
	EnvStoreContracts = "PLASMOD_STORE_CONTRACTS"
	// EnvBadgerInMemory forces Badger to use in-RAM tables only (tests / constrained disks).
	EnvBadgerInMemory = "PLASMOD_BADGER_INMEMORY"
)

const (
	backendMemory = "memory"
	backendDisk   = "disk"
)

// BuildRuntimeFromEnv constructs RuntimeStorage from process environment.
// Default mode is "disk" (Badger-backed persistent storage under PLASMOD_DATA_DIR).
// Set PLASMOD_STORAGE=memory to opt into ephemeral in-process mode (tests / CI).
func BuildRuntimeFromEnv() (*RuntimeBundle, error) {
	return buildRuntime(os.Getenv)
}

func buildRuntime(get func(string) string) (*RuntimeBundle, error) {
	mode := strings.TrimSpace(strings.ToLower(get(EnvStorage)))
	if mode == "" {
		mode = backendDisk
	}
	dataDir := get(EnvDataDir)
	if dataDir == "" {
		dataDir = ".andb_data"
	}
	dataDir = filepath.Clean(dataDir)

	stores := map[string]string{
		"segments":  resolveBackend(mode, get(EnvStoreSegments)),
		"indexes":   resolveBackend(mode, get(EnvStoreIndexes)),
		"objects":   resolveBackend(mode, get(EnvStoreObjects)),
		"edges":     resolveBackend(mode, get(EnvStoreEdges)),
		"versions":  resolveBackend(mode, get(EnvStoreVersions)),
		"policies":  resolveBackend(mode, get(EnvStorePolicies)),
		"contracts": resolveBackend(mode, get(EnvStoreContracts)),
	}

	needBadger := false
	for _, b := range stores {
		if b == backendDisk {
			needBadger = true
			break
		}
	}

	snap := &ConfigSnapshot{
		Mode:           mode,
		DataDir:        dataDir,
		BadgerEnabled:  needBadger,
		Stores:         stores,
		WALPersistence: needBadger || mode == backendDisk,
	}

	if !needBadger {
		mem := NewMemoryRuntimeStorage()
		return &RuntimeBundle{
			RuntimeStorage: mem,
			Config:         snap,
			Badger:         nil,
			Close:          func() error { return nil },
		}, nil
	}

	var db *badger.DB
	var err error
	if strings.EqualFold(strings.TrimSpace(get(EnvBadgerInMemory)), "true") {
		snap.DataDir = ":memory:"
		db, err = openBadgerInMemory()
		if err != nil {
			return nil, fmt.Errorf("storage: open badger (in-memory): %w", err)
		}
	} else {
		if err = os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, fmt.Errorf("storage: mkdir %q: %w", dataDir, err)
		}
		db, err = openBadger(dataDir)
		if err != nil {
			return nil, fmt.Errorf("storage: open badger %q: %w", dataDir, err)
		}
	}

	seg := pickSegment(stores["segments"], db)
	idx := pickIndex(stores["indexes"], db)
	obj := pickObject(stores["objects"], db)
	edg := pickEdge(stores["edges"], db)
	ver := pickVersion(stores["versions"], db)
	pol := pickPolicy(stores["policies"], db)
	ctr := pickContract(stores["contracts"], db)

	rt := NewCompositeRuntimeStorage(seg, idx, obj, edg, ver, pol, ctr, NewHotObjectCache(2000))

	return &RuntimeBundle{
		RuntimeStorage: rt,
		Config:         snap,
		Badger:         db,
		Close: func() error {
			return db.Close()
		},
	}, nil
}

// resolveBackend returns "memory" or "disk" per store.
// Explicit env always wins. Otherwise: mode=disk → disk; mode=hybrid → memory; else memory.
func resolveBackend(mode, explicit string) string {
	e := strings.TrimSpace(strings.ToLower(explicit))
	switch e {
	case "disk", "badger":
		return backendDisk
	case "memory":
		return backendMemory
	}
	switch mode {
	case "disk":
		return backendDisk
	case "hybrid":
		return backendMemory
	default:
		return backendMemory
	}
}

func pickSegment(which string, db *badger.DB) SegmentStore {
	if which == backendDisk {
		return newBadgerSegmentStore(db)
	}
	return NewMemorySegmentStore()
}

func pickIndex(which string, db *badger.DB) IndexStore {
	if which == backendDisk {
		return newBadgerIndexStore(db)
	}
	return NewMemoryIndexStore()
}

func pickObject(which string, db *badger.DB) ObjectStore {
	if which == backendDisk {
		return newBadgerObjectStore(db)
	}
	return NewMemoryObjectStore()
}

func pickEdge(which string, db *badger.DB) GraphEdgeStore {
	if which == backendDisk {
		return newBadgerGraphEdgeStore(db)
	}
	return NewMemoryGraphEdgeStore()
}

func pickVersion(which string, db *badger.DB) SnapshotVersionStore {
	if which == backendDisk {
		return newBadgerSnapshotVersionStore(db)
	}
	return NewMemorySnapshotVersionStore()
}

func pickPolicy(which string, db *badger.DB) PolicyStore {
	if which == backendDisk {
		return newBadgerPolicyStore(db)
	}
	return NewMemoryPolicyStore()
}

func pickContract(which string, db *badger.DB) ShareContractStore {
	if which == backendDisk {
		return newBadgerShareContractStore(db)
	}
	return NewMemoryShareContractStore()
}
