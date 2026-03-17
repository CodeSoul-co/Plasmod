package coordinator

type Hub struct {
	Schema   *SchemaCoordinator
	Object   *ObjectCoordinator
	Policy   *PolicyCoordinator
	Version  *VersionCoordinator
	Schedule *WorkerScheduler
	Memory   *MemoryCoordinator
	Index    *IndexCoordinator
	Shard    *ShardCoordinator
	Query    *QueryCoordinator
	Registry *ModuleRegistry
}

func NewCoordinatorHub(
	schema *SchemaCoordinator,
	object *ObjectCoordinator,
	policy *PolicyCoordinator,
	version *VersionCoordinator,
	schedule *WorkerScheduler,
	memory *MemoryCoordinator,
	index *IndexCoordinator,
	shard *ShardCoordinator,
	query *QueryCoordinator,
) *Hub {
	return &Hub{
		Schema:   schema,
		Object:   object,
		Policy:   policy,
		Version:  version,
		Schedule: schedule,
		Memory:   memory,
		Index:    index,
		Shard:    shard,
		Query:    query,
		Registry: NewModuleRegistry(),
	}
}
