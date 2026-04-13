package semantic

import "plasmod/src/internal/schemas"

// objectMeta describes the properties of a canonical object type.
type objectMeta struct {
	Name        string
	PKField     string
	Versionable bool
	Indexable   bool
}

// ObjectModelRegistry maps canonical ObjectType values to their metadata.
// All first-class objects must be registered here so coordinators and workers
// can introspect type properties without hard-coded switch statements.
type ObjectModelRegistry struct {
	types map[schemas.ObjectType]objectMeta
}

func NewObjectModelRegistry() *ObjectModelRegistry {
	r := &ObjectModelRegistry{types: map[schemas.ObjectType]objectMeta{}}
	r.registerDefaults()
	return r
}

func (r *ObjectModelRegistry) registerDefaults() {
	r.types[schemas.ObjectTypeAgent] = objectMeta{
		Name: "agent", PKField: "agent_id", Versionable: true, Indexable: false,
	}
	r.types[schemas.ObjectTypeSession] = objectMeta{
		Name: "session", PKField: "session_id", Versionable: true, Indexable: false,
	}
	r.types[schemas.ObjectTypeEvent] = objectMeta{
		Name: "event", PKField: "event_id", Versionable: true, Indexable: true,
	}
	r.types[schemas.ObjectTypeMemory] = objectMeta{
		Name: "memory", PKField: "memory_id", Versionable: true, Indexable: true,
	}
	r.types[schemas.ObjectTypeState] = objectMeta{
		Name: "state", PKField: "state_id", Versionable: true, Indexable: false,
	}
	r.types[schemas.ObjectTypeArtifact] = objectMeta{
		Name: "artifact", PKField: "artifact_id", Versionable: true, Indexable: true,
	}
	r.types[schemas.ObjectTypeEdge] = objectMeta{
		Name: "edge", PKField: "edge_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypeObjectVersion] = objectMeta{
		Name: "object_version", PKField: "object_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypeUser] = objectMeta{
		Name: "user", PKField: "user_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypePolicyRecord] = objectMeta{
		Name: "policy_record", PKField: "policy_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypeEmbedding] = objectMeta{
		Name: "embedding", PKField: "vector_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypeShareContract] = objectMeta{
		Name: "share_contract", PKField: "contract_id", Versionable: false, Indexable: false,
	}
	r.types[schemas.ObjectTypeRetrievalSeg] = objectMeta{
		Name: "retrieval_segment", PKField: "segment_id", Versionable: false, Indexable: false,
	}
}

// IsKnown returns true if the type is registered.
func (r *ObjectModelRegistry) IsKnown(t schemas.ObjectType) bool {
	_, ok := r.types[t]
	return ok
}

// IsVersionable returns true if object mutations must produce a version record.
func (r *ObjectModelRegistry) IsVersionable(t schemas.ObjectType) bool {
	m, ok := r.types[t]
	return ok && m.Versionable
}

// IsIndexable returns true if objects of this type should be projected into the
// retrieval (vector/sparse/temporal) index.
func (r *ObjectModelRegistry) IsIndexable(t schemas.ObjectType) bool {
	m, ok := r.types[t]
	return ok && m.Indexable
}

// PKField returns the JSON primary-key field name for the given type.
func (r *ObjectModelRegistry) PKField(t schemas.ObjectType) string {
	if m, ok := r.types[t]; ok {
		return m.PKField
	}
	return ""
}

// RegisteredTypes returns the complete list of registered object type names.
func (r *ObjectModelRegistry) RegisteredTypes() []schemas.ObjectType {
	out := make([]schemas.ObjectType, 0, len(r.types))
	for k := range r.types {
		out = append(out, k)
	}
	return out
}
