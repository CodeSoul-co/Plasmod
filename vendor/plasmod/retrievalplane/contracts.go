package retrievalplane

// Subsystem describes an imported retrieval-plane subtree and the ANDB role it
// plays inside the repository.
type Subsystem struct {
	Name string
	Path string
	Role string
}

// RuntimeContract defines the ANDB-facing boundary expected from any
// retrieval-plane integration, regardless of how much upstream code is kept.
type RuntimeContract interface {
	SearchService() SearchService
	StorageService() StorageService
	CompactionService() CompactionService
}

type SearchService interface {
	QueryPath() string
	SupportsSegmentPlanning() bool
}

type StorageService interface {
	ObjectStorePath() string
	SharedStoragePath() string
}

type CompactionService interface {
	CompactionPath() string
}

// Layout exposes the ANDB naming map for the imported retrieval-plane subtree.
func Layout() []Subsystem {
	return []Subsystem{
		{Name: "core", Path: "core", Role: "native engine source"},
		{Name: "queryruntime", Path: "queryruntime", Role: "query execution runtime"},
		{Name: "storage", Path: "storage", Role: "storage primitives"},
		{Name: "storageshared", Path: "storageshared", Role: "shared storage helpers"},
		{Name: "objectstore", Path: "objectstore", Role: "object storage implementation"},
		{Name: "compaction", Path: "compaction", Role: "compaction pipeline"},
	}
}
