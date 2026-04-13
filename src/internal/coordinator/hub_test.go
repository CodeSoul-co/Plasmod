package coordinator

import (
	"testing"

	"plasmod/src/internal/eventbackbone"
	"plasmod/src/internal/semantic"
	"plasmod/src/internal/storage"
)

func TestNewCoordinatorHub_FieldsInitialized(t *testing.T) {
	store := storage.NewMemoryRuntimeStorage()
	clock := eventbackbone.NewHybridClock()
	policy := semantic.NewPolicyEngine()
	model := semantic.NewObjectModelRegistry()
	planner := semantic.NewDefaultQueryPlanner()

	hub := NewCoordinatorHub(
		NewSchemaCoordinator(model),
		NewObjectCoordinator(store.Objects(), store.Versions()),
		NewPolicyCoordinator(policy, store.Policies()),
		NewVersionCoordinator(clock, store.Versions()),
		NewWorkerScheduler(),
		NewMemoryCoordinator(store.Objects()),
		NewIndexCoordinator(store.Segments(), store.Indexes()),
		NewShardCoordinator(4),
		NewQueryCoordinator(planner, policy),
	)

	if hub.Schema == nil {
		t.Error("Schema coordinator is nil")
	}
	if hub.Object == nil {
		t.Error("Object coordinator is nil")
	}
	if hub.Policy == nil {
		t.Error("Policy coordinator is nil")
	}
	if hub.Query == nil {
		t.Error("Query coordinator is nil")
	}
	if hub.Registry == nil {
		t.Fatal("Registry is nil")
	}
}

func TestModuleRegistry_RegisterAndGet(t *testing.T) {
	r := NewModuleRegistry()
	r.Register("test_service", "value_42")

	got, ok := r.Get("test_service")
	if !ok {
		t.Fatal("Registry.Get: expected to find test_service")
	}
	if got != "value_42" {
		t.Errorf("Registry.Get: want %q, got %v", "value_42", got)
	}

	_, exists := r.Get("nonexistent")
	if exists {
		t.Error("Registry.Get on unknown key should return ok=false")
	}
}
