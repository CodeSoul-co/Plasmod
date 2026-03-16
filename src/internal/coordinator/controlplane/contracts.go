package controlplane

// Subsystem describes an imported control-plane subtree and its role in ANDB.
type Subsystem struct {
	Name string
	Path string
	Role string
}

// RuntimeContract defines the ANDB-facing boundary expected from the imported
// control-plane code before it can be adopted by first-party modules.
type RuntimeContract interface {
	MetadataControl() MetadataControl
	DataControl() DataControl
	QueryControl() QueryControl
	AccessProxy() AccessProxy
}

type MetadataControl interface {
	ModulePath() string
}

type DataControl interface {
	ModulePath() string
}

type QueryControl interface {
	ModulePath() string
}

type AccessProxy interface {
	ModulePath() string
}

// Layout exposes the ANDB naming map for the imported control-plane subtree.
func Layout() []Subsystem {
	return []Subsystem{
		{Name: "coordinator", Path: "coordinator", Role: "shared control services"},
		{Name: "metacontrol", Path: "metacontrol", Role: "metadata and root control"},
		{Name: "datacontrol", Path: "datacontrol", Role: "data placement and coordination"},
		{Name: "querycontrol", Path: "querycontrol", Role: "query scheduling and balancing"},
		{Name: "accessproxy", Path: "accessproxy", Role: "request routing and access proxy"},
	}
}
