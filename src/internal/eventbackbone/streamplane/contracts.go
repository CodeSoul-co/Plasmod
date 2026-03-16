package streamplane

// Subsystem describes an imported stream-plane subtree and its ANDB role.
type Subsystem struct {
	Name string
	Path string
	Role string
}

// RuntimeContract defines the ANDB-facing boundary expected from the imported
// event-backbone subtree before deeper adoption into first-party runtime code.
type RuntimeContract interface {
	ClockService() ClockService
	StreamCoord() StreamCoord
	StreamNode() StreamNode
	FlushPipeline() FlushPipeline
}

type ClockService interface {
	ModulePath() string
}

type StreamCoord interface {
	ModulePath() string
}

type StreamNode interface {
	ModulePath() string
}

type FlushPipeline interface {
	ModulePath() string
}

// Layout exposes the ANDB naming map for the imported stream-plane subtree.
func Layout() []Subsystem {
	return []Subsystem{
		{Name: "clockservice", Path: "clockservice", Role: "time and sequence services"},
		{Name: "streamcoord", Path: "streamcoord", Role: "stream control plane"},
		{Name: "streamnode", Path: "streamnode", Role: "stream execution node"},
		{Name: "flushpipeline", Path: "flushpipeline", Role: "flush and persistence pipeline"},
	}
}
