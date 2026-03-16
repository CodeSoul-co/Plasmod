package coordinator

type Hub struct {
	Schema   *SchemaCoordinator
	Object   *ObjectCoordinator
	Policy   *PolicyCoordinator
	Version  *VersionCoordinator
	Schedule *WorkerScheduler
	Registry *ModuleRegistry
}

func NewCoordinatorHub(schema *SchemaCoordinator, object *ObjectCoordinator, policy *PolicyCoordinator, version *VersionCoordinator, schedule *WorkerScheduler) *Hub {
	return &Hub{
		Schema:   schema,
		Object:   object,
		Policy:   policy,
		Version:  version,
		Schedule: schedule,
		Registry: NewModuleRegistry(),
	}
}
