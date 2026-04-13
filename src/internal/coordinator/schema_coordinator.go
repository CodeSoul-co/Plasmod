package coordinator

import "plasmod/src/internal/semantic"

type SchemaCoordinator struct {
	model *semantic.ObjectModelRegistry
}

func NewSchemaCoordinator(model *semantic.ObjectModelRegistry) *SchemaCoordinator {
	return &SchemaCoordinator{model: model}
}
